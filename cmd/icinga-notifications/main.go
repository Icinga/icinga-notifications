package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/utils"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/icinga/icinga-notifications/internal/channel"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/listener"
	"github.com/icinga/icinga-notifications/internal/retention"
	"github.com/okzk/sdnotify"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
)

const (
	ExitSuccess = 0
	ExitFailure = 1
)

func main() {
	os.Exit(run())
}

func run() int {
	unix.Umask(0077) // Ensure Unix sockets are created with 0600 by default, denying group/other access.
	daemon.ParseFlagsAndConfig()
	conf := daemon.Config()

	logs, err := logging.NewLoggingFromConfig("icinga-notifications", conf.Logging)
	if err != nil {
		utils.PrintErrorThenExit(err, daemon.ExitFailure)
	}

	logger := logs.GetLogger()
	defer func() { _ = logger.Sync() }()

	logger.Infof("Starting Icinga Notifications daemon (%s)", internal.Version.Version)
	db, err := database.NewDbFromConfig(&conf.Database, logs.GetChildLogger("database"), database.RetryConnectorCallbacks{})
	if err != nil {
		logger.Fatalf("Cannot create database connection from config: %+v", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Infof("Connecting to database at '%s'", db.GetAddr())
	if err := db.PingContext(ctx); err != nil {
		logger.Fatalf("Cannot connect to the database: %+v", err)
	}

	if err := internal.CheckSchema(ctx, db); err != nil {
		logger.Fatalf("%+v", err)
	}

	channel.UpsertPlugins(ctx, conf.ChannelsDir, logs.GetChildLogger("channel"), db)

	runtimeConfig := config.NewRuntimeConfig(logs, db)
	if err := runtimeConfig.UpdateFromDatabase(ctx); err != nil {
		logger.Fatalf("Failed to load config from database %+v", err)
	}

	go runtimeConfig.PeriodicUpdates(ctx, 1*time.Second)

	err = incident.LoadOpenIncidents(ctx, db, logs.GetChildLogger("incident"), runtimeConfig)
	if err != nil {
		logger.Fatalf("Cannot load incidents from database: %+v", err)
	}

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		err := retention.New(db, logs.GetChildLogger("retention")).Run(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			logger.Info("Retention has finished")
			return nil
		} else {
			logger.Errorf("Retention has finished with an error: %+v", err)
			return err
		}
	})

	listenerConf := daemon.Config().Listener
	makeListeners := func(useSocket bool) {
		eg.Go(func() error {
			err := listener.NewListener(db, runtimeConfig, logs, useSocket).Run(ctx)
			if err == nil || errors.Is(err, context.Canceled) {
				logger.Info("Listener has finished")
				return nil
			} else {
				logger.Errorf("Listener has finished with an error: %+v", err)
				return err
			}
		})
	}

	if listenerConf.Socket != "" {
		makeListeners(true)
	}
	if listenerConf.Addr != "" {
		makeListeners(false)
	}

	eg.Go(func() error {
		logger := logs.GetChildLogger("event-queue")
		err := event.ProcessQueue(
			ctx,
			db,
			logger,
			func(ctx context.Context, ev *event.Event) error {
				err := incident.ProcessEvent(ctx, db, logs, runtimeConfig, ev)
				if errors.Is(err, incident.ErrSeverityChangeWithoutIncidentFlag) ||
					errors.Is(err, incident.ErrOpenIncidentWithoutSeverity) {
					logger.Debugw("Skipping event processing",
						zap.String("event_name", ev.Name),
						zap.Error(err))
					return nil
				} else if err != nil {
					logger.Errorw("Failed to successfully process event",
						zap.String("event_name", ev.Name),
						zap.Error(err))
					return err
				}

				logger.Infow("Successfully processed event", zap.String("event_name", ev.Name))
				return nil
			})
		if err == nil || errors.Is(err, context.Canceled) {
			logger.Info("Event queue processor has finished")
			return nil
		} else {
			logger.Errorf("Event queue processor has finished with an error: %+v", err)
			return err
		}
	})

	// When Icinga Notifications is started by systemd, we've to notify systemd that we're ready.
	_ = sdnotify.Ready()

	if err := eg.Wait(); err != nil {
		return ExitFailure
	}

	return ExitSuccess
}
