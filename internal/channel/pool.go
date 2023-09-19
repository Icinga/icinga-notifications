package channel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/pluginLoader"
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

type Plugin struct {
	config string
	path   string
	cmd    *exec.Cmd
	writer io.WriteCloser
	reader *bufio.Reader
	Logger *zap.SugaredLogger
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

	if _, err = fmt.Fprintln(writer, config); err != nil {
		return nil, fmt.Errorf("failed to pass config to writer: %w", err)
	}
	logger.Debug("Successfully pass config to writer")

	return &Plugin{cmd: cmd, writer: writer, reader: reader, Logger: logger}, nil
}

func (p *Plugin) Run(req *pluginLoader.NotificationRequest) error {
	marshal, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("json.Marschal failed: %w", err)
	}

	line := string(marshal)

	if _, err = fmt.Fprintln(p.writer, line); err != nil {
		return fmt.Errorf("failed to pass line to writer: %w", err)
	}
	p.Logger.Debugw("Successfully pass line to writer:", zap.String("line", line))

	res, err := p.reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read responce: %w", err)
	}

	p.Logger.Debugw("Successfully read response", zap.String("output", res))

	var response = pluginLoader.Response{}
	if err = json.Unmarshal([]byte(res), &response); err != nil {
		return fmt.Errorf("failed to decode json response: %w", err)
	} else if !response.Success {
		return fmt.Errorf("plugin response contains error: %s", response.Error)
	}

	return nil
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
