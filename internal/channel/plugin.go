package channel

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
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
		ctx    context.Context
		cancel context.CancelFunc

		// ChannelID is the ID of the channel associated with this plugin supervisor.
		//
		// It is used to associate the plugin's state with the correct channel in the database.
		ChannelID int64

		// pluginType is the type of the plugin associated with this supervisor.
		pluginType string
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

	ps := &pluginSupervisor{cmd: cmd, db: db, logger: l, ctx: ctx, cancel: cancel, ChannelID: chID, pluginType: pluginType}
	ps.rpc = jsonrpc.New(ctx, pr, pw, rpcHandler{ps: ps}, l)
	return ps, nil
}

// Stop stops the plugin process and cleans up resources.
//
// It first sends a SIGTERM signal to the process, allowing it to exit gracefully. If the process does not
// terminate within a specified timeout, it forcefully kills the process. Afterward, it closes the JSON-RPC
// connection and cancels the context to clean up any remaining resources.
//
// It should be called only once, and after calling Stop, the pluginSupervisor should not be used again.
func (p *pluginSupervisor) Stop() {
	p.logger.Debug("Stopping channel plugin process")

	// Give the plugin a chance to clean up its resources and exit gracefully with the friendly
	// request (SIGTERM) before we forcefully kill it after a timeout.
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
	_ = p.rpc.Conn().Close()
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

// SendNotification sends the given notification request to the plugin.
//
// It returns the delivery result (if any) or an error if an error occurred during the RPC call.
func (p *pluginSupervisor) SendNotification(ctx context.Context, req *plugin.NotificationRequest) (*plugin.DeliveryResult, error) {
	var result *plugin.DeliveryResult
	if err := p.rpc.Call(ctx, plugin.MethodSendNotification, req, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ComposeStateKey generates a unique state key for the given contact and incident ID.
//
// The state key is generated by hashing the concatenation of the channel ID, incident ID, and all contact
// addresses using SHA-1, resulting in a globally unique identifier (UUID) v5. This ensures that the state
// key is unique for each combination of channel, incident, and contact addresses.
//
// If the contact has no addresses, a zero UUID is returned.
func (p *pluginSupervisor) ComposeStateKey(contact *plugin.Contact, incidentID int64) types.UUID {
	if len(contact.Addresses) == 0 {
		return types.UUID{}
	}

	var b bytes.Buffer
	for _, id := range []int64{p.ChannelID, incidentID} {
		_ = binary.Write(&b, binary.BigEndian, id)
	}

	addresses := slices.Clone(contact.Addresses)
	// Sort the addresses to ensure a consistent state key composition.
	slices.SortStableFunc(addresses, func(a, b *plugin.Address) int {
		return strings.Compare(a.Address, b.Address)
	})

	for _, addr := range addresses {
		b.Write([]byte{0})
		b.WriteString(addr.Address)
	}
	return types.MakeUUID(uuid.NewSHA1(uuid.Nil, b.Bytes()))
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
				h.ps.logger.Warnw("Failed to send invalid request reply", zap.Error(err))
			}
			return
		}

		if req.Params == nil {
			h.ps.logger.Warnw("Plugin sent a request without any parameters", zap.String("method", req.Method))
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

	default:
		if err := jsonrpc.ReplyMethodNotFound(ctx, conn, req.ID); err != nil {
			h.ps.logger.Warnw("Failed to send method not found reply", zap.Error(err))
		}
	}
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
