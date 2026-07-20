package listener

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
)

// StreamOpt is a type safe option function that modifies the [StreamOpts] configuration for streaming JSON results.
type StreamOpt[T any] func(*StreamOpts[T])

// WithOnError sets the callback function to be called when an error occurs during streaming.
//
// The callback function receives the JSON encoder, a boolean indicating whether the HTTP header has already been
// sent, and the error that occurred. It can be used to log the error, send an error response to the client, or
// perform any other necessary actions. Once the callback is called, the streaming will be terminated with that error.
func WithOnError[T any](f OnErrFunc) StreamOpt[T] {
	return func(opts *StreamOpts[T]) { opts.onError = f }
}

// WithOnResult sets the callback function to be called for each input consumed from the result channel.
//
// The callback function should return the value to be sent to the client and an error if any occurred during
// processing. If an error is returned, it will be passed to the OnError callback, and the streaming will be
// terminated with that error. If no error and a nil value is returned, nothing will be sent to the client for
// that input, and the streaming will continue with the next input from the result channel.
func WithOnResult[T any](f func(T) (any, error)) StreamOpt[T] {
	return func(opts *StreamOpts[T]) { opts.onResult = f }
}

// WithSendHeaderEarly sets the option to send the HTTP header early before streaming results.
//
// This can be useful when the client needs to know that the request has been accepted and is being processed,
// even if no results have been sent yet. When this option is set, the HTTP header will be sent immediately without
// waiting for the first result to be available. If this option is not set, the header will be sent only when the
// first success result is ready to be sent to the client.
//
// Note that if an error occurs before any results are sent, OnError will be called, and the header must be sent with
// the appropriate error status code and message. If the header has already been sent, OnError will be called with
// wroteHeader set to true, and the error response should be sent in the body of the response.
func WithSendHeaderEarly[T any]() StreamOpt[T] {
	return func(opts *StreamOpts[T]) { opts.sendHeaderEarly = true }
}

// OnErrFunc is a function type that defines the signature for the error callback function.
type OnErrFunc func(enc *json.Encoder, wroteHeader *bool, err error)

// StreamOpts is a type safe struct that holds the configuration options for streaming JSON results.
type StreamOpts[T any] struct {
	onError         OnErrFunc
	onResult        func(T) (any, error)
	sendHeaderEarly bool
}

// StreamJsonResults streams JSON results from a result channel to an HTTP response writer.
//
// The function listens for results and errors, encodes the results as JSON, and writes them to the response writer
// in NDJSON format. If an error occurs, it calls the OnError callback and terminates the streaming. The [WithOnError]
// and [WithOnResult] options must be provided to handle errors and process results, respectively.
func StreamJsonResults[T any](ctx context.Context, rw http.ResponseWriter, inCh <-chan T, errCh <-chan error, opts ...StreamOpt[T]) error {
	with := new(StreamOpts[T])
	for _, option := range opts {
		option(with)
	}

	if with.onError == nil || with.onResult == nil {
		return stderrors.New("streamJsonResults: onError and onResult callbacks must be provided")
	}

	var wroteHeader bool
	sendSuccessHeader := func() {
		if !wroteHeader {
			// Set the Content-Type header to application/x-ndjson to indicate that the response will be
			// a stream of JSON objects, one object per line, in NDJSON (newline-delimited JSON) format.
			rw.Header().Add("Content-Type", "application/x-ndjson")
			rw.WriteHeader(http.StatusAccepted)
			wroteHeader = true
		}
	}

	if with.sendHeaderEarly {
		sendSuccessHeader()
	}
	// If we neither send an error nor a success response, make sure to send the success header when exiting.
	defer sendSuccessHeader()

	// Don't defer ctrl.Flush() here, because that will implicitly cause chunked encoding to be used,
	// which we do not want to always use as there's no need to for such trivial responses.
	ctrl := http.NewResponseController(rw)
	enc := json.NewEncoder(rw)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			if err != nil {
				with.onError(enc, &wroteHeader, err)
			}
			return err
		case result, ok := <-inCh:
			if !ok {
				err := <-errCh
				if err != nil {
					with.onError(enc, &wroteHeader, err)
				}
				return err
			}

			if res, err := with.onResult(result); err != nil {
				with.onError(enc, &wroteHeader, err)
				return err
			} else if res != nil {
				sendSuccessHeader()
				if err := enc.Encode(res); err != nil {
					return err
				}
				// The default and standard ResponseWriter does implement the [http.Flusher] interface, so flush it.
				if err := ctrl.Flush(); err != nil {
					return err
				}
			}
		}
	}
}
