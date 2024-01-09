package daemon

import (
	"errors"
	"fmt"
	"github.com/creasty/defaults"
	"github.com/goccy/go-yaml"
	icingadbConfig "github.com/icinga/icingadb/pkg/config"
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"
)

// populateFromYamlEnvironmentPathStep is the recursive worker function for PopulateFromYamlEnvironment.
//
// It performs a linear search along the path with pathNo as the current element except when a wild `,inline` appears,
// resulting in branching off to allow peeking into the inlined struct.
func populateFromYamlEnvironmentPathStep(keyPrefix string, cur reflect.Value, path []string, pathNo int, value string) error {
	notFoundErr := errors.New("cannot resolve path")

	subKey := keyPrefix + "_" + strings.Join(path[:pathNo+1], "_")

	t := cur.Type()
	for fieldNo := 0; fieldNo < t.NumField(); fieldNo++ {
		fieldName := t.Field(fieldNo).Tag.Get("yaml")
		if fieldName == "" {
			return fmt.Errorf("field %q misses yaml struct tag", subKey)
		}
		if strings.Contains(fieldName, "_") {
			return fmt.Errorf("field %q contains an underscore, the environment key separator, in its yaml struct tag", subKey)
		}

		if regexp.MustCompile(`^.*(,[a-z]+)*,inline(,[a-z]+)*$`).MatchString(fieldName) {
			// Peek into the `,inline`d struct but ignore potential failure.
			err := populateFromYamlEnvironmentPathStep(keyPrefix, reflect.Indirect(cur).Field(fieldNo), path, pathNo, value)
			if err == nil {
				return nil
			} else if !errors.Is(err, notFoundErr) {
				return err
			}
		}

		if strings.ToUpper(fieldName) != path[pathNo] {
			continue
		}

		if pathNo < len(path)-1 {
			return populateFromYamlEnvironmentPathStep(keyPrefix, reflect.Indirect(cur).Field(fieldNo), path, pathNo+1, value)
		}

		field := cur.Field(fieldNo)
		tmp := reflect.New(field.Type()).Interface()
		err := yaml.NewDecoder(strings.NewReader(value)).Decode(tmp)
		if err != nil {
			return fmt.Errorf("cannot unmarshal into %q: %w", subKey, err)
		}
		field.Set(reflect.ValueOf(tmp).Elem())
		return nil
	}

	return fmt.Errorf("%w %q", notFoundErr, subKey)
}

// PopulateFromYamlEnvironment populates a struct with "yaml" struct tags based on environment variables.
//
// To write into targetElem, it must be passed as a pointer reference.
//
// Environment variables of the form ${KEY_PREFIX}_${KEY_0}_${KEY_i}_${KEY_n}=${VALUE} will be translated to a YAML path
// from the struct field with the "yaml" struct tag ${KEY_0} across all further nested fields ${KEY_i} up to the last
// ${KEY_n}. The ${VALUE} will be YAML decoded into the referenced field of the targetElem.
//
// Next to addressing fields through keys, elementary `,inline` flags are also being supported. This allows referring an
// inline struct's field as it would be a field of the parent.
//
// Consider the following struct:
//
//	type Example struct {
//		Outer struct {
//			Inner int `yaml:"inner"`
//		} `yaml:"outer"`
//	}
//
// The Inner field can get populated through:
//
//	PopulateFromYamlEnvironment("EXAMPLE", &example, []string{"EXAMPLE_OUTER_INNER=23"})
func PopulateFromYamlEnvironment(keyPrefix string, targetElem any, environ []string) error {
	matcher, err := regexp.Compile(`(?s)\A` + keyPrefix + `_([A-Z0-9_-]+)=(.*)\z`)
	if err != nil {
		return err
	}

	if reflect.ValueOf(targetElem).Type().Kind() != reflect.Ptr {
		return errors.New("targetElem is required to be a pointer")
	}

	for _, env := range environ {
		match := matcher.FindStringSubmatch(env)
		if match == nil {
			continue
		}

		path := strings.Split(match[1], "_")
		parent := reflect.Indirect(reflect.ValueOf(targetElem))

		err := populateFromYamlEnvironmentPathStep(keyPrefix, parent, path, 0, match[2])
		if err != nil {
			return err
		}
	}

	return nil
}

// ConfigFile used from the icinga-notifications-daemon.
//
// The ConfigFile will be populated from different sources in the following order, when calling the LoadConfig method:
//  1. Default values (default struct tags) are getting assigned from all nested types.
//  2. Values are getting overridden from the YAML configuration file.
//  3. Values are getting overridden by environment variables of the form ICINGA_NOTIFICATIONS_${KEY}.
//
// The environment variable key is an underscore separated string of uppercase struct fields. For example
//   - ICINGA_NOTIFICATIONS_DEBUG-PASSWORD sets ConfigFile.DebugPassword and
//   - ICINGA_NOTIFICATIONS_DATABASE_HOST sets ConfigFile.Database.Host.
type ConfigFile struct {
	Listen           string                  `yaml:"listen" default:"localhost:5680"`
	DebugPassword    string                  `yaml:"debug-password"`
	ChannelPluginDir string                  `yaml:"channel-plugin-dir" default:"/usr/libexec/icinga-notifications/channel"`
	Icingaweb2URL    string                  `yaml:"icingaweb2-url"`
	Database         icingadbConfig.Database `yaml:"database"`
	Logging          icingadbConfig.Logging  `yaml:"logging"`
}

// config holds the configuration state as a singleton. It is used from LoadConfig and Config
var config *ConfigFile

// LoadConfig loads the daemon configuration from environment variables, YAML configuration, and defaults.
//
// After loading, some validations will be performed. This function MUST be called only once when starting the daemon.
func LoadConfig(cfgPath string) error {
	if config != nil {
		return errors.New("config already set")
	}

	var cfgReader io.ReadCloser
	if cfgPath != "" {
		var err error
		if cfgReader, err = os.Open(cfgPath); err != nil {
			return err
		}
		defer func() { _ = cfgReader.Close() }()
	}

	cfg, err := loadConfig(cfgReader, os.Environ())
	if err != nil {
		return err
	}

	err = cfg.Validate()
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

// loadConfig loads the daemon configuration from environment variables, YAML configuration, and defaults.
func loadConfig(yamlCfg io.Reader, environ []string) (*ConfigFile, error) {
	var c ConfigFile

	if err := defaults.Set(&c); err != nil {
		return nil, err
	}

	err := yaml.NewDecoder(yamlCfg).Decode(&c)
	if err != nil && err != io.EOF {
		return nil, err
	}

	err = PopulateFromYamlEnvironment("ICINGA_NOTIFICATIONS", &c, environ)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate the ConfigFile and return an error if a check failed.
func (c *ConfigFile) Validate() error {
	if c.Icingaweb2URL == "" {
		return fmt.Errorf("Icingaweb2URL field MUST be populated")
	}
	if err := c.Database.Validate(); err != nil {
		return err
	}
	if err := c.Logging.Validate(); err != nil {
		return err
	}

	return nil
}
