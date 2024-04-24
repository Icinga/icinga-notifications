package incident

import (
	"context"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/icinga/icingadb/pkg/com"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"sync"
	"time"
)

var (
	currentIncidents   = make(map[*object.Object]*Incident)
	currentIncidentsMu sync.Mutex
)

// LoadOpenIncidents loads all active (not yet closed) incidents from the database and restores all their states.
// Returns error on any database failure.
func LoadOpenIncidents(ctx context.Context, db *icingadb.DB, logger *logging.Logger, runtimeConfig *config.RuntimeConfig) error {
	logger.Info("Loading all active incidents from database")

	g, ctx := errgroup.WithContext(ctx)

	incidents := make(chan *Incident)
	g.Go(func() error {
		defer close(incidents)

		rows, err := db.QueryxContext(ctx, db.BuildSelectStmt(new(Incident), new(Incident))+` WHERE "recovered_at" IS NULL`)
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
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		return rows.Err()
	})

	g.Go(func() error {
		bulks := com.Bulk(ctx, incidents, db.Options.MaxPlaceholdersPerStatement, com.NeverSplit[*Incident])

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case bulk, ok := <-bulks:
				if !ok {
					return nil
				}

				g.Go(func() error {
					chunkLen := len(bulk)
					objectIds := make([]types.Binary, 0, chunkLen)
					incidentIds := make([]int64, 0, chunkLen)
					incidentsById := make(map[int64]*Incident, chunkLen)
					incidentsByObjId := make(map[string]*Incident, chunkLen)

					for _, i := range bulk {
						incidentsById[i.Id] = i
						incidentsByObjId[i.ObjectID.String()] = i

						objectIds = append(objectIds, i.ObjectID)
						incidentIds = append(incidentIds, i.Id)
					}

					// Restore all incident objects matching the given object ids
					if err := object.RestoreObjects(ctx, db, objectIds); err != nil {
						return err
					}

					// Restore all escalation states and incident rules matching the given incident ids.
					err := utils.ForEachRow[EscalationState](ctx, db, "incident_id", incidentIds, func(state *EscalationState) {
						i := incidentsById[state.IncidentID]
						i.EscalationState[state.RuleEscalationID] = state

						// Restore the incident rule matching the current escalation state if any.
						i.runtimeConfig.RLock()
						defer i.runtimeConfig.RUnlock()

						escalation := i.runtimeConfig.GetRuleEscalation(state.RuleEscalationID)
						if escalation != nil {
							i.Rules[escalation.RuleID] = struct{}{}
						}
					})
					if err != nil {
						return errors.Wrap(err, "cannot restore incident rule escalation states")
					}

					// Restore incident recipients matching the given incident ids.
					err = utils.ForEachRow[ContactRow](ctx, db, "incident_id", incidentIds, func(c *ContactRow) {
						incidentsById[c.IncidentID].Recipients[c.Key] = &RecipientState{Role: c.Role}
					})
					if err != nil {
						return errors.Wrap(err, "cannot restore incident recipients")
					}

					for _, i := range incidentsById {
						i.Object = object.GetFromCache(i.ObjectID)
						i.logger = logger.With(zap.String("object", i.Object.DisplayName()),
							zap.String("incident", i.String()))

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
// The returned error might be wrapped around event.ErrSuperfluousStateChange.
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
		// ignore non-state event without incident
		case ev.Severity == event.SeverityNone:
			return fmt.Errorf("%q does not have an active incident, ignoring %q event from source %d",
				obj.DisplayName(), ev.Type, ev.SourceId)
		case ev.Severity != event.SeverityOK:
			panic(fmt.Sprintf("cannot process event %v with a non-OK state %v without a known incident", ev, ev.Severity))
		default:
			return fmt.Errorf("%w: ok state event from source %d", event.ErrSuperfluousStateChange, ev.SourceId)
		}
	}

	return currentIncident.ProcessEvent(ctx, ev, created)
}
