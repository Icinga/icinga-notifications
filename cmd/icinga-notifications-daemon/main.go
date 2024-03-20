package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/icinga/icinga-notifications/internal/channel"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/internal/incident"
	"github.com/icinga/icinga-notifications/internal/listener"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/icingadb/pkg/utils"
	"go.uber.org/zap"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

func main() {
	var configPath string
	var showVersion bool

	flag.StringVar(&configPath, "config", "", "path to config file")
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

	logs, err := logging.NewLogging(
		"icinga-notifications",
		conf.Logging.Level,
		conf.Logging.Output,
		conf.Logging.Options,
		conf.Logging.Interval,
	)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "cannot initialize logging:", err)
		os.Exit(1)
	}

	logger := logs.GetLogger()
	logger.Infof("Starting Icinga Notifications daemon (%s)", internal.Version.Version)
	db, err := conf.Database.Open(logs.GetChildLogger("database"))
	if err != nil {
		logger.Fatalw("cannot create database connection from config", zap.Error(err))
	}
	defer db.Close()
	{
		logger.Infof("Connecting to database at '%s'", utils.JoinHostPort(conf.Database.Host, conf.Database.Port))
		if err := db.Ping(); err != nil {
			logger.Fatalw("cannot connect to database", zap.Error(err))
		}
	}

	channel.UpsertPlugins(conf.ChannelPluginDir, logs.GetChildLogger("channel"), db)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	runtimeConfig := config.NewRuntimeConfig(db, logs)
	if err := runtimeConfig.UpdateFromDatabase(ctx); err != nil {
		logger.Fatalw("failed to load config from database", zap.Error(err))
	}

	go runtimeConfig.PeriodicUpdates(ctx, 1*time.Second)

	err = incident.LoadOpenIncidents(ctx, db, logs.GetChildLogger("incident"), runtimeConfig)
	if err != nil {
		logger.Fatalw("Can't load incidents from database", zap.Error(err))
	}

	if err := listener.NewListener(db, runtimeConfig, logs).Run(ctx); err != nil {
		logger.Errorw("Listener has finished with an error", zap.Error(err))
	} else {
		logger.Info("Listener has finished")
	}
}
