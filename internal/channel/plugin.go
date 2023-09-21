package channel

import (
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/pkg/plugin"
	"go.uber.org/zap"
	"os/exec"
)

type Plugin struct {
	config string
	path   string
	cmd    *exec.Cmd
	Logger *zap.SugaredLogger

	RPC *RPC
}

func (p *Plugin) GetInfo() (*plugin.Info, error) {
	result, err := p.RPC.RawCall("GetInfo", nil)
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
	if _, err := p.RPC.RawCall("SetConfig", json.RawMessage(config)); err != nil {
		return fmt.Errorf("RawCall failed: %w", err)
	}

	return nil
}

func (p *Plugin) SendNotification(req *plugin.NotificationRequest) error {
	params, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to encode request into json: %w", err)
	}

	_, err = p.RPC.RawCall("SendNotification", params)
	if err != nil {
		return fmt.Errorf("RawCall failed: %w", err)
	}

	return nil
}
