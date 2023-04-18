package config

import (
	"github.com/creasty/defaults"
	"github.com/goccy/go-yaml"
	icingadbConfig "github.com/icinga/icingadb/pkg/config"
	"os"
)

type ConfigFile struct {
	Listen   string                  `yaml:"listen" default:"localhost:5680"`
	Database icingadbConfig.Database `yaml:"database"`
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
	return c.Database.Validate()
}