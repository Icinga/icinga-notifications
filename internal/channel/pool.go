package channel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/contracts"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icingadb/pkg/logging"
	"go.uber.org/zap"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

type Channel struct {
	ID     int64  `db:"id"`
	Name   string `db:"name"`
	Type   string `db:"type"`
	Config string `db:"config" json:"-"` // excluded from JSON config dump as this may contain sensitive information
}

type Plugin struct {
	config string
	path   string
	cmd    *exec.Cmd
	writer io.WriteCloser
	reader *bufio.Reader
	Logger *logging.Logger
}

type Pool struct {
	plugins map[string]*Plugin
	Dir     string
	Logger  *logging.Logger
}

/*func dead() {
	ch := ChannelPool{Dir: "/usr/libexec/icinga-notifications/channel"}

	emails := []string{"{email: aa@aa.com}", "{email: bb@bb.com}"}
	sms := []string{"{phone: 123}", "{phone: 9988}"}

	pluginCollector := map[string][]string{
		"email": emails,
		"sms":   sms,
	}

	for pluginType, values := range pluginCollector {
		for _, line := range values {
			log.Println(ch.Run(pluginType, line))
		}
	}
}*/

func (n *Plugin) execPlugin(line string) error {
	logger := n.Logger.With(zap.String("path", n.path))
	if n.cmd == nil {
		cmd := exec.Command(n.path)

		writer, err := cmd.StdinPipe()
		if err != nil {
			return fmt.Errorf("stdin pipe failed: %w", err)
		}

		logger.Debug("stdin pipe pass")

		tempReader, err := cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("stdout pipe failed: %w", err)
		}

		logger.Debug("stdout pipe pass")

		reader := bufio.NewReader(tempReader)

		cmd.Stderr = os.Stderr

		err = cmd.Start()
		if err != nil {
			return fmt.Errorf("cmd start failed: %w", err)
		}
		logger.Debug("cmd start pass")

		_, err = fmt.Fprintln(writer, n.config)
		if err != nil {
			return fmt.Errorf("writing config failed: %w", err)
		}
		logger.Debug("writing config pass")

		n.cmd = cmd
		n.writer = writer
		n.reader = reader
	}

	_, err := fmt.Fprintln(n.writer, line)
	if err != nil {
		return fmt.Errorf("writing line failed: %w", err)
	}
	logger.Debugw("writing line pass", zap.String("line", line))

	res, err := n.reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading output failed: %w", err)
	}
	logger.Debugw("reading output pass", zap.String("output", res))

	var response struct {
		Success bool
		Error   error
	}
	err = json.Unmarshal([]byte(res), &response)
	if err != nil {
		return fmt.Errorf("cant decode json response: %w", err)
	} else if !response.Success {
		return fmt.Errorf("plugin reported error: %w", response.Error)
	}

	return nil
}

func (c *Pool) Run(pluginType string, config string, line string) error {
	if c.plugins == nil {
		c.plugins = map[string]*Plugin{}
	}

	if c.plugins[pluginType] == nil {
		c.plugins[pluginType] = &Plugin{config: config, path: filepath.Join(c.Dir, pluginType), Logger: c.Logger}
	}

	return c.plugins[pluginType].execPlugin(line)
}

func CreateJson(contact *recipient.Contact, incident contracts.Incident, event *event.Event, icingaweb2Url string) string {
	i := Incident{incident.ID(), incident.ObjectDisplayName()}
	marshal, err := json.Marshal(NotificationRequest{Contact: contact, Incident: i, Event: event, IcingaWeb2Url: icingaweb2Url})
	if err != nil {
		log.Println(err)
	}

	return string(marshal)
}
