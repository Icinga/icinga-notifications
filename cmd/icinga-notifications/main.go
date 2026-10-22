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
	"github.com/icinga/icinga-go-library/periodic"
	"github.com/icinga/icinga-go-library/utils"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/icinga/icinga-notifications/internal/channel"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/listener"
	"github.com/icinga/icinga-notifications/internal/object"
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
	flags := daemon.ParseFlagsAndConfig()
	conf := daemon.Config()

	logs, err := logging.NewLoggingFromConfig("icinga-notifications", conf.Logging)
	if err != nil {
		utils.PrintErrorThenExit(err, daemon.ExitFailure)
	}

	logger := logs.GetLogger()
	defer func() { _ = logger.Sync() }()

	logger.Infof("Starting Icinga Notifications daemon (%s)", internal.Version.Version)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	{
		// Use a dedicated database connection for the schema check and import, so that any session settings changed
		// by the schema don't leak into the main database pool. It also holds the schema lock until it is closed.
		tmpDb, err := database.NewDbFromConfig(&conf.Database, logs.GetChildLogger("database"), database.RetryConnectorCallbacks{})
		if err != nil {
			logger.Fatalf("Cannot create database connection from config: %+v", err)
		}
		tmpDb.SetMaxOpenConns(1)

		if err := internal.AcquireSchemaLock(ctx, tmpDb); err != nil {
			logger.Fatalf("%+v", err)
		}

		switch err := internal.CheckSchema(ctx, tmpDb); {
		case errors.Is(err, internal.ErrSchemaNotExists):
			if !flags.DatabaseAutoImport {
				logger.Fatal("Missing database schema. Please import the schema first or use the --database-auto-import flag to automatically import it.")
			}

			logger.Info("Missing database schema. Attempting to import the schema...")

			if err := internal.ImportSchema(ctx, tmpDb, flags.DatabaseSchemaDir); err != nil {
				logger.Fatalf("Cannot import schema: %+v", err)
			}

			logger.Infof("Successfully imported database schema")
		case err != nil:
			logger.Fatalf("%+v", err)
		}

		// This will close the connection and release the named lock, allowing other processes to acquire it.
		_ = tmpDb.Close()

		// If the --database-schema-only flag is set, exit successfully after checking/importing the schema.
		if flags.DBSchemaOnly {
			return ExitSuccess
		}
	}

	db, err := database.NewDbFromConfig(&conf.Database, logs.GetChildLogger("database"), database.RetryConnectorCallbacks{})
	if err != nil {
		logger.Fatalf("Cannot create database connection from config: %+v", err)
	}
	defer func() { _ = db.Close() }()

	logger.Infof("Connecting to database at '%s'", db.GetAddr())
	if err := db.PingContext(ctx); err != nil {
		logger.Fatalf("Cannot connect to the database: %+v", err)
	}

	channel.UpsertPlugins(ctx, conf.ChannelsDir, logs.GetChildLogger("channel"), db)

	runtimeConfig := config.NewRuntimeConfig(logs, db)
	if err := runtimeConfig.UpdateFromDatabase(ctx); err != nil {
		logger.Fatalf("Failed to load config from database %+v", err)
	}

	go runtimeConfig.PeriodicUpdates(ctx, 1*time.Second)

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

	eg.Go(func() error {
		logger := logs.GetChildLogger("incident")

		stop := periodic.Start(ctx, 30*time.Second, func(periodic.Tick) {
			err := incident.ReevaluateEscalations(ctx, db, logger, runtimeConfig)
			if errors.Is(err, context.Canceled) {
				logger.Info("Reevaluating incident escalations has finished")
			} else if err != nil {
				logger.Errorf("Cannot reevaluate incident escalations: %+v", err)
			}
		}, periodic.Immediate())

		<-ctx.Done()
		stop.Stop()
		return nil
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
		logger := logs.GetChildLogger("job-queue")
		cbs := event.QueueCallbacks{
			GenObjectID: object.ID,
			ProcessEvent: func(ctx context.Context, ev *event.Event) error {
				err := incident.Process(ctx, db, logs, runtimeConfig, ev)
				if errors.Is(err, incident.ErrSeverityChangeWithoutIncidentFlag) ||
					errors.Is(err, incident.ErrOpenIncidentWithoutSeverity) {
					logger.Debugw("Skipping event processing",
						zap.String("event_name", ev.Name),
						zap.Error(err))
					return nil
				}
				return err
			},
			QuickAction: func(ctx context.Context, qa *event.QuickAction) error {
				return incident.Process(ctx, db, logs, runtimeConfig, qa)
			},
		}

		if err := event.ProcessQueue(ctx, db, logger, cbs); err != nil && !errors.Is(err, context.Canceled) {
			logger.Errorf("Event queue processor has finished with an error: %+v", err)
			return err
		}

		logger.Info("Event queue processor has finished")
		return nil
	})

	// When Icinga Notifications is started by systemd, we've to notify systemd that we're ready.
	_ = sdnotify.Ready()

	if err := eg.Wait(); err != nil {
		return ExitFailure
	}

	return ExitSuccess
}
