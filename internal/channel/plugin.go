package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/notifications"
	"github.com/icinga/icinga-go-library/notifications/jsonrpc"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/daemon"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type (
	// pluginSupervisor manages a plugin process and its JSON-RPC connection.
	pluginSupervisor struct {
		cmd    *exec.Cmd
		rpc    *jsonrpc.Endpoint
		db     *database.DB
		logger *zap.SugaredLogger

		// ChannelID is the ID of the channel associated with this plugin supervisor.
		//
		// It is used to associate the plugin's state with the correct channel in the database.
		ChannelID int64

		// highestSeenChangedAt is the highest ChangedAt timestamp seen for the channel's state.
		//
		// It is used to ensure that the plugin only receives state updates that are newer than the last seen state.
		highestSeenChangedAt types.UnixMilli
	}

	// rpcHandler handles the JSON-RPC requests made by any channel plugins to Icinga Notifications.
	rpcHandler struct {
		logger *zap.SugaredLogger
		ps     *pluginSupervisor
	}
)

// newPluginSupervisor starts a new plugin process for the given type and returns a pluginSupervisor to manage it.
func newPluginSupervisor(ctx context.Context, db *database.DB, logger *zap.SugaredLogger, pluginType string, chID int64) (*pluginSupervisor, error) {
	file := filepath.Join(daemon.Config().ChannelsDir, pluginType)

	logger.Debugw("Starting new channel plugin process", zap.String("path", file))

	cmd := exec.Command(file) //#nosec G204 -- plugins are launched from dynamic paths
	pw, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe for channel plugin process: %w", err)
	}

	pr, err := cmd.StdoutPipe()
	if err != nil {
		// This is a workaround for this bug https://github.com/golang/go/issues/58369.
		// This lets Start fail immediately and cleans up the stdin pipe acquired above, so that we don't leak fds.
		cmd.Err = errors.New("cmd should never start")
		cmd.Path = "" // Another safeguard (if there's no path to execute, then there's no way Start can succeed)
		_ = cmd.Start()
		return nil, fmt.Errorf("failed to create stdout pipe for channel plugin process: %w", err)
	}

	// Set up a pipe for the plugin's stderr to capture and log any crashes or error messages.
	// This is important because if the plugin crashes, we want to know why, and stderr is where
	// such messages are typically sent.
	cmd.Stderr = func() *os.File {
		r, w, err := os.Pipe()
		if err != nil {
			logger.Warnw("Failed to create pipe for channel plugin stderr", zap.Error(err))
			return os.Stderr
		}

		go func() {
			defer func() { _ = r.Close() }()

			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()

			const maxBufSize = 512 * 1024 // 512KiB
			buf := new(bytes.Buffer)
			flush := func() {
				if buf.Len() > 0 {
					logger.Errorw("Channel plugin stderr", zap.Int("pid", cmd.Process.Pid), zap.String("stderr", buf.String()))
					buf.Reset()
				}
			}

			for {
				select {
				case <-ctx.Done():
					return

				case <-ticker.C:
					flush()

				default:
					// Plugin might literally be spamming stderr, so flush before we read more data to avoid excessive memory usage.
					flush()
					if err := r.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
						logger.Warnw("Failed to set read deadline for channel plugin stderr pipe", zap.Error(err))
						return
					}
					// Read up to 512KB from the plugin's stderr pipe. This is a safeguard to prevent excessive memory
					// usage from malicious or misbehaving plugins that might write large amounts of data to stderr.
					if n, err := buf.ReadFrom(io.LimitReader(r, maxBufSize)); err != nil {
						if errors.Is(err, os.ErrDeadlineExceeded) {
							continue
						}
						logger.Errorw("Failed to read from channel plugin stderr pipe", zap.Error(err))
						return
					} else if n == 0 {
						// buf.ReadFrom is never supposed to return 0 bytes read unless we've reached io.EOF,
						// but that error is not returned by buf.ReadFrom, so treat it n == 0 as eof and exit the loop.
						flush()
						return
					}
				}
			}
		}()
		return w
	}()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start channel plugin process: %w", err)
	}

	l := logger.With(zap.Int("pid", cmd.Process.Pid))
	l.Debug("Successfully started channel plugin process")

	ps := &pluginSupervisor{cmd: cmd, db: db, logger: l, ChannelID: chID}
	ps.rpc = jsonrpc.New(ctx, pr, pw, rpcHandler{logger: l, ps: ps}, l)
	return ps, nil
}

// Stop stops the plugin process and cleans up resources.
//
// It first attempts to close the RPC connection and send a SIGTERM signal to the process, allowing it to
// exit gracefully. If the process does not terminate within a specified timeout, it forcefully kills the
// process. It should be called only once, and after calling Stop, the pluginSupervisor should not be used again.
func (p *pluginSupervisor) Stop() {
	p.logger.Debug("Stopping channel plugin process")

	// Give the plugin a chance to clean up its resources and exit gracefully with the two friendly
	// requests below (RPC-conn close and SIGTERM) before we forcefully kill it after a timeout.
	_ = p.rpc.Conn().Close()
	_ = p.cmd.Process.Signal(syscall.SIGTERM)

	const timeout = 5 * time.Second
	timer := time.AfterFunc(timeout, func() {
		p.logger.Warnw("Channel plugin did not exit gracefully, forcefully killing it", zap.Duration("timeout", timeout))
		_ = p.cmd.Process.Kill()
	})

	if err := p.cmd.Wait(); err != nil {
		p.logger.Errorw("Channel plugin stopped with an error", zap.Error(err))
	} else {
		p.logger.Infow("Channel plugin stopped successfully")
	}
	timer.Stop()
}

// GetInfo sends the PluginInfo request and returns the response or an error if an error occurred.
func (p *pluginSupervisor) GetInfo(ctx context.Context) (*plugin.Info, error) {
	info := new(plugin.Info)
	if err := p.rpc.Call(ctx, plugin.MethodGetInfo, nil, info); err != nil {
		return nil, err
	}
	return info, nil
}

// SetConfig sends the setConfig request with given config, returns an error if an error occurred.
func (p *pluginSupervisor) SetConfig(ctx context.Context, config string) error {
	return p.rpc.Call(ctx, plugin.MethodSetConfig, json.RawMessage(config), nil)
}

// SendNotification sends the notification, returns an error if fails.
func (p *pluginSupervisor) SendNotification(ctx context.Context, req *plugin.NotificationRequest) error {
	if state, err := getStateByChannelID(ctx, p.db, p.ChannelID, p.highestSeenChangedAt); err != nil {
		return fmt.Errorf("cannot retrieve channel state: %w", err)
	} else if len(state) > 0 {
		req.State = make(map[string]string, len(state))
		for _, s := range state {
			req.State[s.Key] = s.Value
			if s.ChangedAt.Time().After(p.highestSeenChangedAt.Time()) {
				p.highestSeenChangedAt = s.ChangedAt
			}
		}
	}
	return p.rpc.Call(ctx, plugin.MethodSendNotification, req, nil)
}

// handleUpsertState handles the upsert state request from the plugin and updates the database accordingly.
func (p *pluginSupervisor) handleUpsertState(ctx context.Context, conn *jsonrpc.Conn, req *jsonrpc.Request) {
	state := make(map[string]string)
	if err := json.Unmarshal(*req.Params, &state); err != nil {
		p.logger.Warnw("Failed to unmarshal upsert state params", zap.Error(err))
		if err := jsonrpc.ReplyError(ctx, conn, req.ID, jsonrpc.CodeInvalidRequest, "cannot unmarshal upsert state params"); err != nil {
			p.logger.Warnw("Failed to send upsert state error reply", zap.Error(err))
		}
		return
	}

	var stateRecords []*State
	for k, v := range state {
		if k == "" || len(k) > 255 {
			msg := fmt.Sprintf("state key is invalid, must be non-empty and at most 255 chars, %q given", k)
			if err := jsonrpc.ReplyError(ctx, conn, req.ID, jsonrpc.CodeInvalidRequest, msg); err != nil {
				p.logger.Warnw("Failed to send upsert state error reply", zap.Error(err))
				return
			}
		}

		stateRecords = append(stateRecords, &State{
			ChannelID: p.ChannelID,
			Key:       k,
			Value:     v,
			ChangedAt: types.UnixMilli(time.Now()),
		})
	}

	if err := upsertState(ctx, p.db, stateRecords...); err != nil {
		p.logger.Warnw("Failed to send upsert state error reply", zap.Error(err))
		if err := jsonrpc.ReplyError(ctx, conn, req.ID, jsonrpc.CodeInternalError, "failed to upsert state"); err != nil {
			p.logger.Error("Failed to send upsert state error reply", zap.Error(err))
		}
		return
	}

	if err := conn.Reply(ctx, req.ID, nil); err != nil {
		p.logger.Error("Failed to send upsert state success reply", zap.Error(err))
	}
}

// handleDeleteState handles the delete state request from the plugin and updates the database accordingly.
func (p *pluginSupervisor) handleDeleteState(ctx context.Context, conn *jsonrpc.Conn, req *jsonrpc.Request) {
	var stateKey string
	if err := json.Unmarshal(*req.Params, &stateKey); err != nil {
		p.logger.Warnw("Failed to unmarshal delete state params", zap.Error(err))
		if err := jsonrpc.ReplyError(ctx, conn, req.ID, jsonrpc.CodeInvalidRequest, "cannot unmarshal delete state params"); err != nil {
			p.logger.Error("Failed to send delete-state error reply", zap.Error(err))
		}
		return
	}

	if err := deleteStateByKey(ctx, p.db, p.ChannelID, stateKey); err != nil {
		p.logger.Warnw("Failed to send delete-state error reply", zap.Error(err))
		if err := jsonrpc.ReplyError(ctx, conn, req.ID, jsonrpc.CodeInternalError, "failed to delete state"); err != nil {
			p.logger.Error("Failed to send delete-state error reply", zap.Error(err))
		}
	}

	if err := conn.Reply(ctx, req.ID, nil); err != nil {
		p.logger.Error("Failed to send delete-state success reply", zap.Error(err))
	}
}

// Handle handles the JSON-RPC requests made by any channel plugins to Icinga Notifications.
func (h rpcHandler) Handle(ctx context.Context, conn *jsonrpc.Conn, req *jsonrpc.Request) {
	switch req.Method {
	case notifications.MethodLog:
		if !h.verifyCommon(ctx, conn, req, true) {
			return
		}

		var params jsonrpc.LogParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			h.logger.Warnw("Failed to unmarshal received log params", zap.Error(err))
			return
		}
		if params.Level == zapcore.InvalidLevel {
			h.logger.Warnw("Received invalid log level from plugin", zap.String("level", params.Level.String()))
			return
		}
		if params.Message == "" {
			h.logger.Warnw("Received empty log message from plugin")
			return
		}
		h.logger.Logw(params.Level, params.Message, params.Fields...)

	case notifications.MethodUpsertState:
		if h.verifyCommon(ctx, conn, req, true) {
			h.ps.handleUpsertState(ctx, conn, req)
		}

	case notifications.MethodDeleteState:
		if h.verifyCommon(ctx, conn, req, true) {
			h.ps.handleDeleteState(ctx, conn, req)
		}

	default:
		if err := jsonrpc.ReplyMethodNotFound(ctx, conn, req.ID); err != nil {
			h.logger.Error("Failed to send method not found reply", zap.Error(err))
		}
	}
}

// verifyCommon checks if the request has the required fields and other common validations.
//
// It returns true if the request is valid, false otherwise. If the request is invalid, it sends an
// appropriate error reply to the plugin.
func (h rpcHandler) verifyCommon(ctx context.Context, conn *jsonrpc.Conn, req *jsonrpc.Request, requireParams bool) bool {
	if requireParams && req.Params == nil {
		h.ps.logger.Warnw("Plugin sent invalid request parameters", zap.String("method", req.Method))
		if err := jsonrpc.ReplyMissingParams(ctx, conn, req.ID); err != nil {
			h.logger.Warnw("Failed to send missing params error reply", zap.Error(err))
		}
		return false
	}

	upsertDel := req.Method == notifications.MethodUpsertState || req.Method == notifications.MethodDeleteState
	if upsertDel && (h.ps.ChannelID == 0 || h.ps.db == nil) {
		h.ps.logger.Warnf("Plugin called %s on a channel that is not fully initialized yet", req.Method)
		if err := jsonrpc.ReplyError(ctx, conn, req.ID, jsonrpc.CodeInternalError, "channel is not fully initialized"); err != nil {
			h.logger.Warnw("Failed to send channel error reply", zap.Error(err))
		}
		return false
	}
	return true
}

// UpsertPlugins upsert the available_channel_type table with working plugins
func UpsertPlugins(ctx context.Context, channelPluginDir string, logger *logging.Logger, db *database.DB) {
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

		p, err := newPluginSupervisor(ctx, nil, pluginLogger, pluginType, 0)
		if err != nil {
			pluginLogger.Errorw("Failed to start plugin", zap.Error(err))
			continue
		}

		if info, err := p.GetInfo(ctx); err != nil {
			p.logger.Error(err)
		} else {
			info.Type = pluginType
			pluginTypes = append(pluginTypes, pluginType)
			pluginInfos = append(pluginInfos, info)
		}
		p.Stop()
	}

	if len(pluginInfos) == 0 {
		logger.Info("No working plugin found")
		return
	}

	stmt, _ := db.BuildUpsertStmt(&plugin.Info{})
	_, err = db.NamedExecContext(ctx, stmt, pluginInfos)
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

	if len(t) > 255 {
		return fmt.Errorf("type is too long, at most 255 chars allowed, %d given", len(t))
	}

	return nil
}
