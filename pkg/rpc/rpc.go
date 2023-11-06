package rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"go.uber.org/zap"
	"io"
	"sync"
)

type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Id     uint64          `json:"id"`
}

type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	Id     uint64          `json:"id"`
}

type Error struct {
	cause error
}

func (err *Error) Error() string {
	return fmt.Sprintf("RPC error: %s", err.cause.Error())
}

func (err *Error) Unwrap() error {
	return err.cause
}

type RPC struct {
	writer    io.Closer // use encoder for writing instead
	encoder   *json.Encoder
	encoderMu sync.Mutex

	decoder *json.Decoder
	logger  *zap.SugaredLogger

	pendingRequests map[uint64]chan Response
	lastRequestId   uint64
	requestsMu      sync.Mutex

	errChannel        chan struct{} // never transports a value, only closed through setErr() to signal an occurred error
	err               *Error        // only initialized via setErr(), if a rpc (Fatal/non-recoverable) error has occurred
	errOnce           sync.Once
	requestedShutdown bool
}

// NewRPC creates and returns an RPC instance
func NewRPC(writer io.WriteCloser, reader io.Reader, logger *zap.SugaredLogger) *RPC {
	rpc := &RPC{
		writer:          writer,
		encoder:         json.NewEncoder(writer),
		decoder:         json.NewDecoder(reader),
		pendingRequests: map[uint64]chan Response{},
		logger:          logger,
		errChannel:      make(chan struct{}),
	}

	go rpc.processResponses()

	return rpc
}

// Call sends a request with given parameters.
// Returns the Response.Result or an error.
//
// Two different kinds of error can be returned:
//   - rpc.Error: Communication failed and future calls on this instance won't work and a new *RPC has to be created.
//   - Response.Error: The response contains an error (that's non-fatal for the RPC object).
func (r *RPC) Call(method string, params json.RawMessage) (json.RawMessage, error) {
	if err := r.Err(); err != nil {
		return nil, err
	}

	promise := make(chan Response, 1)

	r.requestsMu.Lock()
	r.lastRequestId++
	newId := r.lastRequestId
	r.pendingRequests[newId] = promise
	r.requestsMu.Unlock()

	r.encoderMu.Lock()
	err := r.encoder.Encode(Request{Method: method, Params: params, Id: newId})
	r.encoderMu.Unlock()
	if err != nil {
		err = fmt.Errorf("failed to write request: %w", err)
		r.setErr(err)

		return nil, r.Err()
	}

	select {
	case response := <-promise:
		if response.Error != "" {
			return nil, errors.New(response.Error)
		}

		return response.Result, nil

	case <-r.Done():
		return nil, r.Err()
	}
}

// Err returns a non-nil error, If Done sends. Otherwise, nil is returned
func (r *RPC) Err() error {
	select {
	case <-r.Done():
		return r.err
	default:
		return nil
	}
}

// Done sends when the errChannel has been closed.
// errChannel is closed when encoder/decoder fails to write/read
func (r *RPC) Done() <-chan struct{} {
	return r.errChannel
}

// Close closes the RPC.writer.
// Further requests leads encoder/decoder to fail
func (r *RPC) Close() error {
	r.encoderMu.Lock()
	defer r.encoderMu.Unlock()

	return r.writer.Close()
}

// setErr sets the err encoder/decoder fails and closes errChannel
func (r *RPC) setErr(err error) {
	r.errOnce.Do(func() {
		if reqCount := len(r.pendingRequests); reqCount > 0 {
			r.logger.Debugf("Cancelling %d pending request(s)", reqCount)
		}

		r.err = &Error{cause: err}
		close(r.errChannel)
	})
}

// processResponses sends responses to its channel (identified by response.id)
// In case of any error, all pending requests are dropped
func (r *RPC) processResponses() {
	for r.Err() == nil {
		var response Response
		err := r.decoder.Decode(&response)

		if err != nil {
			r.setErr(fmt.Errorf("failed to read json response: %w", err))

			return
		}

		r.requestsMu.Lock()
		promise := r.pendingRequests[response.Id]
		delete(r.pendingRequests, response.Id)
		r.requestsMu.Unlock()

		if promise != nil {
			promise <- response
		} else {
			r.logger.Warn("Ignored response for unknown ID:", response.Id)
		}
	}
}
