package channel

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"go.uber.org/zap"
	"io"
	"sync"
)

var ErrRpcFailed = errors.New("RPC failed")

type RPC struct {
	writer            io.Closer // use encoder for writing instead
	encoder           *json.Encoder
	encoderMu         sync.Mutex
	decoder           *json.Decoder
	pendingRequests   map[uint64]chan plugin.JsonRpcResponse
	pendingRequestsMu sync.Mutex
	logger            *zap.SugaredLogger
	lastRequestId     uint64

	errChannel chan error
}

func newRPC(writer io.WriteCloser, reader io.Reader, logger *zap.SugaredLogger) *RPC {
	rpc := &RPC{
		writer:          writer,
		encoder:         json.NewEncoder(writer),
		decoder:         json.NewDecoder(reader),
		pendingRequests: map[uint64]chan plugin.JsonRpcResponse{},
		logger:          logger,
		errChannel:      make(chan error, 1),
	}

	go rpc.processResponses()

	return rpc
}

func (r *RPC) Call(method string, params json.RawMessage) (json.RawMessage, error) {
	if err := r.Err(); err != nil {
		return nil, err
	}

	promise := make(chan plugin.JsonRpcResponse)

	r.pendingRequestsMu.Lock()
	r.lastRequestId++
	newId := r.lastRequestId
	r.pendingRequests[newId] = promise
	r.pendingRequestsMu.Unlock()

	r.encoderMu.Lock()
	err := r.encoder.Encode(plugin.JsonRpcRequest{Method: method, Params: params, Id: newId})
	r.encoderMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("json.Encode failed: %w", err)
	}

	select {
	case response := <-promise:
		if response.Error != "" {
			return nil, fmt.Errorf("plugin response contains error: %s", response.Error)
		}

		return response.Result, nil

	case <-r.errChannel:
		return nil, r.Err()
	}
}

func (r *RPC) processResponses() {
	for {
		var response plugin.JsonRpcResponse
		err := r.decoder.Decode(&response)

		if err != nil {
			r.logger.Warnw("failed to decode json response:", zap.Error(err))
			close(r.errChannel)

			return
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

func (r *RPC) Err() error {
	select {
	case <-r.errChannel:
		return ErrRpcFailed
	default:
		return nil
	}
}
