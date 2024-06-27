package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/utils"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/icinga/icinga-notifications/internal/channel"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/icinga2"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/listener"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/okzk/sdnotify"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

func main() {
	var configPath string
	var showVersion bool

	flag.StringVar(&configPath, "config", internal.SysConfDir+"/icinga-notifications/config.yml", "path to config file")
	flag.BoolVar(&showVersion, "version", false, "print version")
	flag.Parse()

	if showVersion {
		// reuse internal.Version.print() once the project name is configurable
		fmt.Println("Icinga Notifications version:", internal.Version.Version)
		fmt.Println()

		fmt.Println("Build information:")
		fmt.Printf("  Go version: %s (%s, %s)\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
		if internal.Version.Commit != "" {
			fmt.Println("  Git commit:", internal.Version.Commit)
		}
		return
	}

	err := daemon.LoadConfig(configPath)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "cannot load config:", err)
		os.Exit(1)
	}

	conf := daemon.Config()

	logs, err := logging.NewLoggingFromConfig("icinga-notifications", conf.Logging)
	if err != nil {
		utils.PrintErrorThenExit(err, 1)
	}

	logger := logs.GetLogger()
	defer func() { _ = logger.Sync() }()

	logger.Infof("Starting Icinga Notifications daemon (%s)", internal.Version.Version)
	db, err := database.NewDbFromConfig(&conf.Database, logs.GetChildLogger("database"), database.RetryConnectorCallbacks{})
	if err != nil {
		logger.Fatalf("Cannot create database connection from config: %+v", err)
	}
	defer db.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Infof("Connecting to database at '%s'", db.GetAddr())
	if err := db.PingContext(ctx); err != nil {
		logger.Fatalf("Cannot connect to the database: %+v", err)
	}

	channel.UpsertPlugins(ctx, conf.ChannelsDir, logs.GetChildLogger("channel"), db)

	icinga2Launcher := &icinga2.Launcher{
		Ctx:           ctx,
		Logs:          logs,
		Db:            db,
		RuntimeConfig: nil, // Will be set below as it is interconnected..
	}

	runtimeConfig := config.NewRuntimeConfig(icinga2Launcher.Launch, logs, db)
	if err := runtimeConfig.UpdateFromDatabase(ctx); err != nil {
		logger.Fatalf("Failed to load config from database %+v", err)
	}

	icinga2Launcher.RuntimeConfig = runtimeConfig

	go runtimeConfig.PeriodicUpdates(ctx, 1*time.Second)

	err = incident.LoadOpenIncidents(ctx, db, logs.GetChildLogger("incident"), runtimeConfig)
	if err != nil {
		logger.Fatalf("Cannot load incidents from database: %+v", err)
	}

	// Restore all muted objects that do not have an active incident yet, so that we do not trigger notifications
	// for them even though they are muted, and also not to override the actual mute reason with a made-up one.
	if err := object.RestoreMutedObjects(ctx, db); err != nil {
		logger.Fatalf("Failed to restore muted objects: %+v", err)
	}

	// Wait to load open incidents from the database before either starting Event Stream Clients or starting the Listener.
	icinga2Launcher.Ready()

	// When Icinga Notifications is started by systemd, we've to notify systemd that we're ready.
	_ = sdnotify.Ready()

	if err := listener.NewListener(db, runtimeConfig, logs).Run(ctx); err != nil {
		logger.Errorf("Listener has finished with an error: %+v", err)
	} else {
		logger.Info("Listener has finished")
	}
}
