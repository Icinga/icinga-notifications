package daemon

import (
	"errors"
	"github.com/creasty/defaults"
	"github.com/icinga/icinga-go-library/config"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/utils"
	"github.com/icinga/icinga-notifications/internal"
	"os"
	"time"
)

const (
	ExitSuccess = 0
	ExitFailure = 1
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

// Validate implements the config.Validator interface.
// Validates the entire daemon configuration on daemon startup.
func (c *ConfigFile) Validate() error {
	if err := c.Database.Validate(); err != nil {
		return err
	}
	if err := c.Logging.Validate(); err != nil {
		return err
	}

	return nil
}

// Assert interface compliance.
var (
	_ defaults.Setter  = (*ConfigFile)(nil)
	_ config.Validator = (*ConfigFile)(nil)
)

// Flags defines the CLI flags supported by Icinga Notifications.
type Flags struct {
	// Version decides whether to just print the version and exit.
	Version bool `long:"version" description:"print version and exit"`
	// Config is the path to the config file
	Config string `short:"c" long:"config" description:"path to config file"`
}

// ParseFlagsAndConfig parses the CLI flags provided to the executable and tries to load the config from the YAML file.
//
// Prints any error during parsing or config loading to os.Stderr and exits, otherwise returns the loaded ConfigFile.
func ParseFlagsAndConfig() *ConfigFile {
	flags := Flags{Config: internal.SysConfDir + "/icinga-notifications/config.yml"}
	if err := config.ParseFlags(&flags); err != nil {
		if errors.Is(err, config.ErrInvalidArgument) {
			panic(err)
		}

		utils.PrintErrorThenExit(err, ExitFailure)
	}

	if flags.Version {
		internal.Version.Print("Icinga Notifications")
		os.Exit(ExitSuccess)
	}

	daemonConfig := new(ConfigFile)
	if err := config.FromYAMLFile(flags.Config, daemonConfig); err != nil {
		if errors.Is(err, config.ErrInvalidArgument) {
			panic(err)
		}

		utils.PrintErrorThenExit(err, ExitFailure)
	}
	return daemonConfig
}
