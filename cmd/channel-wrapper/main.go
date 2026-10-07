package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/shlex"
	"github.com/icinga/icinga-go-library/config"
	"github.com/icinga/icinga-go-library/notifications/jsonrpc"
	"github.com/icinga/icinga-go-library/notifications/plugin"
	"github.com/icinga/icinga-notifications/internal"
	"go.uber.org/zap/zapcore"
)

func main() {
	channelName := filepath.Base(os.Args[1])

	plugin.Run(&ChannelWrapper{Name: channelName})
}

// Environment represents a map of environment variables that are passed to the plugin when it is executed.
//
// The env values can contain variable references, which will be resolved before passing them to the executable.
type Environment map[string]string

// Value is an assembly of segments, each of which can be text or variable
type Value []Segment

// Segment is either plain text, or a variable that is stored as an array of strings to accommodate nested vars
type Segment struct {
	Literal      string   `yaml:"text"`
	VariablePath []string `yaml:"variable"` // a format of ["config", "username"]
}

// Fast check if it is a variable at hand or not, useful for command compilation
// where it is required to either pick saved argument as-is or fill it with data if it is a variable
func (s Segment) isVariable() bool {
	return s.VariablePath != nil
}

// so key and set if are only for nested arguments, value ever present
// in case of a nonconditional argument, only val exists; keys for now are not eligible to be conditional
// the setif gets to be empty or a variable by itself so it is easy to compare to val variable
type cmdArg struct {
	key   string   `yaml:"key"`    // only for map[string] type arguments
	value Value    `yaml:"value"`  // the most important ever-present thing
	setIf []string `yaml:"set_if"` // path to var like ["config", "username"] or nil
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
	EnvTmpls    []cmdArg

	mu sync.Mutex

	ctx context.Context
	rep *jsonrpc.Endpoint
}

// ReceiveEndpoint implements the [plugin.RPCEndpointReceiver] interface.
func (ch *ChannelWrapper) ReceiveEndpoint(ctx context.Context, ep *jsonrpc.Endpoint) {
	ch.ctx = ctx
	ch.rep = ep
}

// GetInfo implements the [plugin.Plugin] interface.
func (ch *ChannelWrapper) GetInfo() *plugin.Info {
	// First-time setup loads channel configuration from the file on startup
	if err := ch.initialize(); err != nil {
		panic(err)
	}

	return &plugin.Info{
		Name:                     ch.Name,
		Version:                  internal.Version.Version,
		Author:                   "Icinga GmbH",
		SupportedRequestVersions: []plugin.RequestVersion{plugin.RequestVersionV1},
		ConfigAttributes:         ch.file.Config,
	}
}

// SetConfig implements the [plugin.Plugin] interface.
func (ch *ChannelWrapper) SetConfig(jsonStr json.RawMessage) error {
	if err := ch.initialize(); err != nil {
		return fmt.Errorf("could not initialize channel wrapper: %w", err)
	}

	var tmp ChannelWrapper
	if err := plugin.PopulateDefaults(&tmp); err != nil {
		return err
	}

	var incomingConfig map[string]any
	if err := json.Unmarshal(jsonStr, &incomingConfig); err != nil {
		return fmt.Errorf("error unmarshaling configuration: %w", err)
	}

	currentConfig := tmp.OptionsByName

	var errs []error

	// Every key in the JSON must be a known option with the right type
	for key := range incomingConfig {
		if _, ok := currentConfig[key]; !ok {
			errs = append(errs, fmt.Errorf("unknown option %q", key))
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

	ch.Config = incomingConfig

	return nil
}

// SendNotification implements the [plugin.Plugin] interface.
func (ch *ChannelWrapper) SendNotification(req *plugin.NotificationRequest) (*plugin.DeliveryResult, error) {
	// Parse message and have it contain everything necessary
	payload, err := req.PayloadAs[plugin.PayloadV1]()
	if err != nil {
		return nil, err
	}

	ch.mu.Lock()
	channelConfig := ch.Config
	cmdArgs := ch.CommandArgs
	ch.mu.Unlock()

	payloadDecoded, err := payloadToMap(payload)
	if err != nil {
		_ = ch.rep.NotifyLog(ch.ctx, zapcore.ErrorLevel, "Error processing notification payload", "error", fmt.Sprintf("%v", err))
		return nil, fmt.Errorf("error processing notification payload: %w", err)
	}

	// A map to hold all data that belongs to the notification
	data := map[string]any{
		"config":       channelConfig,  // from the form
		"notification": payloadDecoded, // from the system
	}
	// Fill in command arguments template
	argvs, err := renderCommand(cmdArgs, data)
	if err != nil {
		_ = ch.rep.NotifyLog(ch.ctx, zapcore.ErrorLevel, "Error assembling command",
			"error", fmt.Sprintf("%v", err))
		return nil, fmt.Errorf("error rendering command arguments: %w", err)
	}

	// Fill in environmental templates
	envVars, err := renderCommand(ch.EnvTmpls, data)
	if err != nil {
		_ = ch.rep.NotifyLog(ch.ctx, zapcore.ErrorLevel, "Error assembling environmental variables",
			"error", fmt.Sprintf("%v", err))
		return nil, fmt.Errorf("error rendering env vars: %w", err)
	}

	ctx, cancel := context.WithTimeout(ch.ctx, 30*time.Second)
	var cmd *exec.Cmd
	defer cancel()

	// If the set only consists of one key, it means the command has no arguments
	if len(argvs) == 1 {
		cmd = exec.CommandContext(ctx, argvs[0]) // #nosec G204
	} else {
		cmd = exec.CommandContext(ctx, argvs[0], argvs[1:]...) // #nosec G204
	}

	cmd.Env = append(os.Environ(), envVars...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = ch.rep.NotifyLog(ch.ctx, zapcore.ErrorLevel, "Error executing command", "error", fmt.Sprintf("%v", err))
		return nil, fmt.Errorf("command failed: %w\n, %s", err, out)
	}

	return nil, nil
}

// Initialize channel: upload config, check and parse it, prepare templates
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

	// Prepare command from file: check arguments' types and create templates for each one
	rawArgs := yamlFile.Command
	var preparedArgs []cmdArg
	if len(rawArgs) == 1 {
		if s, ok := rawArgs[0].(string); ok {
			preparedArgs, err = prepareStringCommand(s)
			if err != nil {
				return fmt.Errorf("error preparing command arguments: %w", err)
			}
		}
	} else {
		preparedArgs, err = prepareArrayCommand(rawArgs)
	}

	if err != nil {
		return fmt.Errorf("error preparing command: %w", err)
	}

	// Environmental variables are also parsed and can contain variables
	rawEnv := yamlFile.Environment
	envTmpls, err := rawEnv.prepare()
	if err != nil {
		return fmt.Errorf("error preparing environmental variables: %w", err)
	}

	ch.file = &yamlFile
	ch.CommandArgs = preparedArgs
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

	var allowedFields []string
	t := reflect.TypeFor[plugin.ConfigOptionCommon]()
	for field := range t.Fields() {
		allowedFields = append(allowedFields, strings.ToLower(field.Name))
	}

	// Check configuration has no duplicate or empty entries
	known := make(map[string]bool, len(y.Config))
	var optionsByName []string
	for i, o := range y.Config {
		switch {
		case o.Name == "":
			errs = append(errs, fmt.Errorf("config[%d]: name is empty", i))
		case known[o.Name]:
			errs = append(errs, fmt.Errorf("config[%d]: duplicate name %q", i, o.Name))
		default:
			optionsByName = append(optionsByName, o.Name)
			known[o.Name] = true
		}
	}

	// check command variables correspond to options
	allowedVars := append(optionsByName, "config", "notification")
	commandArgs, err := prepareArrayCommand(y.Command)
	errs = append(errs, err)

	for _, commandArg := range commandArgs {
		for _, seg := range commandArg.value {
			if seg.isVariable() {
				varPathString := strings.Join(seg.VariablePath, ".")
				switch seg.VariablePath[0] {
				case "config":
					for _, commandVar := range seg.VariablePath {

						if !slices.Contains(allowedVars, commandVar) {
							errs = append(errs, fmt.Errorf(
								"variable %s : field is not allowed: %v",
								varPathString,
								commandVar,
							))
						}
					}
				case "notification":
					payloadType := reflect.TypeFor[plugin.PayloadV1]()
					if err := checkPath(payloadType, seg.VariablePath[1:]); err != nil {
						errs = append(errs, fmt.Errorf("variable %s: %w", varPathString, err))
					}
				default:
					errs = append(errs, fmt.Errorf("invalid format: variables must begin with \"config\" or \"notification\": %s", varPathString))
				}

			}
		}
	}

	return errors.Join(errs...)
}

// Look into environment variables, check invalid characters. Variables not supported (yet)
func (env Environment) prepare() ([]cmdArg, error) {
	var out []cmdArg
	for name, val := range env {
		c := cmdArg{
			"",
			nil,
			nil,
		}
		if !isValidName(name) {
			return nil, fmt.Errorf("env %q: invalid variable name", name)
		}
		v, err := resolveStringToValue(val)
		if err != nil {
			return nil, fmt.Errorf("env %q: %w", name, err)
		}
		if v != nil {
			c.value = v
		}
		out = append(out, c)
	}

	return out, nil
}

func (env Environment) render() ([]cmdArg, error) {
	var out []cmdArg
	for name, val := range env {
		c := cmdArg{
			"",
			nil,
			nil,
		}
		if !isValidName(name) {
			return nil, fmt.Errorf("env %q: invalid variable name", name)
		}
		v, err := resolveStringToValue(val)
		if err != nil {
			return nil, fmt.Errorf("env %q: %w", name, err)
		}
		if v != nil {
			c.value = v
		}
		out = append(out, c)
	}

	return out, nil
}

func isValidName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '_', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// this one breaks down a string command into individual arguments and assembles them into
// array of command arguments
func prepareStringCommand(s string) ([]cmdArg, error) {
	var commandArguments []cmdArg

	args, err := shlex.Split(s)
	if err != nil {
		return nil, fmt.Errorf("failed to split command arguments: %w", err)
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("no command specified")
	}

	for _, arg := range args {
		var commandArgument cmdArg
		a, err := resolveStringToValue(arg)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve argument macros: %w", err)
		}
		if a != nil {
			commandArgument.value = a
		}

		commandArguments = append(commandArguments, commandArgument)
	}

	return commandArguments, nil
}

// this one takes array of yaml arguments and turns it into an array of command arguments
func prepareArrayCommand(a []any) ([]cmdArg, error) {
	var commandArguments []cmdArg

	for i, arg := range a {
		commandArgument := cmdArg{
			"",
			nil,
			nil,
		}
		switch v := arg.(type) {
		case map[string]any:
			// Reject typos like "setif" or "valeu" instead of silently ignoring them.
			for field := range v {
				if field != "key" && field != "value" && field != "set_if" {
					return nil, fmt.Errorf("arg %d: unknown field %q (allowed: key, value, set_if)", i, field)
				}
			}

			// If key exists, write it down, no variables supported, only plain text
			if raw, exists := v["key"]; exists {
				key, ok := raw.(string)
				if !ok {
					return nil, fmt.Errorf("arg %d: key must be a string", i)
				}
				commandArgument.key = key
			}

			// Strings are the core for parser so if problems arise it crashes
			val, err := toString(v["value"])
			if err != nil {
				return nil, fmt.Errorf("arg %d: value could not be converted: %w", i, err)
			}

			// The string is resolved to whatever it really is – a singular variable, a whole flag, or a flag with a variable
			parsedVal, err := resolveStringToValue(val)
			if err != nil {
				return nil, fmt.Errorf("arg %d: value could not be parsed: %w", i, err)
			}
			if parsedVal != nil {
				commandArgument.value = parsedVal
			}

			// set_if is basically the same as value, except we only retain variable, or nothing at all if no variable is there
			// no complex evaluation yet
			setIf, err := toString(v["set_if"])
			if err != nil {
				return nil, fmt.Errorf("arg %d: value could not be converted: %w", i, err)
			}
			parsedSetIf, err := resolveStringToValue(setIf)
			if err != nil {
				return nil, fmt.Errorf("arg %d: set_if could not be parsed: %w", i, err)
			}
			if parsedSetIf != nil {
				commandArgument.setIf = parsedSetIf[0].VariablePath
			} else {
				return nil, fmt.Errorf("arg %d: set_if syntax invalid", i)
			}
		case string:
			// Normal strings can have variables too!
			parsedVal, err := resolveStringToValue(v)
			if err != nil {
				return nil, fmt.Errorf("arg %d: value could not be parsed: %w", i, err)
			}
			if parsedVal != nil {
				commandArgument.value = parsedVal
			}
		default:
			parsed, err := toString(v)
			if err != nil {
				return nil, fmt.Errorf("arg %d: value could not be converted: %w", i, err)
			}
			commandArgument.value = Value{Segment{Literal: parsed}}
		}
		commandArguments = append(commandArguments, commandArgument)
	}
	return commandArguments, nil
}

// renderTemplate glues one template's segments into a single string.
func renderTemplate(t []Segment, data map[string]any) (string, error) {
	var sb strings.Builder
	for _, s := range t {
		if !s.isVariable() {
			sb.WriteString(s.Literal)
			continue
		}
		v, err := lookup(s.VariablePath, data)
		if err != nil {
			return "", err
		}
		str, err := toString(v)
		if err != nil {
			return "", err
		}
		sb.WriteString(str)
	}
	return sb.String(), nil
}

// this one evaluates command arguments and turns them into simply an array of strings
// to be fed to exec.command
func renderCommand(args []cmdArg, data map[string]any) ([]string, error) {
	var rendered []string

	for _, a := range args {
		// 1. set_if present? evaluate it; false (or missing) = skip whole arg
		if len(a.setIf) != 0 {
			v, err := lookup(a.setIf, data)
			if err != nil || !isTruthy(v) {
				continue
			}
		}
		// 2. key (optional)
		if a.key != "" {
			rendered = append(rendered, a.key)
		}
		// 3. value
		val, err := renderTemplate(a.value, data)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, val)
	}

	return rendered, nil
}

// this one looks up variable data in the json
func lookup(path []string, data map[string]any) (string, error) {
	var cur any = data
	for i, step := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("%s is not an object", strings.Join(path[:i], "."))
		}
		if cur, ok = m[step]; !ok {
			return "", fmt.Errorf("no such variable %s", strings.Join(path[:i+1], "."))
		}
	}
	curStr, err := toString(cur)
	if err != nil {
		return "", err
	}
	return curStr, nil
}

// this one checks payload structure itself to be able to validate yaml at startup
func checkPath(t reflect.Type, path []string) error {
	var marshalerType = reflect.TypeFor[json.Marshaler]()
	for i, step := range path {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}

		if t.Kind() != reflect.Struct ||
			t.Implements(marshalerType) || reflect.PointerTo(t).Implements(marshalerType) {
			return fmt.Errorf("%s has no fields", strings.Join(path[:i], "."))
		}
		f, ok := fieldByJSONName(t, step)
		if !ok {
			return fmt.Errorf("no such field %s", strings.Join(path[:i+1], "."))
		}
		t = f.Type
	}
	return nil
}

// fieldByJSONName looks up structure field by its json name
func fieldByJSONName(t reflect.Type, name string) (reflect.StructField, bool) {
	for f := range t.Fields() { // Go 1.26; older version: for i := range t.NumField() { f := t.Field(i) ... }
		if !f.IsExported() {
			continue
		}
		tag, _, _ := strings.Cut(f.Tag.Get("json"), ",") // `json:"started_at,omitempty"` → "started_at"
		if tag == "-" {
			continue
		}
		if f.Anonymous && tag == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				if sf, ok := fieldByJSONName(ft, name); ok {
					return sf, true
				}
			}
			continue
		}
		if tag == "" {
			tag = f.Name // без тега JSON использует имя поля как есть
		}
		if tag == name {
			return f, true
		}
	}
	return reflect.StructField{}, false
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

// parseTemplate splits a string into Literal and Variable segments
//
//	"http://localhost:$config.port$/x" → [Lit "http://localhost:", Var config.port, Lit "/x"]
//	"$$"  → literal "$"
func resolveStringToValue(s string) (Value, error) {
	var segments Value
	var lit strings.Builder // collects literal text until the next variable

	flushLiteral := func() {
		if lit.Len() > 0 {
			segments = append(segments, Segment{Literal: lit.String()})
			lit.Reset()
		}
	}

	for i := 0; i < len(s); i++ {
		c := s[i]

		// normal character → part of the literal
		if c != '$' {
			lit.WriteByte(c)
			continue
		}

		// "$$" → escaped dollar, a literal "$"
		if i+1 < len(s) && s[i+1] == '$' {
			lit.WriteByte('$')
			i++ // skip the second $
			continue
		}

		// single "$" → a variable starts; find the closing "$"
		end := strings.IndexByte(s[i+1:], '$')
		if end == -1 {
			return nil, fmt.Errorf("unclosed variable at position %d in %q", i, s)
		}
		name := s[i+1 : i+1+end]

		path := strings.Split(name, ".")
		if slices.Contains(path, "") { // catches "$.a$", "$a..b$", "$a.$"
			return nil, fmt.Errorf("invalid variable name %q in %q", name, s)
		}

		flushLiteral() // save the text before the variable
		segments = append(segments, Segment{VariablePath: path})
		i += end + 1 // jump to the closing $; the loop's i++ moves past it
	}

	flushLiteral() // save the text after the last variable
	return segments, nil
}

func payloadToMap(payload *plugin.PayloadV1) (map[string]any, error) {
	raw, err := json.Marshal(payload) // struct → JSON bytes
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw)) // decoder reading those bytes
	dec.UseNumber()                              // keep numbers as text
	var payloadDecoded map[string]any
	err = dec.Decode(&payloadDecoded) // bytes → map[string]any
	if err != nil {
		return nil, err
	}

	return payloadDecoded, nil
}

func toString(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case int:
		return strconv.Itoa(x), nil
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(x), nil
	case nil:
		return "", nil
	case json.Number:
		return x.String(), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case uint64:
		return strconv.FormatUint(x, 10), nil
	default:
		return "", fmt.Errorf("can't put %T into a string", v)
	}
}
