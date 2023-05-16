package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/listener"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/icingadb/pkg/utils"
	"go.uber.org/zap"
	"os"
	"time"
)

func main() {
	var configPath string

	flag.StringVar(&configPath, "config", "", "path to config file")
	flag.Parse()

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

	if err := listener.NewListener(db, conf, runtimeConfig, logs.GetChildLogger("listener")).Run(); err != nil {
		panic(err)
	}
}
