package event

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-notifications/internal/pool"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/theory/jsonpath"
	"go.uber.org/zap/zapcore"
)

// Event received of a specified Type for internal processing.
//
// This is a representation of an event received from an external source with additional metadata with sole
// purpose of being used for internal processing. All the JSON serializable fields are these inherited from
// the base event type, and are used to decode the request body. Currently, there is no Event being marshalled
// into its JSON representation.
type Event struct {
	Time     time.Time `json:"-"`
	SourceId int64     `json:"-"`

	baseEv.Event `json:",inline"`

	// evaluatedRelations caches the results of evaluating JSONPath exprs against the Relations field of this event.
	//
	// This is used to avoid evaluating the same JSONPath expression multiple times during rule evaluation of an event,
	// as the same filter column can be used in multiple conditions of a rule or even multiple event rules.
	evaluatedRelations map[string]jsonpath.NodeList
}

// CompleteURL returns the complete URL for this event by combining the Icinga Web 2 URL with the event's own url field.
//
// If the event's url field is an absolute URL, is empty, or it can't be URL parsed, then this method is a no-op.
// Otherwise, it is resolved against the provided Icinga Web 2 URL to form a complete URL.
func (e *Event) CompleteURL(icingaWebBaseUrl *url.URL) {
	if e.URL == "" {
		return
	}

	u, err := url.Parse(strings.TrimLeft(e.URL, "/"))
	if err != nil {
		return // leave it as is if it cannot be parsed as a URL
	}

	if !u.IsAbs() {
		// Actually, the Icinga Web 2 base url should always contain the trailing slash, but just in
		// case it doesn't, make sure to add it before resolving the event URL against it to avoid
		// losing the last path segment of the base url.
		e.URL = icingaWebBaseUrl.JoinPath("/").ResolveReference(u).String()
	}
}

// Validate validates the current event state.
// Returns an error if it detects a misconfigured field.
func (e *Event) Validate() error {
	if err := e.Event.Validate(); err != nil {
		return err
	}

	if e.Time.IsZero() {
		return fmt.Errorf("invalid event: time must not be empty")
	}

	if e.SourceId == 0 {
		return fmt.Errorf("invalid event: source ID must not be empty")
	}

	return nil
}

// MarshalLogObject implements the [zapcore.ObjectMarshaler] interface to allow logging the event as a structured object.
func (e *Event) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddString("name", e.Name)
	encoder.AddTime("time", e.Time)
	encoder.AddInt64("source_id", e.SourceId)
	encoder.AddString("severity", e.Severity.String())

	if e.Muted.Valid {
		encoder.AddBool("muted", e.Muted.Bool)
		encoder.AddString("muted_reason", e.MutedReason)
	}
	if e.OpenOrEscalate() {
		encoder.AddBool("incident", true)
	}
	if e.CloseIncident() {
		encoder.AddBool("close", true)
	}
	if e.NotifyRecipients() {
		encoder.AddBool("notify", true)
	}

	return encoder.AddObject("tags", zapcore.ObjectMarshalerFunc(func(objectEncoder zapcore.ObjectEncoder) error {
		for key, value := range e.Tags {
			objectEncoder.AddString(key, value)
		}
		return nil
	}))
}

// ExtractMissingRelations determines which of the given filter columns are missing in the Relations field of this event.
//
// It evaluates the filter columns as JSONPath expressions against the Relations field and returns
// a list of filter columns as valid JSONPaths that do not have any matching nodes in the Relations
// field and are not part of the CompleteRelations field. For filter columns that do have matching
// nodes, it caches the evaluated nodes for potential later use during rules evaluation.
func (e *Event) ExtractMissingRelations(filterColumns ...[]string) []string {
	if e.evaluatedRelations == nil {
		e.evaluatedRelations = make(map[string]jsonpath.NodeList)
	}

	jpp := pool.GetJSONPathParser()
	defer pool.PutJSONPathParser(jpp)

	// completePaths caches the parsed JSONPath expressions of the complete relations.
	completePaths := map[string]*jsonpath.Path{}
	var result []string
filterColumnsLoop:
	for _, columns := range filterColumns {
		var missing []string
		for _, filterColumn := range columns {
			if _, cached := e.evaluatedRelations[filterColumn]; cached {
				continue filterColumnsLoop
			}
			// This should never panic, as the filter columns have already been validated when loading the rules.
			path := jpp.MustParse(utils.PrefixWithJSONPathRootSelector(filterColumn))
			if nodes := path.Select(e.Relations); len(nodes) == 0 {
				isComplete := slices.ContainsFunc(e.CompleteRelations, func(relation string) bool {
					completePath, ok := completePaths[relation]
					if !ok {
						// If we can't parse the provided relation as a JSONPath expression, just ignore it and treat
						// it as a non-matching relation (but still cache the failed parsing result to avoid trying to
						// parse it again for the next filter column).
						completePath, _ = jpp.Parse(utils.PrefixWithJSONPathRootSelector(relation))
						completePaths[relation] = completePath
					}
					return completePath != nil && strings.HasPrefix(path.String(), completePath.String())
				})
				if !isComplete {
					missing = append(missing, path.String())
				}
			} else {
				// Cache the evaluated nodes for this filter column for potentially later use during rules evaluation.
				e.evaluatedRelations[filterColumn] = nodes
				// Stop evaluating the remaining filter columns of this list, as we only need to
				// find one matching column for the condition to be potentially satisfied.
				continue filterColumnsLoop
			}
		}
		for _, column := range missing {
			if !slices.Contains(result, column) {
				result = append(result, column)
			}
		}
	}
	return result
}

func (e *Event) EvalEqual(attrs, value any) (bool, error) {
	return slices.ContainsFunc(e.retrieveValuesFor(attrs), func(v any) bool {
		result, err := utils.CompareAny(v, value)
		if err != nil {
			return false
		}
		return result == 0
	}), nil
}

func (e *Event) EvalLess(attrs, value any) (bool, error) {
	return slices.ContainsFunc(e.retrieveValuesFor(attrs), func(v any) bool {
		result, err := utils.CompareAny(v, value)
		if err != nil {
			return false
		}
		return result < 0
	}), nil
}

func (e *Event) EvalLike(attrs, value any) (bool, error) {
	// Wildcard matching can't be implemented with types other than string, so convert it to a string unconditionally.
	rgx, err := regexp.Compile(fmt.Sprint(value))
	if err != nil {
		return false, err
	}

	return slices.ContainsFunc(e.retrieveValuesFor(attrs), func(v any) bool {
		if _, ok := v.(map[string]any); ok {
			return false
		}
		if _, ok := v.([]any); ok {
			return false
		}
		return rgx.MatchString(fmt.Sprint(v))
	}), nil
}

func (e *Event) EvalLessOrEqual(attrs, value any) (bool, error) {
	return slices.ContainsFunc(e.retrieveValuesFor(attrs), func(v any) bool {
		result, err := utils.CompareAny(v, value)
		if err != nil {
			return false
		}
		return result <= 0
	}), nil
}

func (e *Event) EvalExists(attrs any) bool { return len(e.retrieveValuesFor(attrs)) > 0 }

// retrieveValuesFor retrieves the values for the given key from the Relations field of this event.
func (e *Event) retrieveValuesFor(attrs any) jsonpath.NodeList {
	if e.evaluatedRelations == nil {
		e.evaluatedRelations = make(map[string]jsonpath.NodeList)
	}

	if attrs, ok := attrs.([]string); ok {
		jpp := pool.GetJSONPathParser()
		defer pool.PutJSONPathParser(jpp)

		for _, attr := range attrs {
			attr := fmt.Sprint(attr)
			nodes, cached := e.evaluatedRelations[attr]
			if !cached {
				path := jpp.MustParse(utils.PrefixWithJSONPathRootSelector(attr))
				nodes = path.Select(e.Relations)
				e.evaluatedRelations[attr] = nodes
			}
			if len(nodes) > 0 {
				return nodes
			}
		}
	}
	return nil
}
