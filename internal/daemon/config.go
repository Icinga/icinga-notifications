package daemon

import (
	"errors"
	"github.com/creasty/defaults"
	"github.com/goccy/go-yaml"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-notifications/internal"
	"os"
	"time"
)

type ConfigFile struct {
	Listen        string          `yaml:"listen" default:"localhost:5680"`
	DebugPassword string          `yaml:"debug-password"`
	ChannelsDir   string          `yaml:"channels-dir"`
	ApiTimeout    time.Duration   `yaml:"api-timeout" default:"1m"`
	Icingaweb2URL string          `yaml:"icingaweb2-url"`
	Database      database.Config `yaml:"database"`
	Logging       logging.Config  `yaml:"logging"`
}

// SetDefaults implements the defaults.Setter interface.
func (c *ConfigFile) SetDefaults() {
	if defaults.CanUpdate(c.ChannelsDir) {
		c.ChannelsDir = internal.LibExecDir + "/icinga-notifications/channels"
	}
}

// Assert interface compliance.
var _ defaults.Setter = (*ConfigFile)(nil)

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
