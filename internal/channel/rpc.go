package channel

import (
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"go.uber.org/zap"
	"io"
	"sync"
)

type RPCError struct {
	cause error
}

type RPC struct {
	writer            io.Closer // use encoder for writing instead
	encoder           *json.Encoder
	encoderMu         sync.Mutex
	decoder           *json.Decoder
	pendingRequests   map[uint64]chan plugin.JsonRpcResponse
	pendingRequestsMu sync.Mutex
	logger            *zap.SugaredLogger
	lastRequestId     uint64
	mu                sync.Mutex

	errChannel chan error

	rpcErr *RPCError
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

	r.mu.Lock()
	r.lastRequestId++
	newId := r.lastRequestId
	r.pendingRequests[newId] = promise
	err := r.encoder.Encode(plugin.JsonRpcRequest{Method: method, Params: params, Id: newId})
	r.mu.Unlock()
	if err != nil {
		err = fmt.Errorf("failed to write request: %w", err)
		r.setRPCErr(err)

		return nil, err
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
			r.logger.Infof("dropping %d pending request(s)", len(r.pendingRequests))

			r.setRPCErr(fmt.Errorf("failed to decode json response: %w", err))

			return
		}

		r.mu.Lock()
		promise := r.pendingRequests[response.Id]
		delete(r.pendingRequests, response.Id)
		r.mu.Unlock()

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
		return r.rpcErr
	default:
		return nil
	}
}

func (r *RPC) setRPCErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.rpcErr == nil {
		r.rpcErr = &RPCError{cause: err}
		r.pendingRequests = nil
		close(r.errChannel)
	}
}

func (err *RPCError) Error() string {
	return fmt.Sprintf("RPCError: %s", err.cause.Error())
}

func (err *RPCError) Unwrap() error {
	return err.cause
}
