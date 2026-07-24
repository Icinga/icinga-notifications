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
	"time"

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

// Mode is a Unix file permission mode parsed from an octal string in YAML or environment configuration.
type Mode fs.FileMode

// UnmarshalText implements [encoding.TextUnmarshaler] for Mode, parsing an octal string (e.g. "0660") into a file mode.
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
	Addr              string           `yaml:"address" env:"ADDRESS"`
	Socket            string           `yaml:"socket" env:"SOCKET"`
	SocketMode        *Mode            `yaml:"socket_mode" env:"SOCKET_MODE" default:"0660"`
	SocketGroup       string           `yaml:"socket_group" env:"SOCKET_GROUP"`
	DebugPassword     string           `yaml:"debug_password" env:"DEBUG_PASSWORD"`
	DebugPasswordFile string           `yaml:"debug_password_file" env:"DEBUG_PASSWORD_FILE"`
	TLSOptions        config.TLSCommon `yaml:",inline"`
	CrlFile           string           `yaml:"crl_file" env:"CRL_FILE"`
}

func (l *Listener) Validate() error {
	if err := config.LoadPasswordFile(&l.DebugPassword, l.DebugPasswordFile); err != nil {
		return err
	}

	if l.Socket != "" {
		if mode := l.SocketMode; mode != nil && *mode > 0o777 {
			return fmt.Errorf("the socket_mode \"%04o\" is too large (max 777)", mode)
		} else if mode != nil && *mode&0o666 == 0 {
			return fmt.Errorf(
				"socket_mode \"%04o\" grants no read/write access; the socket cannot accept connections",
				mode,
			)
		}

		if groupName := l.SocketGroup; groupName != "" {
			group, err := user.LookupGroup(groupName)
			if err != nil {
				return fmt.Errorf("cannot find group %q: %w", groupName, err)
			}

			if _, err = strconv.Atoi(group.Gid); err != nil {
				return fmt.Errorf("cannot parse GID for group %q: %w", groupName, err)
			}
		}
	} else if l.Addr == "" {
		// Only set the default Address for TCP server if no socket path is provided
		l.Addr = "localhost:5680"
	}

	if l.TLSOptions.Enable {
		if l.TLSOptions.Ca == "" {
			return errors.New("missing CA certificate")
		}

		tlsOpts := &config.ServerTLS{
			TLSCommon:  l.TLSOptions,
			ClientAuth: config.TlsClientAuthType(tls.VerifyClientCertIfGiven),
			CrlFile:    l.CrlFile,
		}
		if err := tlsOpts.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// GetTlsConfig returns a *[tls.Config] and a *[config.CrlChecker] based on the TLS options
// specified in the Listener configuration.
// The boolean return value indicates whether revocation checking is enabled (true) or not (false).
func (l *Listener) GetTlsConfig() (*tls.Config, *config.CrlChecker, bool, error) {
	tlsOpts := &config.ServerTLS{
		TLSCommon:  l.TLSOptions,
		ClientAuth: config.TlsClientAuthType(tls.VerifyClientCertIfGiven),
		CrlFile:    l.CrlFile,
	}
	tlsConfig, err := tlsOpts.MakeConfig()
	if err != nil {
		return nil, nil, false, err
	}

	if l.TLSOptions.Enable && tlsOpts.CrlFile != "" {
		crlChecker, err := tlsOpts.InitRevocationChecking(tlsConfig)
		if err != nil {
			return nil, nil, false, err
		}

		return tlsConfig, crlChecker, true, nil
	}

	return tlsConfig, nil, false, nil
}

type ConfigFile struct {
	ChannelsDir   string          `yaml:"channels_dir" env:"CHANNELS_DIR"`
	Icingaweb2URL string          `yaml:"icingaweb2_url" env:"ICINGAWEB2_URL"`
	Listener      Listener        `yaml:"listener" envPrefix:"LISTENER_"`
	Database      database.Config `yaml:"database" envPrefix:"DATABASE_"`
	Retention     Retention       `yaml:"retention" envPrefix:"RETENTION_"`
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
	if err := c.Retention.Validate(); err != nil {
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

// RetentionOpts defines additional overrides for retention periods of specific components.
//
// Currently, we only have a single component (incidents), but this leaves room for future expansion
// without breaking the config structure. The fields here must be pointers to distinguish between
// "not set" and "set to zero" (i.e. no retention) when overriding the default retention period.
type RetentionOpts struct {
	Incident *time.Duration `yaml:"incident" env:"INCIDENT"`
}

// Validate implements the [config.Validator] interface.
func (r *RetentionOpts) Validate() error {
	if r.Incident != nil && *r.Incident < 0 {
		return errors.New("invalid retention period for incidents")
	}
	return nil
}

// Retention defines the retention policy for Icinga Notifications history cleanups.
type Retention struct {
	Period    time.Duration `yaml:"period" env:"PERIOD"`
	Interval  time.Duration `yaml:"interval" env:"INTERVAL" default:"1h"`
	BatchSize uint64        `yaml:"batch_size" env:"BATCH_SIZE" default:"5000"`
	Options   RetentionOpts `yaml:"options" envPrefix:"OPTIONS_"`
}

// Validate implements the [config.Validator] interface.
func (r *Retention) Validate() error {
	if r.Period < 0 {
		return errors.New("invalid retention period")
	}
	if r.Interval <= 0 {
		return errors.New("interval must be greater than zero")
	}
	if r.BatchSize == 0 {
		return errors.New("'batch_size' must be greater than zero")
	}
	return r.Options.Validate()
}

// Assert interface compliance.
var (
	_ defaults.Setter  = (*ConfigFile)(nil)
	_ config.Validator = (*ConfigFile)(nil)
	_ config.Validator = (*Listener)(nil)
	_ config.Validator = (*Retention)(nil)
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
