package channel

import (
	"bufio"
	"fmt"
	"github.com/icinga/icinga-notifications/pkg/rpc"
	"go.uber.org/zap"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

type Channel struct {
	ID     int64  `db:"id"`
	Name   string `db:"name"`
	Type   string `db:"type"`
	Config string `db:"config" json:"-"` // excluded from JSON config dump as this may contain sensitive information

	Logger *zap.SugaredLogger
	cmd    *exec.Cmd
	rpc    *rpc.RPC
	mu     sync.Mutex
}

func (c *Channel) StartPlugin(pluginDir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd != nil && c.rpc.Err() == nil {
		return nil
	}

	if matched, _ := regexp.MatchString("^[a-zA-Z0-9]*$", c.Type); !matched {
		return fmt.Errorf("channel type must only contain a-zA-Z0-9, %q given", c.Type)
	}

	path := filepath.Join(pluginDir, c.Type)

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

	go forwardLogs(errPipe, c.Logger)

	if err = cmd.Start(); err != nil {
		return fmt.Errorf("failed to start cmd: %w", err)
	}
	c.Logger.Debug("Cmd started successfully")

	c.cmd = cmd
	c.rpc = rpc.NewRPC(writer, reader, c.Logger)

	if err = c.SetConfig(c.Config); err != nil {
		go c.terminate(c.cmd, c.rpc)

		c.cmd = nil
		c.rpc = nil

		return fmt.Errorf("failed to set config: %w", err)
	}
	c.Logger.Debugw("Successfully set config", zap.String("config", c.Config))

	return nil
}

func (c *Channel) ResetPlugin() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd == nil {
		c.Logger.Debug("channel has already been reset")
		return
	}

	go c.terminate(c.cmd, c.rpc)

	c.cmd = nil
	c.rpc = nil

	c.Logger.Debug("reset channel successfully")
}

func forwardLogs(errPipe io.Reader, logger *zap.SugaredLogger) {
	reader := bufio.NewReader(errPipe)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				logger.Errorw("Failed to read stderr line", zap.Error(err))
			}

			return
		}

		logger.Info(line)
	}
}

// run as go routine to terminate given channel
func (c *Channel) terminate(cmd *exec.Cmd, rpc *rpc.RPC) {
	c.Logger.Debug("terminating channel")
	_ = rpc.Close()

	timer := time.AfterFunc(5*time.Second, func() {
		c.Logger.Debug("killing the channel")
		_ = cmd.Process.Kill()
	})

	_ = cmd.Wait()
	timer.Stop()
	c.Logger.Debug("Channel terminated successfully")
}
