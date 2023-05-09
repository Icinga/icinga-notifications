package config

import (
	"github.com/creasty/defaults"
	"github.com/goccy/go-yaml"
	icingadbConfig "github.com/icinga/icingadb/pkg/config"
	"os"
)

type ConfigFile struct {
	Listen        string                  `yaml:"listen" default:"localhost:5680"`
	DebugPassword string                  `yaml:"debug-password"`
	Icingaweb2URL string                  `yaml:"icingaweb2-url"`
	Database      icingadbConfig.Database `yaml:"database"`
	Logging       icingadbConfig.Logging  `yaml:"logging"`
}

func FromFile(path string) (*ConfigFile, error) {
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
