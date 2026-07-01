package incident

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/icinga/icinga-go-library/com"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/logging"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

var (
	currentIncidents   = make(map[*object.Object]*Incident)
	currentIncidentsMu sync.Mutex
)

// LoadOpenIncidents loads all active (not yet closed) incidents from the database and restores all their states.
// Returns error on any database failure.
func LoadOpenIncidents(ctx context.Context, db *database.DB, logger *logging.Logger, runtimeConfig *config.RuntimeConfig) error {
	logger.Info("Loading all active incidents from database")

	g, ctx := errgroup.WithContext(ctx)

	incidents := make(chan *Incident)
	g.Go(func() error {
		defer close(incidents)

		//nolint:sqlclosecheck // False positive, does not detect deferred close: https://github.com/ryanrolds/sqlclosecheck/issues/43
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
							Time: time.Now(),
							Event: baseEv.Event{
								Incident: types.MakeBool(true),
								Message:  fmt.Sprintf("Incident reached age %v (daemon was restarted)", time.Since(i.StartedAt.Time())),
							},
						})
					}

					return nil
				})
			}
		}
	})

	return g.Wait()
}

func GetCurrent(db *database.DB, obj *object.Object, logger *logging.Logger, runtimeConfig *config.RuntimeConfig, create bool) *Incident {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	currentIncident := currentIncidents[obj]

	if currentIncident == nil && create {
		incidentLogger := logger.With(zap.String("object", obj.DisplayName()))
		currentIncident = NewIncident(db, obj, runtimeConfig, incidentLogger)

		currentIncidents[obj] = currentIncident
	}

	return currentIncident
}

func RemoveCurrent(obj *object.Object) {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	delete(currentIncidents, obj)
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

// GetCurrentIncidentsForSource returns a slice containing all currently open incidents belonging to a source.
func GetCurrentIncidentsForSource(sourceID int64) []*Incident {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	var result []*Incident
	for _, incident := range currentIncidents {
		if incident.Object.SourceID == sourceID {
			result = append(result, incident)
		}
	}
	return result
}

// ErrOpenIncidentWithoutSeverity is returned when an event tries to open a new incident without a severity.
var ErrOpenIncidentWithoutSeverity = errors.New("cannot open or escalate an incident without a severity")

// loadOpenIncidentForObject checks the database for an open Incident and adds it to currentIncidents.
//
// In HA setups, another node may have created an Incident we don't know about after loadOpenIncidents.
func loadOpenIncidentForObject(
	ctx context.Context,
	db *database.DB,
	logger *logging.Logger,
	runtimeConfig *config.RuntimeConfig,
	obj *object.Object,
) error {
	i := NewIncident(db, obj, runtimeConfig, logger.With(zap.String("object", obj.DisplayName())))

	stmt := db.Rebind(db.BuildSelectStmt(i, i) + ` WHERE "recovered_at" IS NULL AND "object_id" = ?`)
	err := db.GetContext(ctx, i, stmt, obj.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return errors.Wrap(err, "cannot load open incident for object")
	}

	i.logger = i.logger.With(zap.String("incident", i.String()))

	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()
	if currentIncidents[obj] == nil {
		currentIncidents[obj] = i
	}

	return nil
}

// ProcessEvent from an event.Event.
//
// This function first gets this Event's object.Object and its incident.Incident. Then, after performing some safety
// checks, it calls the Incident.ProcessEvent method.
//
// It might return [ErrOpenIncidentWithoutSeverity] if the event is trying to open an incident without a severity or
// [ErrSeverityChangeWithoutIncidentFlag] if the event is trying to change the severity of an incident without the
// incident flag set. In both cases, the listener should map these errors to a 400 Bad Request response to the source.
func ProcessEvent(
	ctx context.Context,
	db *database.DB,
	logs *logging.Logging,
	runtimeConfig *config.RuntimeConfig,
	ev *event.Event,
) error {
	o := object.Get(db, ev)

	if !HasCurrent(o) {
		err := loadOpenIncidentForObject(ctx, db, logs.GetChildLogger("incident"), runtimeConfig, o)
		if err != nil {
			return err
		}
	}

	if ev.OpenOrEscalate() && ev.Severity == baseEv.SeverityNone && !HasCurrent(o) {
		return ErrOpenIncidentWithoutSeverity
	}

	currentIncident := GetCurrent(
		db,
		o,
		logs.GetChildLogger("incident"),
		runtimeConfig,
		ev.OpenOrEscalate())

	if currentIncident == nil {
		if ev.OpenOrEscalate() {
			panic(fmt.Sprintf("BUG: incident should have been created for event %v, but it was not", ev))
		}
		return nil
	}

	if err := currentIncident.ProcessEvent(ctx, ev); errors.Is(err, ErrIncidentRecovered) {
		// If in an HA setup another node just have closed the incident, retry after the cache entry was removed.
		return ProcessEvent(ctx, db, logs, runtimeConfig, ev)
	} else {
		return err
	}
}

// HasCurrent returns true if there is an active incident for the given object.
func HasCurrent(obj *object.Object) bool {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	return currentIncidents[obj] != nil
}
