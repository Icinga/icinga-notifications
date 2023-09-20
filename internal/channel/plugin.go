package channel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"go.uber.org/zap"
	"io"
	"os/exec"
)

type Plugin struct {
	config string
	path   string
	cmd    *exec.Cmd
	writer io.WriteCloser
	reader *bufio.Reader
	Logger *zap.SugaredLogger
}

func (p *Plugin) GetInfo() (*plugin.Info, error) {
	result, err := p.RawCall("GetInfo", nil)
	if err != nil {
		return nil, fmt.Errorf("RawCall failed: %w", err)
	}

	info := &plugin.Info{}
	err = json.Unmarshal(result, info)
	if err != nil {
		return nil, err
	}

	return info, nil
}

func (p *Plugin) SetConfig(config string) error {
	if _, err := p.RawCall("SetConfig", json.RawMessage(config)); err != nil {
		return fmt.Errorf("RawCall failed: %w", err)
	}

	return nil
}

func (p *Plugin) SendNotification(req *plugin.NotificationRequest) error {
	params, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to encode request into json: %w", err)
	}

	_, err = p.RawCall("SendNotification", params)
	if err != nil {
		return fmt.Errorf("RawCall failed: %w", err)
	}

	return nil
}

func (p *Plugin) RawCall(method string, params json.RawMessage) (json.RawMessage, error) {
	req := plugin.JsonRpcRequest{Method: method, Params: params, Id: uuid.New().String()}
	marshal, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("json.Marschal failed: %w", err)
	}

	line := string(marshal)
	if _, err = fmt.Fprintln(p.writer, line); err != nil {
		return nil, fmt.Errorf("failed to pass line to writer: %w", err)
	}
	p.Logger.Debugw("Successfully pass line to writer:", zap.String("line", line))

	res, err := p.reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read responce: %w", err)
	}

	p.Logger.Debugw("Successfully read response", zap.String("output", res))

	var response = plugin.JsonRpcResponse{}
	if err = json.Unmarshal([]byte(res), &response); err != nil {
		return nil, fmt.Errorf("failed to decode json response: %w", err)
	} else if response.Error != "" {
		return nil, fmt.Errorf("plugin response contains error: %s", response.Error)
	}

	return response.Result, nil
}
