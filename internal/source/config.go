package source

import (
	"fmt"

	"github.com/icinga/icinga-go-library/config"
)

type Config struct {
	Type         string `yaml:"type" json:"type" env:"TYPE"`
	Name         string `yaml:"name" json:"name" env:"NAME"`
	Username     string `yaml:"username" json:"username" env:"USERNAME"`
	Password     string `yaml:"password" json:"password" env:"PASSWORD"`
	PasswordFile string `yaml:"password_file" json:"password_file" env:"PASSWORD_FILE"`
}

func (c *Config) Validate() error {
	if err := config.LoadPasswordFile(&c.Password, c.PasswordFile); err != nil {
		return err
	}

	if c.Type == "" {
		return fmt.Errorf("source.type is required when source configuration is set")
	}
	if c.Name == "" {
		return fmt.Errorf("source.name is required when source configuration is set")
	}
	if c.Username == "" {
		return fmt.Errorf("source.username is required when source configuration is set")
	}
	if c.Password == "" {
		return fmt.Errorf("source.password or source.password_file is required when source configuration is set")
	}

	return nil
}

func Validate(config []Config) error {
	seenUsernames := make(map[string]struct{}, len(config))
	for i := range config {
		if err := config[i].Validate(); err != nil {
			return fmt.Errorf("source.%d: %w", i, err)
		}
		if _, ok := seenUsernames[config[i].Username]; ok {
			return fmt.Errorf("source.%d: duplicate source username %q", i, config[i].Username)
		}
		seenUsernames[config[i].Username] = struct{}{}
	}

	return nil
}
