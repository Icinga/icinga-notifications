package channel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"go.uber.org/zap"
	"io"
	"sync"
)

type RPC struct {
	writer            io.WriteCloser
	writerMu          sync.Mutex
	reader            *bufio.Reader
	pendingRequests   map[string]chan plugin.JsonRpcResponse
	pendingRequestsMu sync.Mutex
	Logger            *zap.SugaredLogger
}

func newRPC(writer io.WriteCloser, reader io.Reader, logger *zap.SugaredLogger) *RPC {
	rpc := &RPC{
		writer:          writer,
		reader:          bufio.NewReader(reader),
		pendingRequests: map[string]chan plugin.JsonRpcResponse{},
		Logger:          logger,
	}

	go rpc.prepareResponse()

	return rpc
}

func (r *RPC) RawCall(method string, params json.RawMessage) (json.RawMessage, error) {
	id := uuid.New().String()
	req := plugin.JsonRpcRequest{Method: method, Params: params, Id: id}
	promise := make(chan plugin.JsonRpcResponse)

	r.pendingRequestsMu.Lock()
	r.pendingRequests[id] = promise
	r.pendingRequestsMu.Unlock()

	marshal, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("json.Marschal failed: %w", err)
	}

	line := string(marshal)
	r.writerMu.Lock()
	_, err = fmt.Fprintln(r.writer, line)
	r.writerMu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("failed to pass line to writer: %w", err)
	}
	r.Logger.Debugw("Successfully pass line to writer:", zap.String("line", line))

	response := <-promise

	if response.Error != "" {
		return nil, fmt.Errorf("plugin response contains error: %s", response.Error)
	}

	return response.Result, nil
}

func (r *RPC) prepareResponse() {
	for {
		var response = plugin.JsonRpcResponse{}
		res, err := r.reader.ReadString('\n')
		if err != nil {
			r.Logger.Warnw("failed to read response: ", zap.Error(err))
			continue
		}

		r.Logger.Debugw("Successfully read response", zap.String("output", res))

		if err = json.Unmarshal([]byte(res), &response); err != nil {
			r.Logger.Warnw("failed to decode json response:", zap.Error(err))
			continue
		}

		r.pendingRequestsMu.Lock()
		promise := r.pendingRequests[response.Id]
		delete(r.pendingRequests, response.Id)
		r.pendingRequestsMu.Unlock()

		if promise != nil {
			promise <- response
		} else {
			r.Logger.Warn("Unknown response")
		}
	}
}

func (r *RPC) Close() error {
	return r.writer.Close()
}
