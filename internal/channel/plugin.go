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
	mu     sync.Mutex
	logger *zap.SugaredLogger
}

// NewPlugin starts and returns a new plugin instance. If the start of the plugin fails, an error is returned
func NewPlugin(pluginType string, logger *zap.SugaredLogger) (*Plugin, error) {
	p := &Plugin{logger: logger}

	p.mu.Lock()
	defer p.mu.Unlock()

	file := filepath.Join(daemon.Config().ChannelPluginDir, pluginType)
	cmd := exec.Command(file)

	writer, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	reader, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	errPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start cmd: %w", err)
	}
	p.logger.Info("Successfully started channel plugin")

	go forwardLogs(errPipe, p.logger)

	p.cmd = cmd
	p.rpc = rpc.NewRPC(writer, reader, p.logger)

	return p, nil
}

// Stop stops the plugin
func (p *Plugin) Stop() {
	go terminate(p)
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

func terminate(p *Plugin) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.logger.Debug("Stopping the channel plugin")

	_ = p.rpc.Close()
	timer := time.AfterFunc(5*time.Second, func() {
		p.logger.Debug("killing the channel plugin")
		_ = p.cmd.Process.Kill()
	})

	<-p.rpc.Done()
	timer.Stop()
	p.logger.Warn("Stopped channel plugin")
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
	logger.Infof("Upserting working plugins")
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
		logger.Errorw("Failed to upsert channel plugins", zap.Error(err))
	} else {
		logger.Infof(
			"Successfully upserted %d plugins: %s",
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
