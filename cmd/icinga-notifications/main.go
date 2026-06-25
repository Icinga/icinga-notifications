package main

import (
	"context"
	"fmt"
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
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/listener"
	"github.com/okzk/sdnotify"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
)

func main() {
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

	// When Icinga Notifications is started by systemd, we've to notify systemd that we're ready.
	_ = sdnotify.Ready()

	g, gCtx := errgroup.WithContext(ctx)
	listenerConf := daemon.Config().Listener
	if listenerConf.Socket != "" {
		g.Go(func() error {
			if err := listener.NewListener(db, runtimeConfig, logs, true).Run(gCtx); err != nil {
				return fmt.Errorf("socket listener: %w", err)
			}
			return nil
		})
	}

	if listenerConf.Addr != "" {
		g.Go(func() error {
			if err := listener.NewListener(db, runtimeConfig, logs, false).Run(gCtx); err != nil {
				return fmt.Errorf("tcp listener: %w", err)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		logger.Errorf("A listener has finished with an error: %+v", err)
	} else {
		logger.Info("All listeners have finished")
	}
}
