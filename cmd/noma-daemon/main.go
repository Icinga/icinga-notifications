package main

import (
	"flag"
	"fmt"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/noma/internal/config"
	"github.com/icinga/noma/internal/listener"
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

	// TODO: proper logging config
	logs, err := logging.NewLogging("noma", zap.DebugLevel, logging.CONSOLE, logging.Options{}, 10*time.Second)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "cannot initialize logging:", err)
		os.Exit(1)
	}

	logger := logs.GetLogger()
	logger.Info("connecting to database")
	db, err := conf.Database.Open(logger)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "cannot connect to database:", err)
		os.Exit(1)
	}
	logger.Debugw("pinged database", zap.Error(db.Ping()))

	if err := listener.NewListener(conf.Listen).Run(); err != nil {
		panic(err)
	}
}
