package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/icinga/icinga-go-library/config"
	"github.com/icinga/icinga-go-library/notifications/jsonrpc"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-notifications/internal"
	"go.uber.org/zap/zapcore"
)

func main() {
	channelName := filepath.Base(os.Args[0])
	plugin.Run(&ChannelWrapper{Name: channelName})
}

// Environment represents a map of environment variables that are passed to the plugin when it is executed.
//
// The env values can contain template references, which will be resolved before passing them to the executable.
type Environment map[string]string

// Command arguments represent a command to be executed, which can be either a string or an array of any type.
// The command name itself is also regarded as an argument.
// Arguments that do not have values or do not possess a condition are saved as keys, and the rest becomes nil.
//
// If the command is a string, then it will be executed as-is after template references have been resolved.
//
// If the command is an array, then we can assume and also enforce that the first element is the command to be
// executed, and the rest are its arguments. In this case, the arguments might be a mix of strings and other types,
// which will be evaluated and converted to their final value before being passed to the command. For example, an
// argument could be of the following form:
//
// -
//
//	key: "--some-arg"
//	value:" "{{ .some_condition }}"
//	set_if: "{{ .some_condition }}"
//
// In this case, the argument needs a complex evaluation of the `set_if` condition, and if it evaluates to true,
// then the argument with key "--some-arg" and its resolved value will be passed to the command at the exact position
// where it was defined in the array. If the `set_if` condition evaluates to false, then the argument will be
// skipped and not passed to the command at all.
//
// However, if the argument is not of the above form, then the only evaluation that will be done is the resolution
// of any template references in the argument value (if any), and nothing else.
type cmdArg struct {
	key   *template.Template `yaml:"key"`
	val   *template.Template `yaml:"value"`
	setIf *template.Template `yaml:"set_if"`
}

// ChannelWrapper represents the configuration and state of a channel wrapper plugin.
type ChannelWrapper struct {
	file          *YAMLFile
	Name          string
	OptionsByName map[string]plugin.ConfigOption
	// Config represents the actual config of the channel wrapper as received from the [plugin.SetConfig] call.
	//
	// The keys of the map are the config option names as defined in the [YAMLFile.Config] section, and the
	// values are the corresponding values provided by the user.
	Config      map[string]any
	CommandArgs []cmdArg
	EnvTmpls    map[string]*template.Template

	mu sync.Mutex

	ctx context.Context
	rep *jsonrpc.Endpoint
}

func (ch *ChannelWrapper) ReceiveEndpoint(ctx context.Context, ep *jsonrpc.Endpoint) {
	ch.ctx = ctx
	ch.rep = ep
}

func (ch *ChannelWrapper) GetInfo() *plugin.Info {
	// First-time setup loads channel configuration from the file on startup
	if err := ch.initialize(); err != nil {
		panic(err)
	}

	return &plugin.Info{
		Name:             ch.Name,
		Version:          internal.Version.Version,
		Author:           "Icinga GmbH",
		ConfigAttributes: ch.file.Config,
	}
}

func (ch *ChannelWrapper) SetConfig(jsonStr json.RawMessage) error {
	var err error
	if err := ch.initialize(); err != nil {
		return fmt.Errorf("could not initialize channel wrapper: %w", err)
	}

	var tmp ChannelWrapper
	if err = plugin.PopulateDefaults(&tmp); err != nil {
		return err
	}

	var incomingConfig map[string]any
	if err = json.Unmarshal(jsonStr, &incomingConfig); err != nil {
		return fmt.Errorf("error unmarshaling configuration: %w", err)
	}

	currentConfig := tmp.OptionsByName

	var errs []error

	// Every key in the JSON must be a known option with the right type
	for key := range incomingConfig {
		_, ok := currentConfig[key]
		if !ok {
			errs = append(errs, fmt.Errorf("unknown option %q", key))
			continue
		}
	}

	// Every required option must be present
	for _, o := range currentConfig {
		if _, ok := incomingConfig[o.Name]; o.Required && !ok {
			errs = append(errs, fmt.Errorf("missing required option %q", o.Name))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	ch.mu.Lock()
	defer ch.mu.Unlock()

	ch.Config = make(map[string]any)
	ch.Config = incomingConfig

	return nil
}

func (ch *ChannelWrapper) SendNotification(req *plugin.NotificationRequest) error {
	// Parse message and have it contain everything necessary
	var reqMsg bytes.Buffer
	_, _ = fmt.Fprint(&reqMsg, plugin.FormatSubject(req)+"\n\n")
	plugin.FormatMessage(&reqMsg, req)

	ch.mu.Lock()
	config := ch.Config
	cmdArgs := ch.CommandArgs
	ch.mu.Unlock()

	// A struct to hold all data that belongs to the template
	data := map[string]any{
		"config":       config,                                     // from the form
		"notification": map[string]any{"message": reqMsg.String()}, // from the system
	}

	// Fill in command arguments template
	argvs := make([]string, 0, len(cmdArgs))
	for _, a := range cmdArgs {
		parts, err := render(a, data)

		if err != nil {
			_ = ch.rep.NotifyLog(ch.ctx, zapcore.ErrorLevel, "Error assembling command", "error", fmt.Sprintf("%v", err))
			return fmt.Errorf("error rendering command arguments: %w", err)
		}
		argvs = append(argvs, parts...)
	}

	// Fill in environmental templates
	envVars, err := renderEnv(ch.EnvTmpls, data)
	if err != nil {
		_ = ch.rep.NotifyLog(ch.ctx, zapcore.ErrorLevel, "Error assembling environmental variables",
			"error", fmt.Sprintf("%v", err))
		return fmt.Errorf("error rendering env vars: %w", err)
	}

	ctx, cancel := context.WithTimeout(ch.ctx, 30*time.Second)
	var cmd *exec.Cmd
	defer cancel()

	// If the set only consists of one key, it means the command was originally a string,
	// and we need to split it to get the command itself and the arguments.
	// It can only be done after the templates were filled due to syntax.
	if len(argvs) == 1 {
		splitArgs := strings.Split(argvs[0], " ")
		cmd = exec.CommandContext(ctx, splitArgs[0], splitArgs[1:]...) // #nosec G204
	} else {
		cmd = exec.CommandContext(ctx, argvs[0], argvs[1:]...) // #nosec G204b
	}

	if len(envVars) > 0 {
		cmd.Env = append(os.Environ(), envVars...)
	}

	if cmd == nil {
		return fmt.Errorf("invalid empty command")
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = ch.rep.NotifyLog(ch.ctx, zapcore.ErrorLevel, "Error executing command", "error", fmt.Sprintf("%v", err))
		return fmt.Errorf("command failed: %v, %v", err, out)
	}

	return nil
}

func (ch *ChannelWrapper) initialize() error {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if ch.file != nil {
		return nil
	}

	var yamlFile YAMLFile
	// Read configuration from file, validation included
	args := os.Args[1:]
	err := config.FromYAMLFile(args[0], &yamlFile)
	if err != nil {
		return fmt.Errorf("error reading configuration: %w", err)
	}
	// Make the config easily addressable by name, not by obscure number
	byName := make(map[string]plugin.ConfigOption, len(yamlFile.Config))
	for _, option := range yamlFile.Config {
		byName[option.Name] = option
	}

	ch.OptionsByName = byName

	// Prepare command from file: check arguments' types and create templates for each one.
	// It parses both normal and nested arguments. Command is always a set of key, value and set_if;
	// in case of normal argument, it becomes the key, the rest of the fields are skipped
	// in case of a nested argument, the key and the value are saved along with possible condition.
	// That also means that the configuration cannot have multiple key-value pairs in a single YAML entry.
	// Single line command is accepted as well, becoming the sole key and parsed additionally when
	// notification is sent.
	rawArgs := yamlFile.Command
	commandArgs, err := prepareCommand(rawArgs)
	if err != nil {
		return fmt.Errorf("error preparing command: %w", err)
	}

	// Environmental variables are also parsed as templates
	// But since they do not contain maps, it is a separate, smaller function
	rawEnv := yamlFile.Environment
	envTmpls, err := prepareEnv(rawEnv)
	if err != nil {
		return fmt.Errorf("error preparing environmental variables: %w", err)
	}

	ch.file = &yamlFile
	ch.CommandArgs = commandArgs
	ch.EnvTmpls = envTmpls

	return nil
}

// YAMLFile represents the structure of the YAML configuration file for the channel wrapper.
type YAMLFile struct {
	Config      []plugin.ConfigOption `yaml:"config"`
	Environment Environment           `yaml:"environment"`
	Command     []any                 `yaml:"command"`
}

func (y *YAMLFile) Validate() error {
	var errs []error

	// For validation, test data is used for purposes of a dry-run
	testConfig := make(map[string]any)
	for _, v := range y.Config {
		switch v.Type {
		case "number":
			testConfig[v.Name] = 10
		default:
			testConfig[v.Name] = "test_val"
		}
	}

	testData := map[string]any{
		"config":       testConfig,
		"notification": map[string]any{"message": "test message"},
	}

	// Check that the file is not empty
	if y == nil {
		return fmt.Errorf("YAML file is nil, terminating")
	}

	// Check configuration has no duplicate or empty entries
	known := make(map[string]bool, len(y.Config))
	for i, o := range y.Config {
		switch {
		case o.Name == "":
			errs = append(errs, fmt.Errorf("config[%d]: name is empty", i))
		case known[o.Name]:
			errs = append(errs, fmt.Errorf("config[%d]: duplicate name %q", i, o.Name))
		default:
			known[o.Name] = true
		}
	}

	// Check environment variables are written correctly
	if len(y.Environment) > 0 {
		envTmpls, err := prepareEnv(y.Environment)
		if err != nil {
			errs = append(errs, err)
		}
		for name, t := range envTmpls {
			if _, err := execTmpl(t, testData); err != nil {
				errs = append(errs, fmt.Errorf("env %s: %w", name, err))
			}
		}
	}

	// Check whether command/script exists at all and can be run
	if len(y.Command) > 0 {
		cmd := y.Command[0]

		if len(y.Command) == 1 { // a string was passed
			splitArgs := strings.Split(fmt.Sprintf("%v", y.Command[0]), " ")
			if len(splitArgs) < 1 {
				errs = append(errs, fmt.Errorf("no command specified"))
			}
			cmd = splitArgs[0]
		}

		// Check if command is executable
		//_, err := exec.LookPath(fmt.Sprint(cmd)) // #nosec G204
		//if err != nil {
		//	errs = append(errs, fmt.Errorf("command not found: %v", err))
		//}
		log.Print(cmd)

		args, err := prepareCommand(y.Command)
		if err != nil {
			errs = append(errs, err)
		}

		// Check that command arguments correspond to actual fields in the configuration
		for i, a := range args {
			if a.setIf != nil {
				// Clone so the production template keeps missingkey=zero
				strict, err := a.setIf.Clone()
				if err != nil {
					errs = append(errs, fmt.Errorf("arg %d set_if: %w ", i, err))
				} else {
					strict.Option("missingkey=error")
					if _, err := execTmpl(strict, testData); err != nil {
						errs = append(errs, fmt.Errorf("arg %d set_if: %w ", i, err))
					}
				}
			}

			// Check key and value without render() to avoid skipping them in case set_if is false
			if _, err := execTmpl(a.key, testData); err != nil {
				errs = append(errs, fmt.Errorf("arg %d key: %w ", i, err))
			}

			if a.val != nil {
				if _, err := execTmpl(a.val, testData); err != nil {
					errs = append(errs, fmt.Errorf("arg %d value: %w ", i, err))
				}
			}
		}

	}

	if len(errs) > 0 {
		return fmt.Errorf("%v", errs)
	}

	return nil
}

// Look into environment variables, check invalid characters, turn into templates
func prepareEnv(env Environment) (map[string]*template.Template, error) {
	out := make(map[string]*template.Template, len(env))
	for name, val := range env {
		if name == "" || strings.ContainsAny(name, "= \t\n") {
			return nil, fmt.Errorf("env %q: invalid variable name", name)
		}
		t, err := template.New("env." + name).
			Option("missingkey=error").Parse(val)
		if err != nil {
			return nil, fmt.Errorf("env %s %q: %w", name, val, err)
		}
		out[name] = t
	}

	return out, nil
}

// Prepare command argument templates, check spelling, check incorrect fields
// Also used in script validation to have the program crash as soon as there is a problem
func prepareCommand(rawArgs []any) ([]cmdArg, error) {
	commandArgs := make([]cmdArg, 0, len(rawArgs))

	for i, arg := range rawArgs {
		var commandArg cmdArg
		switch v := arg.(type) {
		case map[string]any:
			// Reject typos like "setif" or "valeu" instead of silently ignoring them.
			for field := range v {
				if field != "key" && field != "value" && field != "set_if" {
					return nil, fmt.Errorf("arg %d: unknown field %q (allowed: key, value, set_if)", i, field)
				}
			}

			key, ok := v["key"]
			if !ok {
				return nil, fmt.Errorf("arg %d: missing \"key\"", i)
			}

			// Check each parse immediately, so no error can be overwritten.
			var err error
			if commandArg.key, err = template.New(fmt.Sprintf("arg%d.key", i)).
				Option("missingkey=error").Parse(fmt.Sprint(key)); err != nil {
				return nil, fmt.Errorf("arg %d key %q: %w", i, fmt.Sprint(key), err)
			}

			if val, ok := v["value"]; ok {
				if commandArg.val, err = template.New(fmt.Sprintf("arg%d.value", i)).
					Option("missingkey=error").Parse(fmt.Sprint(val)); err != nil {
					return nil, fmt.Errorf("arg %d value %q: %w", i, fmt.Sprint(val), err)
				}
			}

			if cond, ok := v["set_if"]; ok { // same name for check AND read
				if commandArg.setIf, err = template.New(fmt.Sprintf("arg%d.set_if", i)).
					Option("missingkey=zero").Parse(fmt.Sprint(cond)); err != nil {
					return nil, fmt.Errorf("arg %d set_if %q: %w", i, fmt.Sprint(cond), err)
				}
			}

		default:
			var err error
			if commandArg.key, err = template.New(fmt.Sprintf("arg%d", i)).
				Option("missingkey=error").Parse(fmt.Sprint(v)); err != nil {
				return nil, fmt.Errorf("arg %d %q: %w", i, fmt.Sprint(v), err)
			}
		}

		commandArgs = append(commandArgs, commandArg)
	}

	return commandArgs, nil
}

// Fill individual environment variable templates with actual data
func renderEnv(tmpls map[string]*template.Template, data any) ([]string, error) {
	env := make([]string, 0, len(tmpls))
	for name, t := range tmpls {
		v, err := execTmpl(t, data)
		if err != nil {
			return nil, fmt.Errorf("env %s: %w", name, err)
		}
		env = append(env, name+"="+v)
	}

	return env, nil
}

// Fill individual command argument templates with actual data
func render(a cmdArg, data any) ([]string, error) {
	if a.setIf != nil {
		v, err := execTmpl(a.setIf, data)
		if err != nil {
			return nil, fmt.Errorf("set_if: %w", err)
		}

		// Evaluate set_if condition; if false, the entire template is skipped
		if !isTruthy(v) {
			return nil, nil
		}
	}

	key, err := execTmpl(a.key, data)
	if err != nil {
		return nil, fmt.Errorf("key: %w", err)
	}

	res := []string{key}
	if a.val != nil {
		v, err := execTmpl(a.val, data)
		if err != nil {
			return nil, fmt.Errorf("value: %w", err)
		}
		res = append(res, v)
	}

	return res, nil
}

func execTmpl(t *template.Template, data any) (string, error) {
	var buf bytes.Buffer
	err := t.Execute(&buf, data)
	if err != nil {
		return "", fmt.Errorf("template execution failed: %w", err)
	}

	return buf.String(), nil
}

// isTruthy decides whether the rendered set_if text means "yes".
func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", // condition rendered to nothing
		"false",      // a bool false from the config
		"0",          // a number zero
		"<no value>": // what Go's templates print for a missing map key
		return false
	}

	// Anything else ("true", "high", "yes", "1", ...) counts as yes.
	return true
}
