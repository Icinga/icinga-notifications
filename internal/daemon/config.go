package daemon

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/user"
	"strconv"

	"github.com/creasty/defaults"
	"github.com/icinga/icinga-go-library/config"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/utils"
	"github.com/icinga/icinga-notifications/internal"
)

const (
	ExitSuccess = 0
	ExitFailure = 1
)

type Mode fs.FileMode

func (m *Mode) UnmarshalText(text []byte) error {
	parsedString, err := strconv.ParseUint(string(text), 8, 32)

	if err != nil {
		return fmt.Errorf("invalid socket_mode %q: expected an octal value like 0660: %w", text, err)
	}
	*m = Mode(parsedString)

	return nil
}

// Listener defines the configuration for the Icinga Notifications API listener.
type Listener struct {
	Addr              string `yaml:"address" env:"ADDRESS"`
	Socket            string `yaml:"socket" env:"SOCKET"`
	SocketMode        *Mode  `yaml:"socket_mode" env:"SOCKET_MODE" default:"0600"`
	SocketGroup       string `yaml:"socket_group" env:"SOCKET_GROUP"`
	socketGid         string
	DebugPassword     string           `yaml:"debug_password" env:"DEBUG_PASSWORD"`
	DebugPasswordFile string           `yaml:"debug_password_file" env:"DEBUG_PASSWORD_FILE"`
	TLSOptions        config.TLSCommon `yaml:",inline"`
}

func (l *Listener) Validate() error {
	if err := config.LoadPasswordFile(&l.DebugPassword, l.DebugPasswordFile); err != nil {
		return err
	}

	if l.Socket != "" {
		path := l.Socket
		info, err := os.Stat(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cannot read socket path: %w", err)
		} else if err == nil {
			if info.Mode()&os.ModeSocket == 0 {
				return fmt.Errorf("the configured socket path already exists and is not a socket: %q", path)
			}
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("cannot remove existing unix socket: %w", err)
			}
		}

		if mode := l.SocketMode; mode != nil && *mode > 0o777 {
			return fmt.Errorf("the socket_mode \"%04o\" is too large (max 777)", mode)
		} else if mode != nil && *mode&0o666 == 0 {
			return fmt.Errorf(
				"socket_mode \"%04o\" grants no read/write access; the socket cannot accept connections",
				mode,
			)
		}

		if _, err := l.GetSocketGid(); err != nil {
			return err
		}
	} else if l.Addr == "" {
		// Only set the default Address for TCP server if no socket path is provided
		l.Addr = "localhost:5680"
	}

	if l.TLSOptions.Enable {
		if l.TLSOptions.Ca == "" {
			return errors.New("missing CA certificate")
		}

		tlsOpts := &config.ServerTLS{TLSCommon: l.TLSOptions, ClientAuth: config.TlsClientAuthType(tls.VerifyClientCertIfGiven)}
		if err := tlsOpts.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// GetTlsConfig returns a *[tls.Config] based on the TLS options specified in the Listener configuration.
func (l *Listener) GetTlsConfig() (*tls.Config, error) {
	tlsOpts := &config.ServerTLS{TLSCommon: l.TLSOptions, ClientAuth: config.TlsClientAuthType(tls.VerifyClientCertIfGiven)}
	return tlsOpts.MakeConfig()
}

// GetSocketGid returns the GID of the configured SocketGroup, or -1 if no group is set.
func (l *Listener) GetSocketGid() (gid int, err error) {
	if l.socketGid == "" {
		if groupName := l.SocketGroup; groupName != "" {
			group, err := user.LookupGroup(groupName)
			if err != nil {
				return -1, fmt.Errorf("cannot find group %q: %w", groupName, err)
			}

			if gid, err = strconv.Atoi(group.Gid); err != nil {
				return -1, fmt.Errorf("cannot parse GID for group %q: %w", groupName, err)
			}

			l.socketGid = group.Gid
		} else {
			return -1, nil
		}
	} else {
		gid, err = strconv.Atoi(l.socketGid)
		if err != nil {
			return -1, fmt.Errorf("cannot parse GID %q: %w", l.socketGid, err)
		}
	}

	return gid, nil
}

type ConfigFile struct {
	ChannelsDir   string          `yaml:"channels_dir" env:"CHANNELS_DIR"`
	Icingaweb2URL string          `yaml:"icingaweb2_url" env:"ICINGAWEB2_URL"`
	Listener      Listener        `yaml:"listener" envPrefix:"LISTENER_"`
	Database      database.Config `yaml:"database" envPrefix:"DATABASE_"`
	Logging       logging.Config  `yaml:"logging" envPrefix:"LOGGING_"`

	// IcingaWeb2UrlParsed holds the parsed Icinga Web 2 URL after validation of the config file.
	//
	// This field is not part of the YAML config and is only populated after successful validation.
	// The resulting URL always ends with a trailing slash, making it easier to resolve relative paths against it.
	IcingaWeb2UrlParsed *url.URL
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
	if err := c.Listener.Validate(); err != nil {
		return err
	}
	if err := c.Database.Validate(); err != nil {
		return err
	}
	if err := c.Logging.Validate(); err != nil {
		return err
	}

	if c.Icingaweb2URL == "" {
		return errors.New("icingaweb2_url must be set")
	}

	parsedUrl, err := url.Parse(c.Icingaweb2URL)
	if err != nil {
		return fmt.Errorf("invalid icingaweb2_url: %w", err)
	}

	if !parsedUrl.IsAbs() {
		return errors.New("icingaweb2_url must be an absolute URL")
	}

	parsedUrl.RawQuery = "" // Ignore query params if provided, as they are not relevant for resolving event URLs
	// Ensure the URL ends with a trailing slash for easier resolution of relative paths.
	c.IcingaWeb2UrlParsed = parsedUrl.JoinPath("/")

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
