package channel

import (
	"bufio"
	"fmt"
	"github.com/icinga/icingadb/pkg/logging"
	"go.uber.org/zap"
	"io"
	"os/exec"
	"path/filepath"
)

const pluginDir = "/usr/libexec/icinga-notifications/channel"

type Channel struct {
	ID     int64  `db:"id"`
	Name   string `db:"name"`
	Type   string `db:"type"`
	Config string `db:"config" json:"-"` // excluded from JSON config dump as this may contain sensitive information

	Plugin *Plugin
	Logger *logging.Logger
}

func (c *Channel) GetPlugin() (*Plugin, error) {
	if c.Plugin == nil {
		p, err := SpawnPlugin(filepath.Join(pluginDir, c.Type), c.Config, c.Logger)
		if err != nil {
			return p, fmt.Errorf("unknown channel type %q", c.Type)
		}

		c.Plugin = p
	}

	return c.Plugin, nil
}

func (c *Channel) ResetPlugin() error {
	if c.Plugin != nil {
		if err := c.Plugin.writer.Close(); err != nil {
			return err
		}

		c.Plugin = nil
	}

	return nil
}

func SpawnPlugin(path string, config string, baseLogger *logging.Logger) (*Plugin, error) {
	logger := baseLogger.With(zap.String("path", path))

	cmd := exec.Command(path)

	writer, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	tempReader, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	reader := bufio.NewReader(tempReader)

	errPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	go forwardLogs(errPipe, logger)

	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start cmd: %w", err)
	}
	logger.Debug("Cmd started successfully")

	p := &Plugin{cmd: cmd, writer: writer, reader: reader, Logger: logger}

	info, err := p.GetInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get p info: %w", err)
	}
	logger.Debug("Plugin info: ", info)

	if err = p.SetConfig(config); err != nil {
		return nil, fmt.Errorf("failed to set config: %w", err)
	}
	logger.Debug("Successfully set p config: ", config)

	return p, nil
}

func forwardLogs(errPipe io.Reader, logger *zap.SugaredLogger) {
	reader := bufio.NewReader(errPipe)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			logger.Errorw("Failed to read stderr line", zap.Error(err))
			return
		}

		logger.Info(line)
	}
}
