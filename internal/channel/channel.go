package channel

import (
	"bufio"
	"fmt"
	"github.com/icinga/icingadb/pkg/logging"
	"go.uber.org/zap"
	"io"
	"os/exec"
	"path/filepath"
	"time"
)

const pluginDir = "/usr/libexec/icinga-notifications/channel"

type Channel struct {
	ID     int64  `db:"id"`
	Name   string `db:"name"`
	Type   string `db:"type"`
	Config string `db:"config" json:"-"` // excluded from JSON config dump as this may contain sensitive information

	Logger *logging.Logger
	cmd    *exec.Cmd
	rpc    *RPC
}

func (c *Channel) StartPlugin() error {
	if c.cmd != nil {
		return nil
	}

	path := filepath.Join(pluginDir, c.Type)
	logger := c.Logger.With(zap.String("cmd", path))

	cmd := exec.Command(path)

	writer, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	reader, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	errPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	go forwardLogs(errPipe, logger)

	if err = cmd.Start(); err != nil {
		return fmt.Errorf("failed to start cmd: %w", err)
	}
	logger.Debug("Cmd started successfully")

	rpc := newRPC(writer, reader, logger)

	c.cmd = cmd
	c.rpc = rpc

	if err = c.SetConfig(c.Config); err != nil {
		c.ResetPlugin()

		return fmt.Errorf("failed to set config: %w", err)
	}
	logger.Debug("Successfully set config: ", c.Config)

	return nil
}

func (c *Channel) ResetPlugin() {
	if c.cmd != nil {

		go func(cmd *exec.Cmd, rpc *RPC) {
			_ = rpc.Close()

			timer := time.AfterFunc(5*time.Second, func() {
				_ = cmd.Process.Kill()
			})

			_ = cmd.Wait()
			timer.Stop()
		}(c.cmd, c.rpc)

		c.cmd = nil
		c.rpc = nil
	}
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
