package main

import (
	"context"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/utils"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/icinga/icinga-notifications/internal/channel"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/listener"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/okzk/sdnotify"
	"os/signal"
	"syscall"
	"time"
)

func main() {
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

	channel.UpsertPlugins(ctx, conf.ChannelsDir, logs.GetChildLogger("channel"), db)

	runtimeConfig := config.NewRuntimeConfig(logs, db)
	if err := runtimeConfig.UpdateFromDatabase(ctx); err != nil {
		logger.Fatalf("Failed to load config from database %+v", err)
	}

	go runtimeConfig.PeriodicUpdates(ctx, 1*time.Second)

	logger.Info("Loading all active incidents from database")
	if err = incident.LoadOpenIncidents(ctx, db, runtimeConfig); err != nil {
		logger.Fatalf("Cannot load incidents from database: %+v", err)
	}

	// Restore all muted objects that do not have an active incident yet, so that we do not trigger notifications
	// for them even though they are muted, and also not to override the actual mute reason with a made-up one.
	if err := object.RestoreMutedObjects(ctx, db); err != nil {
		logger.Fatalf("Failed to restore muted objects: %+v", err)
	}

	// When Icinga Notifications is started by systemd, we've to notify systemd that we're ready.
	_ = sdnotify.Ready()

	if err := listener.NewListener(runtimeConfig).Run(ctx); err != nil {
		logger.Errorf("Listener has finished with an error: %+v", err)
	} else {
		logger.Info("Listener has finished")
	}
}
