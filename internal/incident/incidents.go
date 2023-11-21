package incident

import (
	"context"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icingadb/pkg/com"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"sync"
	"time"
)

// ErrSuperfluousStateChange indicates a superfluous state change being ignored and stopping further processing.
var ErrSuperfluousStateChange = errors.New("ignoring superfluous state change")

var (
	currentIncidents   = make(map[*object.Object]*Incident)
	currentIncidentsMu sync.Mutex
)

// LoadOpenIncidents loads all active (not yet closed) incidents from the database and restores all their states.
// Returns error on any database failure.
func LoadOpenIncidents(ctx context.Context, db *icingadb.DB, logger *logging.Logger, runtimeConfig *config.RuntimeConfig) error {
	logger.Info("Loading all active incidents from database")

	g, chCtx := errgroup.WithContext(ctx)

	incidents := make(chan *Incident)
	g.Go(func() error {
		defer close(incidents)

		rows, err := db.QueryxContext(chCtx, db.BuildSelectStmt(new(Incident), new(Incident))+` WHERE "recovered_at" IS NULL`)
		if err != nil {
			return err
		}
		// In case the incidents in the loop below are successfully traversed, rows is automatically closed and an
		// error is returned (if any), making this rows#Close() call a no-op. Escaping from this function unexpectedly
		// means we have a more serious problem, so in either case just disregard the error here.
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			i := NewIncident(db, nil, runtimeConfig, nil)
			if err := rows.StructScan(i); err != nil {
				return err
			}

			select {
			case incidents <- i:
			case <-chCtx.Done():
				return chCtx.Err()
			}
		}

		return rows.Err()
	})

	g.Go(func() error {
		bulks := com.Bulk(chCtx, incidents, db.Options.MaxPlaceholdersPerStatement, com.NeverSplit[*Incident])

		for {
			select {
			case <-chCtx.Done():
				return chCtx.Err()
			case bulk, ok := <-bulks:
				if !ok {
					return nil
				}

				g.Go(func() error {
					chunkLen := len(bulk)
					objectIds := make([]any, chunkLen)
					incidentIds := make([]any, chunkLen)
					incidentsById := make(map[any]*Incident, chunkLen)
					incidentsByObjId := make(map[any]*Incident, chunkLen)

					for k, i := range bulk {
						incidentsById[i.Id] = i
						incidentsByObjId[i.ObjectID.String()] = i

						objectIds[k] = i.ObjectID
						incidentIds[k] = i.Id
					}

					// Restore all incident objects matching the given object ids
					if err := restoreAttrsFor(chCtx, db, incidentsByObjId, new(object.Object), objectIds); err != nil {
						return errors.Wrap(err, "cannot restore incidents object")
					}

					// Restore object ID tags matching the given object ids
					if err := restoreAttrsFor(chCtx, db, incidentsByObjId, new(object.IdTagRow), objectIds); err != nil {
						return errors.Wrap(err, "cannot restore incident object ID tags")
					}

					// Restore object extra tags matching the given object ids
					if err := restoreAttrsFor(chCtx, db, incidentsByObjId, new(object.ExtraTagRow), objectIds); err != nil {
						return errors.Wrap(err, "cannot restore incident object ID tags")
					}

					// Restore all escalation states and incident rules matching the given incident ids.
					if err := restoreAttrsFor(chCtx, db, incidentsById, new(EscalationState), incidentIds); err != nil {
						return errors.Wrap(err, "cannot restore incident rule escalation states")
					}

					// Restore incident recipients matching the given incident ids.
					if err := restoreAttrsFor(chCtx, db, incidentsById, new(ContactRow), incidentIds); err != nil {
						return errors.Wrap(err, "cannot restore incident recipients")
					}

					for _, i := range incidentsById {
						i.logger = logger.With(zap.String("object", i.Object.DisplayName()),
							zap.String("incident", i.String()))

						object.Cache(i.Object)

						currentIncidentsMu.Lock()
						currentIncidents[i.Object] = i
						currentIncidentsMu.Unlock()

						i.RetriggerEscalations(&event.Event{
							Time:    time.Now(),
							Type:    event.TypeInternal,
							Message: "Incident reevaluation at daemon startup",
						})
					}

					return nil
				})
			}
		}
	})

	return g.Wait()
}

func GetCurrent(
	ctx context.Context, db *icingadb.DB, obj *object.Object, logger *logging.Logger, runtimeConfig *config.RuntimeConfig,
	create bool,
) (*Incident, bool, error) {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	created := false
	currentIncident := currentIncidents[obj]

	if currentIncident == nil && create {
		created = true

		incidentLogger := logger.With(zap.String("object", obj.DisplayName()))
		currentIncident = NewIncident(db, obj, runtimeConfig, incidentLogger)

		currentIncidents[obj] = currentIncident
	}

	if !created && currentIncident != nil {
		currentIncident.Lock()
		defer currentIncident.Unlock()

		if err := currentIncident.restoreRecipients(ctx); err != nil {
			return nil, false, err
		}
	}

	return currentIncident, created, nil
}

func RemoveCurrent(obj *object.Object) {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	currentIncident := currentIncidents[obj]

	if currentIncident != nil {
		delete(currentIncidents, obj)
	}
}

// GetCurrentIncidents returns a map of all incidents for debugging purposes.
func GetCurrentIncidents() map[int64]*Incident {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	m := make(map[int64]*Incident)
	for _, incident := range currentIncidents {
		m[incident.Id] = incident
	}
	return m
}

// ProcessEvent from an event.Event.
//
// This function first gets this Event's object.Object and its incident.Incident. Then, after performing some safety
// checks, it calls the Incident.ProcessEvent method.
//
// The returned error might be wrapped around ErrSuperfluousStateChange.
func ProcessEvent(
	ctx context.Context,
	db *icingadb.DB,
	logs *logging.Logging,
	runtimeConfig *config.RuntimeConfig,
	ev *event.Event,
) error {
	obj, err := object.FromEvent(ctx, db, ev)
	if err != nil {
		return fmt.Errorf("cannot sync event object: %w", err)
	}

	createIncident := ev.Severity != event.SeverityNone && ev.Severity != event.SeverityOK
	currentIncident, created, err := GetCurrent(
		ctx,
		db,
		obj,
		logs.GetChildLogger("incident"),
		runtimeConfig,
		createIncident)
	if err != nil {
		return fmt.Errorf("cannot get current incident for %q: %w", obj.DisplayName(), err)
	}

	if currentIncident == nil {
		switch {
		case ev.Type == event.TypeAcknowledgement:
			return fmt.Errorf("%q does not have an active incident, ignoring acknowledgement event from source %d",
				obj.DisplayName(), ev.SourceId)
		case ev.Severity != event.SeverityOK:
			panic(fmt.Sprintf("cannot process event %v with a non-OK state %v without a known incident", ev, ev.Severity))
		default:
			return fmt.Errorf("%w: ok state event from source %d", ErrSuperfluousStateChange, ev.SourceId)
		}
	}

	return currentIncident.ProcessEvent(ctx, ev, created)
}

// restoreAttrsFor restores the attributes of the given subject type from the database.
//
// It bulks SELECT the info from the database via a `SELECT ... WHERE ... IN(?)` query scoped to the specified args.
// The column name for the where clause is determined automatically based on the provided subject type.
// Currently, it only supports object.Object, object.IdTagRow, object.ExtraTagRow, EscalationState, ContactRow types
// and panics if something else is provided.
//
// Note, this function accesses/modifies the specified incidents map without obtaining any locks
// and must be protected against simultaneous write operations if a shared map is used.
func restoreAttrsFor(ctx context.Context, db *icingadb.DB, incidents map[any]*Incident, subject any, scopes []any) error {
	var (
		// Name of the column used to filter the database query by.
		column string
		// A callback set based on the specified subject and called after each successful row retrieval.
		restore func(any)
		// A factory func set based on the given subject and called before each row scan.
		factory func() any
	)

	switch subject.(type) {
	case *EscalationState:
		column = "incident_id"
		factory = func() any { return new(EscalationState) }
		restore = func(r any) {
			state := r.(*EscalationState)
			i := incidents[state.IncidentID]
			i.EscalationState[state.RuleEscalationID] = state

			// Restore the incident rule matching the current escalation state if any.
			i.runtimeConfig.RLock()
			escalation := i.runtimeConfig.GetRuleEscalation(state.RuleEscalationID)
			if escalation != nil {
				i.Rules[escalation.RuleID] = struct{}{}
			}
			i.runtimeConfig.RUnlock()
		}
	case *ContactRow:
		column = "incident_id"
		factory = func() any { return new(ContactRow) }
		restore = func(r any) {
			c := r.(*ContactRow)
			incidents[c.IncidentID].Recipients[c.Key] = &RecipientState{Role: c.Role}
		}
	case *object.IdTagRow:
		column = "object_id"
		factory = func() any { return new(object.IdTagRow) }
		restore = func(r any) {
			id := r.(*object.IdTagRow)
			incidents[id.ObjectId.String()].Object.Tags[id.Tag] = id.Value
		}
	case *object.ExtraTagRow:
		column = "object_id"
		factory = func() any { return new(object.ExtraTagRow) }
		restore = func(r any) {
			extraTag := r.(*object.ExtraTagRow)
			incidents[extraTag.ObjectId.String()].Object.ExtraTags[extraTag.Tag] = extraTag.Value
		}
	case *object.Object:
		column = "id"
		ev := &event.Event{Tags: make(map[string]string), ExtraTags: make(map[string]string)}
		factory = func() any { return object.New(db, ev) }
		restore = func(r any) {
			obj := r.(*object.Object)
			incidents[obj.ID.String()].Object = obj
		}
	default: // should never happen!
		panic(fmt.Sprintf("invalid database subject for incient#restoreAttrsFor() provided: %v", subject))
	}

	query := fmt.Sprintf("%s WHERE %q IN(?)", db.BuildSelectStmt(subject, subject), column)
	stmt, args, err := sqlx.In(query, scopes)
	if err != nil {
		return errors.Wrapf(err, "cannot build placeholders for %q", query)
	}

	rows, err := db.QueryxContext(ctx, db.Rebind(stmt), args...)
	if err != nil {
		return err
	}
	// In case the records in the loop below are successfully traversed, rows is automatically closed and an
	// error is returned (if any), making this rows#Close() call a no-op. Escaping from this function unexpectedly
	// means we have a more serious problem, so in either case just discard the error here.
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		row := factory()
		if err = rows.StructScan(row); err != nil {
			return err
		}

		restore(row)
	}

	return rows.Err()
}
