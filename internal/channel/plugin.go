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
	"unicode/utf8"

	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/notifications"
	"github.com/icinga/icinga-go-library/notifications/jsonrpc"
	"github.com/icinga/icinga-go-library/notifications/plugin"
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
		ctx    context.Context
		cancel context.CancelFunc

		// ChannelID is the ID of the channel associated with this plugin supervisor.
		//
		// It is used to associate the plugin's state with the correct channel in the database.
		ChannelID int64
	}

	// rpcHandler handles the JSON-RPC requests made by any channel plugins to Icinga Notifications.
	rpcHandler struct {
		ps *pluginSupervisor
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

	ctx, cancel := context.WithCancel(ctx)
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
			defer func() {
				_ = r.Close()
				_ = w.Close()
			}()

			const maxBufSize = 512 * 1024
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

				default:
					// Plugin might literally be spamming stderr, so flush before we read more data to avoid excessive memory usage.
					flush()
					if err := r.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
						logger.Warnw("Failed to set read deadline for channel plugin stderr pipe", zap.Error(err))
						return
					}
					// Read up to 512KiB from the plugin's stderr pipe. This is a safeguard to prevent excessive memory
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
		cancel() // This will also cause the write end of the stderr pipe to be closed if we created new one for it.
		return nil, fmt.Errorf("failed to start channel plugin process: %w", err)
	}

	l := logger.With(zap.Int("pid", cmd.Process.Pid))
	l.Debug("Successfully started channel plugin process")

	ps := &pluginSupervisor{cmd: cmd, db: db, logger: l, ctx: ctx, cancel: cancel, ChannelID: chID}
	ps.rpc = jsonrpc.New(ctx, pr, pw, rpcHandler{ps: ps}, l)
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
	p.cancel()
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
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if states, err := getStateByChannelID(dbCtx, p.db, p.ChannelID); err != nil {
		return fmt.Errorf("cannot retrieve channel state: %w", err)
	} else {
		for _, s := range states {
			req.States = append(req.States, s.State)
		}
	}
	return p.rpc.Call(ctx, plugin.MethodSendNotification, req, nil)
}

// allowedLogLevels defines the set of log levels that are allowed to be sent from channel plugins to Icinga Notifications.
var allowedLogLevels = map[zapcore.Level]struct{}{
	zapcore.DebugLevel: {},
	zapcore.InfoLevel:  {},
	zapcore.WarnLevel:  {},
	zapcore.ErrorLevel: {},
}

// Handle handles the JSON-RPC requests made by any channel plugins to Icinga Notifications.
func (h rpcHandler) Handle(ctx context.Context, conn *jsonrpc.Conn, req *jsonrpc.Request) {
	switch req.Method {
	case notifications.MethodLog:
		if !req.Notif {
			msg := "channel::Log request must be a notification, but got a request with an ID"
			if err := jsonrpc.ReplyError(ctx, conn, req.ID, jsonrpc.CodeInvalidRequest, msg); err != nil {
				h.ps.logger.Errorw("Failed to send invalid request reply", zap.Error(err))
			}
			return
		}
		if !h.verifyCommon(ctx, conn, req, true) {
			return
		}

		var params jsonrpc.LogParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			h.ps.logger.Warnw("Failed to unmarshal received log params", zap.Error(err))
			return
		}
		if _, allowed := allowedLogLevels[params.Level]; !allowed {
			h.ps.logger.Warnw("Received invalid log level from plugin", zap.String("level", params.Level.String()))
			return
		}
		if params.Message == "" {
			h.ps.logger.Warnw("Received empty log message from plugin")
			return
		}
		h.ps.logger.Logw(params.Level, params.Message, params.Fields...)

	case notifications.MethodUpsertState:
		if !h.verifyCommon(ctx, conn, req, true) {
			return
		}

		stateRecords := h.deserializePluginState(ctx, conn, req)
		if stateRecords == nil {
			return // deserializePluginState already sent an error reply to the plugin.
		}

		dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		if err := upsertStates(dbCtx, h.ps.db, stateRecords); err != nil {
			h.ps.logger.Warnw("Failed to send upsert state error reply", zap.Error(err))
			if err := jsonrpc.ReplyError(ctx, conn, req.ID, jsonrpc.CodeInternalError, "failed to upsert state"); err != nil {
				h.ps.logger.Error("Failed to send upsert state error reply", zap.Error(err))
			}
			return
		}

		if err := conn.Reply(ctx, req.ID, nil); err != nil {
			h.ps.logger.Error("Failed to send upsert state success reply", zap.Error(err))
		}

	case notifications.MethodDeleteState:
		if !h.verifyCommon(ctx, conn, req, true) {
			return
		}
		stateRecords := h.deserializePluginState(ctx, conn, req)
		if stateRecords == nil {
			return // Already replied with an error in deserializePluginState.
		}

		dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		if err := deleteStates(dbCtx, h.ps.db, stateRecords); err != nil {
			h.ps.logger.Warnw("Failed to send delete-state error reply", zap.Error(err))
			if err := jsonrpc.ReplyError(ctx, conn, req.ID, jsonrpc.CodeInternalError, "failed to delete state"); err != nil {
				h.ps.logger.Error("Failed to send delete-state error reply", zap.Error(err))
			}
			return
		}

		if err := conn.Reply(ctx, req.ID, nil); err != nil {
			h.ps.logger.Error("Failed to send delete-state success reply", zap.Error(err))
		}

	default:
		if err := jsonrpc.ReplyMethodNotFound(ctx, conn, req.ID); err != nil {
			h.ps.logger.Errorw("Failed to send method not found reply", zap.Error(err))
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
			h.ps.logger.Warnw("Failed to send missing params error reply", zap.Error(err))
		}
		return false
	}

	upsertDel := req.Method == notifications.MethodUpsertState || req.Method == notifications.MethodDeleteState
	if upsertDel && (h.ps.ChannelID == 0 || h.ps.db == nil) {
		h.ps.logger.Warnf("Plugin called %s on a channel that is not fully initialized yet", req.Method)
		if err := jsonrpc.ReplyError(ctx, conn, req.ID, jsonrpc.CodeInternalError, "channel is not fully initialized"); err != nil {
			h.ps.logger.Warnw("Failed to send channel error reply", zap.Error(err))
		}
		return false
	}
	return true
}

// deserializePluginState deserializes the plugin state from the JSON-RPC request parameters.
//
// It returns a slice of State objects if successful, or nil if there was an error.
// If there was an error, it sends an appropriate error reply to the plugin.
func (h rpcHandler) deserializePluginState(ctx context.Context, conn *jsonrpc.Conn, req *jsonrpc.Request) []*State {
	replyErr := func(msg string) {
		if err := jsonrpc.ReplyError(ctx, conn, req.ID, jsonrpc.CodeInvalidParams, msg); err != nil {
			h.ps.logger.Warnw("Failed to send error reply", zap.String("method", req.Method), zap.Error(err))
		}
	}

	dec := json.NewDecoder(bytes.NewReader(*req.Params))
	dec.DisallowUnknownFields()
	if t, err := dec.Token(); err != nil {
		if !errors.Is(err, io.EOF) {
			replyErr(err.Error())
		}
		return nil
	} else if t != json.Delim('[') {
		replyErr("channel plugin state params must be a JSON array")
		return nil
	}

	var stateRecords []*State
	for dec.More() {
		state := &State{ChannelID: h.ps.ChannelID}
		if err := dec.Decode(state); err != nil {
			replyErr(err.Error())
			return nil
		}

		if state.Key == "" || utf8.RuneCountInString(state.Key) > maxStateKeyLen {
			replyErr(fmt.Sprintf("invalid state key, must be non-empty and at most %d chars, %q given", maxStateKeyLen, state.Key))
			return nil
		}

		if req.Method == notifications.MethodUpsertState && (state.Value == "" || utf8.RuneCountInString(state.Value) > maxStateValueLen) {
			replyErr(fmt.Sprintf("invalid state value, must be non-empty and at most %d chars, %q given", maxStateValueLen, state.Value))
			return nil
		}
		stateRecords = append(stateRecords, state)
	}

	if len(stateRecords) == 0 {
		replyErr("channel plugin state params must be a non-empty JSON array")
		return nil
	}
	return stateRecords
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

// pluginTypeValidateRegex defines Regexp with only allowed characters of the channel plugin type.
var pluginTypeValidateRegex = regexp.MustCompile("^[a-zA-Z0-9_-]+$")

// ValidateType returns an error if non-allowed chars are detected, nil otherwise.
func ValidateType(t string) error {
	if !pluginTypeValidateRegex.MatchString(t) {
		return fmt.Errorf("type contains invalid chars, may only contain a-zA-Z0-9_-, %q given", t)
	}

	if len(t) > 255 {
		return fmt.Errorf("type is too long, at most 255 chars allowed, %d given", len(t))
	}

	return nil
}
