package daemon

import (
	"bytes"
	"github.com/goccy/go-yaml"
	icingadbConfig "github.com/icinga/icingadb/pkg/config"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"strings"
	"testing"
	"time"
)

func TestPopulateFromYamlEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		target  any
		environ []string // Prefix is an additional "_" for this test, resulting in "__${KEY..}".
		want    string
		wantErr bool
	}{
		{
			name:   "empty",
			target: &struct{}{},
			want:   "{}",
		},
		{
			name:    "missing-yaml-tag",
			target:  &struct{ A int }{},
			environ: []string{"__A=23"},
			wantErr: true,
		},
		{
			name: "primitive-types",
			target: &struct {
				A bool    `yaml:"a"`
				B uint64  `yaml:"b"`
				C int64   `yaml:"c"`
				D float64 `yaml:"d"`
				E string  `yaml:"e"`
			}{},
			environ: []string{
				"__A=true",
				"__B=9001",
				"__C=-9001",
				"__D=23.42",
				"__E=Hello World!",
			},
			want: `
a: true
b: 9001
c: -9001
d: 23.42
e: Hello World!
			`,
		},
		{
			name: "nested-struct",
			target: &struct {
				A struct {
					A int `yaml:"a"`
				} `yaml:"a"`
			}{},
			environ: []string{"__A_A=23"},
			want: `
a:
  a: 23
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := PopulateFromYamlEnvironment("_", tt.target, tt.environ)
			assert.Equal(t, tt.wantErr, err != nil, "unexpected error: %v", err)
			if err != nil {
				return
			}

			var yamlBuff bytes.Buffer
			assert.NoError(t, yaml.NewEncoder(&yamlBuff).Encode(tt.target), "encoding YAML")
			assert.Equal(t, strings.TrimSpace(tt.want), strings.TrimSpace(yamlBuff.String()), "unexpected ConfigFile")
		})
	}
}

func TestPopulateFromYamlEnvironmentInline(t *testing.T) {
	type Inner struct {
		IA int    `yaml:"ia"`
		IB string `yaml:"ib"`
	}
	type Outer struct {
		A float64 `yaml:"a"`
		I Inner   `yaml:",inline"`
	}

	environ := []string{
		"__A=3.14",
		"__IA=12345",
		"__IB=_inline string",
	}

	var outer Outer
	assert.NoError(t, PopulateFromYamlEnvironment("_", &outer, environ), "populating _inline struct")
}

func TestPopulateFromYamlEnvironmentConfigFile(t *testing.T) {
	tests := []struct {
		name    string
		environ []string
		want    string
		wantErr bool
	}{
		{
			name: "empty",
			want: `
listen: ""
debug-password: ""
channel-plugin-dir: ""
icingaweb2-url: ""
database:
  type: ""
  host: ""
  port: 0
  database: ""
  user: ""
  password: ""
  tls: false
  cert: ""
  key: ""
  ca: ""
  insecure: false
  options:
    max_connections: 0
    max_connections_per_table: 0
    max_placeholders_per_statement: 0
    max_rows_per_transaction: 0
logging:
  level: info
  output: ""
  interval: 0s
  options: {}
			`,
		},
		{
			name: "irrelevant-keys",
			environ: []string{
				"ICINGA_NOPE=FOO",
				"FOO=NOPE",
			},
			want: `
listen: ""
debug-password: ""
channel-plugin-dir: ""
icingaweb2-url: ""
database:
  type: ""
  host: ""
  port: 0
  database: ""
  user: ""
  password: ""
  tls: false
  cert: ""
  key: ""
  ca: ""
  insecure: false
  options:
    max_connections: 0
    max_connections_per_table: 0
    max_placeholders_per_statement: 0
    max_rows_per_transaction: 0
logging:
  level: info
  output: ""
  interval: 0s
  options: {}
			`,
		},
		{
			name: "base-unknown-field",
			environ: []string{
				"ICINGA_NOTIFICATIONS_INVALID=no",
			},
			wantErr: true,
		},
		{
			name: "base-config",
			environ: []string{
				"ICINGA_NOTIFICATIONS_LISTEN='[2001:db8::1]:5680'",
				"ICINGA_NOTIFICATIONS_DEBUG-PASSWORD=insecure",
				"ICINGA_NOTIFICATIONS_CHANNEL-PLUGIN-DIR=/channel",
				"ICINGA_NOTIFICATIONS_ICINGAWEB2-URL=http://[2001:db8::1]/icingaweb2/",
			},
			want: `
listen: "[2001:db8::1]:5680"
debug-password: insecure
channel-plugin-dir: /channel
icingaweb2-url: http://[2001:db8::1]/icingaweb2/
database:
  type: ""
  host: ""
  port: 0
  database: ""
  user: ""
  password: ""
  tls: false
  cert: ""
  key: ""
  ca: ""
  insecure: false
  options:
    max_connections: 0
    max_connections_per_table: 0
    max_placeholders_per_statement: 0
    max_rows_per_transaction: 0
logging:
  level: info
  output: ""
  interval: 0s
  options: {}
			`,
		},
		{
			name: "nested-config",
			environ: []string{
				"ICINGA_NOTIFICATIONS_LISTEN='[2001:db8::1]:5680'",
				"ICINGA_NOTIFICATIONS_DEBUG-PASSWORD=insecure",
				"ICINGA_NOTIFICATIONS_CHANNEL-PLUGIN-DIR=/channel",
				"ICINGA_NOTIFICATIONS_ICINGAWEB2-URL=http://[2001:db8::1]/icingaweb2/",
				"ICINGA_NOTIFICATIONS_DATABASE_TYPE=pgsql",
				"ICINGA_NOTIFICATIONS_DATABASE_HOST='[2001:db8::23]'",
				"ICINGA_NOTIFICATIONS_DATABASE_PORT=5432",
				"ICINGA_NOTIFICATIONS_DATABASE_DATABASE=icinga_notifications",
				"ICINGA_NOTIFICATIONS_DATABASE_USER=icinga_notifications",
				"ICINGA_NOTIFICATIONS_DATABASE_PASSWORD=insecure",
				"ICINGA_NOTIFICATIONS_LOGGING_LEVEL=debug",
				"ICINGA_NOTIFICATIONS_LOGGING_OUTPUT=console",
				"ICINGA_NOTIFICATIONS_LOGGING_INTERVAL=9001h",
			},
			want: `
listen: "[2001:db8::1]:5680"
debug-password: insecure
channel-plugin-dir: /channel
icingaweb2-url: http://[2001:db8::1]/icingaweb2/
database:
  type: pgsql
  host: "[2001:db8::23]"
  port: 5432
  database: icinga_notifications
  user: icinga_notifications
  password: insecure
  tls: false
  cert: ""
  key: ""
  ca: ""
  insecure: false
  options:
    max_connections: 0
    max_connections_per_table: 0
    max_placeholders_per_statement: 0
    max_rows_per_transaction: 0
logging:
  level: debug
  output: console
  interval: 9001h0m0s
  options: {}
			`,
		},
		{
			name: "inlined-database-tls-config",
			environ: []string{
				"ICINGA_NOTIFICATIONS_DATABASE_TLS=true",
				"ICINGA_NOTIFICATIONS_DATABASE_CERT=./client.crt",
				"ICINGA_NOTIFICATIONS_DATABASE_KEY=./client.key",
				"ICINGA_NOTIFICATIONS_DATABASE_CA=./ca.crt",
				"ICINGA_NOTIFICATIONS_DATABASE_INSECURE=false",
			},
			want: `
listen: ""
debug-password: ""
channel-plugin-dir: ""
icingaweb2-url: ""
database:
  type: ""
  host: ""
  port: 0
  database: ""
  user: ""
  password: ""
  tls: true
  cert: ./client.crt
  key: ./client.key
  ca: ./ca.crt
  insecure: false
  options:
    max_connections: 0
    max_connections_per_table: 0
    max_placeholders_per_statement: 0
    max_rows_per_transaction: 0
logging:
  level: info
  output: ""
  interval: 0s
  options: {}
			`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg ConfigFile
			err := PopulateFromYamlEnvironment("ICINGA_NOTIFICATIONS", &cfg, tt.environ)
			assert.Equal(t, tt.wantErr, err != nil, "unexpected error: %v", err)
			if err != nil {
				return
			}

			var yamlBuff bytes.Buffer
			assert.NoError(t, yaml.NewEncoder(&yamlBuff).Encode(&cfg), "encoding YAML")
			assert.Equal(t, strings.TrimSpace(tt.want), strings.TrimSpace(yamlBuff.String()), "unexpected ConfigFile")
		})
	}
}

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name string
		envs []string
		yaml string
		want *ConfigFile
	}{
		{
			// Some defaults are inherited from nested fields, e.g., all Options within ConfigFile.Database.Options.
			name: "defaults",
			want: &ConfigFile{
				Listen:           "localhost:5680",
				DebugPassword:    "",
				ChannelPluginDir: "/usr/libexec/icinga-notifications/channel",
				Icingaweb2URL:    "",
				Database: icingadbConfig.Database{
					Type: "mysql",
					Options: icingadb.Options{
						MaxConnections:              16,
						MaxConnectionsPerTable:      8,
						MaxPlaceholdersPerStatement: 8192,
						MaxRowsPerTransaction:       8192,
					},
				},
				Logging: icingadbConfig.Logging{
					Level:    zap.InfoLevel,
					Interval: 20 * time.Second,
				},
			},
		},
		{
			name: "envs-base",
			envs: []string{
				"ICINGA_NOTIFICATIONS_LISTEN='[2001:db8::1]:5680'",
				"ICINGA_NOTIFICATIONS_DEBUG-PASSWORD=insecure",
				"ICINGA_NOTIFICATIONS_CHANNEL-PLUGIN-DIR=/channel",
				"ICINGA_NOTIFICATIONS_ICINGAWEB2-URL=http://[2001:db8::1]/icingaweb2/",
			},
			want: &ConfigFile{
				Listen:           "[2001:db8::1]:5680",
				DebugPassword:    "insecure",
				ChannelPluginDir: "/channel",
				Icingaweb2URL:    "http://[2001:db8::1]/icingaweb2/",
				Database: icingadbConfig.Database{
					Type: "mysql",
					Options: icingadb.Options{
						MaxConnections:              16,
						MaxConnectionsPerTable:      8,
						MaxPlaceholdersPerStatement: 8192,
						MaxRowsPerTransaction:       8192,
					},
				},
				Logging: icingadbConfig.Logging{
					Level:    zap.InfoLevel,
					Interval: 20 * time.Second,
				},
			},
		},
		{
			name: "env-nested",
			envs: []string{
				"ICINGA_NOTIFICATIONS_LISTEN='[2001:db8::1]:5680'",
				"ICINGA_NOTIFICATIONS_DEBUG-PASSWORD=insecure",
				"ICINGA_NOTIFICATIONS_CHANNEL-PLUGIN-DIR=/channel",
				"ICINGA_NOTIFICATIONS_ICINGAWEB2-URL=http://[2001:db8::1]/icingaweb2/",
				"ICINGA_NOTIFICATIONS_DATABASE_TYPE=pgsql",
				"ICINGA_NOTIFICATIONS_DATABASE_HOST='[2001:db8::23]'",
				"ICINGA_NOTIFICATIONS_DATABASE_PORT=5432",
				"ICINGA_NOTIFICATIONS_DATABASE_DATABASE=icinga_notifications",
				"ICINGA_NOTIFICATIONS_DATABASE_USER=icinga_notifications",
				"ICINGA_NOTIFICATIONS_DATABASE_PASSWORD=insecure",
				"ICINGA_NOTIFICATIONS_LOGGING_LEVEL=debug",
				"ICINGA_NOTIFICATIONS_LOGGING_OUTPUT=console",
				"ICINGA_NOTIFICATIONS_LOGGING_INTERVAL=9001h",
			},
			want: &ConfigFile{
				Listen:           "[2001:db8::1]:5680",
				DebugPassword:    "insecure",
				ChannelPluginDir: "/channel",
				Icingaweb2URL:    "http://[2001:db8::1]/icingaweb2/",
				Database: icingadbConfig.Database{
					Type:     "pgsql",
					Host:     "[2001:db8::23]",
					Port:     5432,
					Database: "icinga_notifications",
					User:     "icinga_notifications",
					Password: "insecure",
					Options: icingadb.Options{
						MaxConnections:              16,
						MaxConnectionsPerTable:      8,
						MaxPlaceholdersPerStatement: 8192,
						MaxRowsPerTransaction:       8192,
					},
				},
				Logging: icingadbConfig.Logging{
					Level:    zap.DebugLevel,
					Output:   "console",
					Interval: 9001 * time.Hour,
				},
			},
		},
		{
			name: "yaml-base",
			yaml: `
listen: "[2001:db8::1]:5680"
debug-password: "insecure"
channel-plugin-dir: "/channel"
icingaweb2-url: "http://[2001:db8::1]/icingaweb2/"
			`,
			want: &ConfigFile{
				Listen:           "[2001:db8::1]:5680",
				DebugPassword:    "insecure",
				ChannelPluginDir: "/channel",
				Icingaweb2URL:    "http://[2001:db8::1]/icingaweb2/",
				Database: icingadbConfig.Database{
					Type: "mysql",
					Options: icingadb.Options{
						MaxConnections:              16,
						MaxConnectionsPerTable:      8,
						MaxPlaceholdersPerStatement: 8192,
						MaxRowsPerTransaction:       8192,
					},
				},
				Logging: icingadbConfig.Logging{
					Level:    zap.InfoLevel,
					Interval: 20 * time.Second,
				},
			},
		},
		{
			name: "yaml-nested",
			yaml: `
listen: "[2001:db8::1]:5680"
debug-password: "insecure"
channel-plugin-dir: "/channel"
icingaweb2-url: "http://[2001:db8::1]/icingaweb2/"

database:
  type: "pgsql"
  host: "[2001:db8::23]"
  port: 5432
  database: "icinga_notifications"
  user: "icinga_notifications"
  password: "insecure"

logging:
  level: "debug"
  output: "console"
  interval: "9001h"
			`,
			want: &ConfigFile{
				Listen:           "[2001:db8::1]:5680",
				DebugPassword:    "insecure",
				ChannelPluginDir: "/channel",
				Icingaweb2URL:    "http://[2001:db8::1]/icingaweb2/",
				Database: icingadbConfig.Database{
					Type:     "pgsql",
					Host:     "[2001:db8::23]",
					Port:     5432,
					Database: "icinga_notifications",
					User:     "icinga_notifications",
					Password: "insecure",
					Options: icingadb.Options{
						MaxConnections:              16,
						MaxConnectionsPerTable:      8,
						MaxPlaceholdersPerStatement: 8192,
						MaxRowsPerTransaction:       8192,
					},
				},
				Logging: icingadbConfig.Logging{
					Level:    zap.DebugLevel,
					Output:   "console",
					Interval: 9001 * time.Hour,
				},
			},
		},
		{
			name: "yaml-env-mixed",
			yaml: `
listen: "[2001:db8::1]:5680"
debug-password: "INCORRECT"
channel-plugin-dir: "/channel"
icingaweb2-url: "http://[2001:db8::1]/icingaweb2/"
			`,
			envs: []string{
				"ICINGA_NOTIFICATIONS_DEBUG-PASSWORD=insecure",
			},
			want: &ConfigFile{
				Listen:           "[2001:db8::1]:5680",
				DebugPassword:    "insecure",
				ChannelPluginDir: "/channel",
				Icingaweb2URL:    "http://[2001:db8::1]/icingaweb2/",
				Database: icingadbConfig.Database{
					Type: "mysql",
					Options: icingadb.Options{
						MaxConnections:              16,
						MaxConnectionsPerTable:      8,
						MaxPlaceholdersPerStatement: 8192,
						MaxRowsPerTransaction:       8192,
					},
				},
				Logging: icingadbConfig.Logging{
					Level:    zap.InfoLevel,
					Interval: 20 * time.Second,
				},
			},
		},
		{
			name: "yaml-env-mixed-nested",
			yaml: `
listen: "[2001:db8::1]:5680"
debug-password: "INCORRECT"
channel-plugin-dir: "/channel"
icingaweb2-url: "http://[2001:db8::1]/icingaweb2/"

database:
  type: "pgsql"
  host: "[2001:db8::23]"
  port: 5432
  database: "icinga_notifications"
  user: "icinga_notifications"
  password: "insecure"
			`,
			envs: []string{
				"ICINGA_NOTIFICATIONS_DEBUG-PASSWORD=insecure",
				"ICINGA_NOTIFICATIONS_LOGGING_LEVEL=debug",
				"ICINGA_NOTIFICATIONS_LOGGING_OUTPUT=console",
				"ICINGA_NOTIFICATIONS_LOGGING_INTERVAL=9001h",
			},
			want: &ConfigFile{
				Listen:           "[2001:db8::1]:5680",
				DebugPassword:    "insecure",
				ChannelPluginDir: "/channel",
				Icingaweb2URL:    "http://[2001:db8::1]/icingaweb2/",
				Database: icingadbConfig.Database{
					Type:     "pgsql",
					Host:     "[2001:db8::23]",
					Port:     5432,
					Database: "icinga_notifications",
					User:     "icinga_notifications",
					Password: "insecure",
					Options: icingadb.Options{
						MaxConnections:              16,
						MaxConnectionsPerTable:      8,
						MaxPlaceholdersPerStatement: 8192,
						MaxRowsPerTransaction:       8192,
					},
				},
				Logging: icingadbConfig.Logging{
					Level:    zap.DebugLevel,
					Output:   "console",
					Interval: 9001 * time.Hour,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loadConfig(strings.NewReader(tt.yaml), tt.envs)
			assert.NoError(t, err, "unexpected error")
			assert.Equal(t, tt.want, got, "unexpected ConfigFile")
		})
	}
}
