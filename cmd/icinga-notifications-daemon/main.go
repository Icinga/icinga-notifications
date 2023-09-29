package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/icinga/icinga-notifications/internal"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/listener"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/icingadb/pkg/utils"
	"go.uber.org/zap"
	"os"
	"runtime"
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

	if configPath == "" {
		_, _ = fmt.Fprintln(os.Stderr, "missing -config flag")
		os.Exit(1)
	}

	conf, err := config.FromFile(configPath)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "cannot load config:", err)
		os.Exit(1)
	}

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

	runtimeConfig := config.NewRuntimeConfig(db, logs.GetChildLogger("runtime-updates"))
	if err := runtimeConfig.UpdateFromDatabase(context.TODO()); err != nil {
		logger.Fatalw("failed to load config from database", zap.Error(err))
	}

	go runtimeConfig.PeriodicUpdates(context.TODO(), 1*time.Second)

	if err := listener.NewListener(db, conf, runtimeConfig, logs).Run(); err != nil {
		panic(err)
	}
}
