package channel

import (
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"
	"io"
	"sync"
	"testing"
)

func Test_newRPC(t *testing.T) {
	w, r := dummyRemote()
	logger := zaptest.NewLogger(t).Sugar()
	rpc := newRPC(w, r, logger)
	wg := sync.WaitGroup{}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			for j := 0; j < 1000; j++ {
				params := fmt.Sprintf(`{"go":"%d-%d"}`, i, j)

				res, err := rpc.RawCall("hello", json.RawMessage(params))
				if err != nil {
					panic(err)
				}

				t.Log(string(res))
				assert.Equal(t, params, string(res))

			}
			wg.Done()
		}(i)
	}
	wg.Wait()
}

func dummyRemote() (io.WriteCloser, io.Reader) {
	reqReader, reqWriter := io.Pipe()
	resReader, resWriter := io.Pipe()

	go func() {
		dec := json.NewDecoder(reqReader)
		enc := json.NewEncoder(resWriter)

		for {
			var req plugin.JsonRpcRequest
			err := dec.Decode(&req)
			if err != nil {
				panic(err)
			}

			var res plugin.JsonRpcResponse

			res.Id = req.Id
			res.Result = req.Params

			err = enc.Encode(&res)
			if err != nil {
				panic(err)
			}
		}
	}()

	return reqWriter, resReader
}
