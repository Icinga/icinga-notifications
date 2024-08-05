package incident

import (
	"context"
	"fmt"
	"github.com/icinga/icinga-go-library/com"
	"github.com/icinga/icinga-go-library/database"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config"
	"github.com/icinga/icinga-notifications/internal/event"
	"github.com/icinga/icinga-notifications/internal/object"
	"github.com/icinga/icinga-notifications/internal/utils"
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
func LoadOpenIncidents(ctx context.Context, db *database.DB, runtimeConfig *config.RuntimeConfig) error {
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
			i := NewIncident(nil, runtimeConfig)
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
						incidentsById[i.ID] = i
						incidentsByObjId[i.ObjectID.String()] = i

						objectIds = append(objectIds, i.ObjectID)
						incidentIds = append(incidentIds, i.ID)
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
						i.RuntimeConfig.RLock()
						defer i.RuntimeConfig.RUnlock()

						escalation := i.RuntimeConfig.GetRuleEntry(state.RuleEscalationID)
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
						i.isMuted = i.Object.IsMuted()
						i.Logger = i.Logger.With(zap.String("object", i.Object.DisplayName()),
							zap.String("incident", i.String()))

						currentIncidentsMu.Lock()
						currentIncidents[i.Object] = i
						currentIncidentsMu.Unlock()

						i.RetriggerEscalations(&event.Event{
							Time: time.Now(),
							Event: baseEv.Event{
								Type:    baseEv.TypeIncidentAge,
								Message: fmt.Sprintf("Incident reached age %v (daemon was restarted)", time.Since(i.StartedAt.Time())),
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

func GetCurrent(ctx context.Context, obj *object.Object, runtimeConfig *config.RuntimeConfig, create bool) (*Incident, error) {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	currentIncident := currentIncidents[obj]

	if currentIncident == nil && create {
		currentIncident = NewIncident(obj, runtimeConfig)

		currentIncidents[obj] = currentIncident
	}

	if currentIncident != nil {
		currentIncident.Lock()
		defer currentIncident.Unlock()

		if !currentIncident.StartedAt.Time().IsZero() {
			if err := currentIncident.restoreRecipients(ctx); err != nil {
				return nil, err
			}
		}
	}

	return currentIncident, nil
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
		m[incident.ID] = incident
	}
	return m
}

// ClearCache clears the current incidents cache.
//
// Primarily used for testing purposes to ensure a clean state between test runs.
func ClearCache() {
	currentIncidentsMu.Lock()
	defer currentIncidentsMu.Unlock()

	currentIncidents = make(map[*object.Object]*Incident)
}
