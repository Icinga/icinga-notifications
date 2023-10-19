package daemon

import (
	"errors"
	"github.com/creasty/defaults"
	"github.com/goccy/go-yaml"
	icingadbConfig "github.com/icinga/icingadb/pkg/config"
	"os"
)

type Icinga2ApiConfig struct {
	NotificationsEventSourceId int64  `yaml:"notifications-event-source-id"`
	Host                       string `yaml:"host"`
	AuthUser                   string `yaml:"auth-user"`
	AuthPass                   string `yaml:"auth-pass"`
	InsecureTls                bool   `yaml:"insecure-tls"`
}

type ConfigFile struct {
	Listen           string                  `yaml:"listen" default:"localhost:5680"`
	DebugPassword    string                  `yaml:"debug-password"`
	ChannelPluginDir string                  `yaml:"channel-plugin-dir" default:"/usr/libexec/icinga-notifications/channel"`
	Icingaweb2URL    string                  `yaml:"icingaweb2-url"`
	Icinga2Apis      []Icinga2ApiConfig      `yaml:"icinga2-apis"`
	Database         icingadbConfig.Database `yaml:"database"`
	Logging          icingadbConfig.Logging  `yaml:"logging"`
}

// config holds the configuration state as a singleton. It is used from LoadConfig and Config
var config *ConfigFile

// LoadConfig loads the daemon config from given path. Call it only once when starting the daemon.
func LoadConfig(path string) error {
	if config != nil {
		return errors.New("config already set")
	}

	cfg, err := fromFile(path)
	if err != nil {
		return err
	}

	config = cfg

	return nil
}

// Config returns the config that was loaded while starting the daemon
func Config() *ConfigFile {
	return config
}

func fromFile(path string) (*ConfigFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var c ConfigFile

	if err := defaults.Set(&c); err != nil {
		return nil, err
	}

	d := yaml.NewDecoder(f)
	if err := d.Decode(&c); err != nil {
		return nil, err
	}

	if err := c.Validate(); err != nil {
		return nil, err
	}

	return &c, nil
}

func (c *ConfigFile) Validate() error {
	if err := c.Database.Validate(); err != nil {
		return err
	}
	if err := c.Logging.Validate(); err != nil {
		return err
	}

	return nil
}
