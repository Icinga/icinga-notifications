package event

import (
	"context"
	"errors"
	"fmt"
	"github.com/icinga/icinga-go-library/database"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/pool"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/jmoiron/sqlx"
	"github.com/theory/jsonpath"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// ErrSuperfluousStateChange indicates a superfluous state change being ignored and stopping further processing.
var ErrSuperfluousStateChange = errors.New("ignoring superfluous state change")

// ErrSuperfluousMuteUnmuteEvent indicates that a superfluous mute or unmute event is being ignored and is
// triggered when trying to mute/unmute an already muted/unmuted incident.
var ErrSuperfluousMuteUnmuteEvent = errors.New("ignoring superfluous (un)mute event")

// Event received of a specified Type for internal processing.
//
// This is a representation of an event received from an external source with additional metadata with sole
// purpose of being used for internal processing. All the JSON serializable fields are these inherited from
// the base event type, and are used to decode the request body. Currently, there is no Event being marshalled
// into its JSON representation.
type Event struct {
	Time     time.Time `json:"-"`
	SourceId int64     `json:"-"`
	ID       int64     `json:"-"`

	baseEv.Event `json:",inline"`

	// evaluatedRelations caches the results of evaluating JSONPath exprs against the Relations field of this event.
	//
	// This is used to avoid evaluating the same JSONPath expression multiple times during rule evaluation of an event,
	// as the same filter column can be used in multiple conditions of a rule or even multiple event rules.
	evaluatedRelations map[string]jsonpath.NodeList
}

// CompleteURL prefixes the URL with the given Icinga Web 2 base URL unless it already carries a URL or is empty.
func (e *Event) CompleteURL(icingaWebBaseUrl string) {
	if e.URL == "" {
		return
	}

	if !strings.HasSuffix(icingaWebBaseUrl, "/") {
		icingaWebBaseUrl += "/"
	}

	u, err := url.Parse(e.URL)
	if err != nil || u.Scheme == "" {
		e.URL = icingaWebBaseUrl + e.URL
	}
}

// Validate validates the current event state.
// Returns an error if it detects a misconfigured field.
func (e *Event) Validate() error {
	if len(e.Tags) == 0 {
		return fmt.Errorf("invalid event: tags must not be empty")
	}

	for tag := range e.Tags {
		if len(tag) > 255 {
			return fmt.Errorf("invalid event: tag %q is too long, at most 255 chars allowed, %d given", tag, len(tag))
		}
	}

	if e.SourceId == 0 {
		return fmt.Errorf("invalid event: source ID must not be empty")
	}

	if e.Severity != baseEv.SeverityNone && e.Type != baseEv.TypeState {
		return fmt.Errorf("invalid event: if 'severity' is set, 'type' must be set to %q", baseEv.TypeState)
	}
	if e.Type == baseEv.TypeMute && (!e.Mute.Valid || !e.Mute.Bool) {
		return fmt.Errorf("invalid event: 'mute' must be true if 'type' is set to %q", baseEv.TypeMute)
	}
	if e.Type == baseEv.TypeUnmute && (!e.Mute.Valid || e.Mute.Bool) {
		return fmt.Errorf("invalid event: 'mute' must be false if 'type' is set to %q", baseEv.TypeUnmute)
	}
	if e.Mute.Valid && e.Mute.Bool && e.MuteReason == "" {
		return fmt.Errorf("invalid event: 'mute_reason' must not be empty if 'mute' is set")
	}

	if e.Type == baseEv.TypeUnknown {
		return errors.New("invalid event: missing type")
	}

	return nil
}

// SetMute alters the event mute and mute reason.
func (e *Event) SetMute(muted bool, reason string) {
	e.Mute = types.Bool{Valid: true, Bool: muted}
	e.MuteReason = reason
}

func (e *Event) String() string {
	return fmt.Sprintf("[time=%s type=%q severity=%s]", e.Time, e.Type, e.Severity.String())
}

// Sync transforms this event to *event.EventRow and synchronises with the database.
func (e *Event) Sync(ctx context.Context, tx *sqlx.Tx, db *database.DB, objectId types.Binary) error {
	if e.ID != 0 {
		return nil
	}

	eventRow := NewEventRow(e, objectId)
	eventID, err := database.InsertObtainID(ctx, tx, database.BuildInsertStmtWithout(db, eventRow, "id"), eventRow)
	if err == nil {
		e.ID = eventID
	}

	return err
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

// EventRow represents a single event database row and isn't an in-memory representation of an event.
type EventRow struct {
	ID         int64           `db:"id"`
	Time       types.UnixMilli `db:"time"`
	ObjectID   types.Binary    `db:"object_id"`
	Type       types.String    `db:"type"`
	Severity   baseEv.Severity `db:"severity"`
	Username   types.String    `db:"username"`
	Message    types.String    `db:"message"`
	Mute       types.Bool      `db:"mute"`
	MuteReason types.String    `db:"mute_reason"`
}

// TableName implements the contracts.TableNamer interface.
func (er *EventRow) TableName() string {
	return "event"
}

func NewEventRow(e *Event, objectId types.Binary) *EventRow {
	return &EventRow{
		Time:       types.UnixMilli(e.Time),
		ObjectID:   objectId,
		Type:       types.MakeString(e.Type.String(), types.TransformEmptyStringToNull),
		Severity:   e.Severity,
		Username:   types.MakeString(e.Username, types.TransformEmptyStringToNull),
		Message:    types.MakeString(e.Message, types.TransformEmptyStringToNull),
		Mute:       e.Mute,
		MuteReason: types.MakeString(e.MuteReason, types.TransformEmptyStringToNull),
	}
}
