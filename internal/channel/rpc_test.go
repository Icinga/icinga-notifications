package channel

import (
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"go.uber.org/zap"
	"io"
	"testing"
)

func Test_newRPC(t *testing.T) {
	w, r := dummyRemote()
	rpc := newRPC(w, r, zap.NewNop().Sugar())

	res, err := rpc.RawCall("hello", json.RawMessage(`{"foo":"bar"}`))
	if err != nil {
		panic(err)
	}

	t.Log(string(res))
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
			_, err = fmt.Fprintln(resWriter)
			if err != nil {
				panic(err)
			}
		}
	}()

	return reqWriter, resReader
}
