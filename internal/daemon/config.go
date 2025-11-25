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
)

const (
	ExitSuccess = 0
	ExitFailure = 1
)

type ConfigFile struct {
	Listen        string          `yaml:"listen" env:"LISTEN" default:"localhost:5680"`
	DebugPassword string          `yaml:"debug-password" env:"DEBUG_PASSWORD"`
	ChannelsDir   string          `yaml:"channels-dir" env:"CHANNELS_DIR"`
	Icingaweb2URL string          `yaml:"icingaweb2-url" env:"ICINGAWEB2_URL"`
	Database      database.Config `yaml:"database" envPrefix:"DATABASE_"`
	Logging       logging.Config  `yaml:"logging" envPrefix:"LOGGING_"`
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

// GetConfigPath implements [config.Flags].
func (f Flags) GetConfigPath() string {
	if f.Config == "" {
		return internal.SysConfDir + "/icinga-notifications/config.yml"
	}

	return f.Config
}

// IsExplicitConfigPath implements [config.Flags].
func (f Flags) IsExplicitConfigPath() bool {
	return f.Config != ""
}

// daemonConfig holds the configuration state as a singleton.
// It is initialised by the ParseFlagsAndConfig func and exposed through the Config function.
var daemonConfig *ConfigFile

// Config returns the config that was loaded while starting the daemon.
// Panics when ParseFlagsAndConfig was not called earlier.
func Config() *ConfigFile {
	if daemonConfig == nil {
		panic("ERROR: daemon.Config() called before daemon.ParseFlagsAndConfig()")
	}

	return daemonConfig
}

// ParseFlagsAndConfig parses the CLI flags provided to the executable and tries to load the config from the YAML file.
// Prints any error during parsing or config loading to os.Stderr and exits.
func ParseFlagsAndConfig() {
	var flags Flags
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

	daemonConfig = new(ConfigFile)
	if err := config.Load(daemonConfig, config.LoadOptions{
		Flags:      flags,
		EnvOptions: config.EnvOptions{Prefix: "ICINGA_NOTIFICATIONS_"},
	}); err != nil {
		if errors.Is(err, config.ErrInvalidArgument) {
			panic(err)
		}

		utils.PrintErrorThenExit(err, ExitFailure)
	}
}
