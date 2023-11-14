package channel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"github.com/icinga/icinga-notifications/pkg/rpc"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"go.uber.org/zap"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Plugin struct {
	cmd    *exec.Cmd
	rpc    *rpc.RPC
	logger *zap.SugaredLogger

	stopOnce sync.Once
}

// NewPlugin starts and returns a new plugin instance. If the start of the plugin fails, an error is returned
func NewPlugin(pluginType string, logger *zap.SugaredLogger) (*Plugin, error) {
	file := filepath.Join(daemon.Config().ChannelPluginDir, pluginType)

	logger.Debugw("Starting new channel plugin process", zap.String("path", file))

	cmd := exec.Command(file)

	started := false
	var childIOPipes []io.Closer
	var parentIOPipes []io.Closer
	defer func() {
		for _, pipe := range childIOPipes {
			_ = pipe.Close()
		}

		if !started {
			for _, pipe := range parentIOPipes {
				_ = pipe.Close()
			}
		}
	}()

	reqRead, reqWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create reqRead/reqWrite pipe: %w", err)
	}
	cmd.Stdin = reqRead
	childIOPipes = append(childIOPipes, reqRead)
	parentIOPipes = append(parentIOPipes, reqWrite)

	resRead, resWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create resRead/resWrite pipe: %w", err)
	}
	cmd.Stdout = resWrite
	childIOPipes = append(childIOPipes, resWrite)
	parentIOPipes = append(parentIOPipes, resRead)

	logRead, logWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create logRead/logWrite pipe: %w", err)
	}
	cmd.Stderr = logWrite
	childIOPipes = append(childIOPipes, logWrite)
	parentIOPipes = append(parentIOPipes, logRead)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start cmd: %w", err)
	}
	started = true

	l := logger.With(zap.Int("pid", cmd.Process.Pid))
	p := &Plugin{
		cmd:    cmd,
		rpc:    rpc.NewRPC(reqWrite, resRead, l),
		logger: l,
	}

	go forwardLogs(logRead, l)
	l.Debug("Successfully started channel plugin process")

	return p, nil
}

// Stop stops the plugin. Multiple calls are safe because sync.Once is used internally
func (p *Plugin) Stop() {
	p.stopOnce.Do(func() {
		go func() {
			p.logger.Debug("Requesting channel plugin stop")
			_ = p.rpc.Close()
			const timeout = 5 * time.Second
			timer := time.AfterFunc(timeout, func() {
				p.logger.Warnw("Channel plugin did not terminate after timeout, killing it", zap.Duration("timeout", timeout))
				_ = p.cmd.Process.Kill()
			})

			if err := p.cmd.Wait(); err != nil {
				p.logger.Errorw("Channel plugin stopped with an error", zap.Error(err))
			}
			timer.Stop()

			p.logger.Debug("Channel plugin terminated")
		}()
	})
}

func (p *Plugin) Pid() int {
	return p.cmd.Process.Pid
}

// GetInfo sends the PluginInfo request and returns the response or an error if an error occurred
func (p *Plugin) GetInfo() (*plugin.Info, error) {
	result, err := p.rpc.Call(plugin.MethodGetInfo, nil)
	if err != nil {
		return nil, err
	}

	info := &plugin.Info{}
	err = json.Unmarshal(result, info)
	if err != nil {
		return nil, err
	}

	return info, nil
}

// SetConfig sends the setConfig request with given config, returns an error if an error occurred
func (p *Plugin) SetConfig(config string) error {
	_, err := p.rpc.Call(plugin.MethodSetConfig, json.RawMessage(config))

	return err
}

// SendNotification sends the notification, returns an error if fails
func (p *Plugin) SendNotification(req *plugin.NotificationRequest) error {
	params, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to prepare request params: %w", err)
	}

	_, err = p.rpc.Call(plugin.MethodSendNotification, params)

	return err
}

func forwardLogs(errPipe io.Reader, logger *zap.SugaredLogger) {
	scanner := bufio.NewScanner(errPipe)
	for scanner.Scan() {
		logger.Info(scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		logger.Errorw("Failed to scan stderr line", zap.Error(err))
	}
}

// UpsertPlugins upsert the available_channel_type table with working plugins
func UpsertPlugins(channelPluginDir string, logger *logging.Logger, db *icingadb.DB) {
	logger.Debug("Updating available channel types")
	files, err := os.ReadDir(channelPluginDir)
	if err != nil {
		logger.Errorw("Failed to read the channel plugin directory", zap.Error(err))
	}

	var pluginInfos []*plugin.Info
	var pluginTypes []string

	for _, file := range files {
		pluginType := file.Name()
		pluginLogger := logger.With(zap.String("type", pluginType))
		if err := ValidateType(pluginType); err != nil {
			pluginLogger.Warnw("Ignoring plugin", zap.Error(err))
			continue
		}

		p, err := NewPlugin(pluginType, pluginLogger)
		if err != nil {
			pluginLogger.Errorw("Failed to start plugin", zap.Error(err))
			continue
		}

		info, err := p.GetInfo()
		if err != nil {
			p.logger.Error(err)
			p.Stop()
			continue
		}
		p.Stop()
		info.Type = pluginType

		pluginTypes = append(pluginTypes, pluginType)
		pluginInfos = append(pluginInfos, info)
	}

	if len(pluginInfos) == 0 {
		logger.Info("No working plugin found")
		return
	}

	stmt, _ := db.BuildUpsertStmt(&plugin.Info{})
	_, err = db.NamedExec(stmt, pluginInfos)
	if err != nil {
		logger.Errorw("Failed to update available channel types", zap.Error(err))
	} else {
		logger.Infof(
			"Successfully updated %d available channel types: %s",
			len(pluginInfos),
			strings.Join(pluginTypes, ", "))
	}
}

// pluginTypeValidateRegex defines Regexp with only allowed characters of the channel plugin type
var pluginTypeValidateRegex = regexp.MustCompile("^[a-zA-Z0-9]+$")

// ValidateType returns an error if non-allowed chars are detected, nil otherwise
func ValidateType(t string) error {
	if !pluginTypeValidateRegex.MatchString(t) {
		return fmt.Errorf("type contains invalid chars, may only contain a-zA-Z0-9, %q given", t)
	}

	return nil
}
