package channel

import (
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"go.uber.org/zap"
	"io"
	"sync"
)

type RPC struct {
	writer            io.Closer // use encoder for writing instead
	encoder           *json.Encoder
	encoderMu         sync.Mutex
	decoder           *json.Decoder
	pendingRequests   map[uint64]chan plugin.JsonRpcResponse
	pendingRequestsMu sync.Mutex
	logger            *zap.SugaredLogger
	lastRequestId     uint64
}

func newRPC(writer io.WriteCloser, reader io.Reader, logger *zap.SugaredLogger) *RPC {
	rpc := &RPC{
		writer:          writer,
		encoder:         json.NewEncoder(writer),
		decoder:         json.NewDecoder(reader),
		pendingRequests: map[uint64]chan plugin.JsonRpcResponse{},
		logger:          logger,
	}

	go rpc.processResponses()

	return rpc
}

func (r *RPC) RawCall(method string, params json.RawMessage) (json.RawMessage, error) {
	promise := make(chan plugin.JsonRpcResponse)

	r.pendingRequestsMu.Lock()
	r.lastRequestId++
	newId := r.lastRequestId
	r.pendingRequests[newId] = promise
	r.pendingRequestsMu.Unlock()

	req := plugin.JsonRpcRequest{Method: method, Params: params, Id: newId}

	r.encoderMu.Lock()
	err := r.encoder.Encode(req)
	r.encoderMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("json.Encode failed: %w", err)
	}

	response := <-promise

	if response.Error != "" {
		return nil, fmt.Errorf("plugin response contains error: %s", response.Error)
	}

	return response.Result, nil
}

func (r *RPC) processResponses() {
	for {
		var response plugin.JsonRpcResponse
		err := r.decoder.Decode(&response)
		if err != nil {
			r.logger.Warnw("failed to decode json response:", zap.Error(err))
			continue
		}

		r.pendingRequestsMu.Lock()
		promise := r.pendingRequests[response.Id]
		delete(r.pendingRequests, response.Id)
		r.pendingRequestsMu.Unlock()

		if promise != nil {
			promise <- response
		} else {
			r.logger.Warn("Unknown response")
		}
	}
}

func (r *RPC) Close() error {
	return r.writer.Close()
}
