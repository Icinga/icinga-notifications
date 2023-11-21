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

var (
	currentIncidents   = make(map[*object.Object]*Incident)
	currentIncidentsMu sync.Mutex
)

// LoadOpenIncidents loads all active (not yet closed) incidents from the database and restores all their states.
// Returns error ony database failure.
func LoadOpenIncidents(ctx context.Context, db *icingadb.DB, logger *logging.Logger, runtimeConfig *config.RuntimeConfig) error {
	logger.Info("Loading all active incidents from database")

	g, childCtx := errgroup.WithContext(ctx)

	incidents := make(chan *Incident)
	g.Go(func() error {
		defer close(incidents)

		rows, err := db.QueryxContext(childCtx, db.BuildSelectStmt(&Incident{}, &Incident{})+` WHERE "recovered_at" IS NULL`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			i := NewIncident(db, nil, runtimeConfig, nil)
			if err := rows.StructScan(i); err != nil {
				return err
			}

			select {
			case incidents <- i:
			case <-childCtx.Done():
				return childCtx.Err()
			}
		}

		return nil
	})

	g.Go(func() error {
		bulks := com.Bulk(childCtx, incidents, db.Options.MaxPlaceholdersPerStatement, com.NeverSplit[*Incident])

		expandArgs := func(subject any, bindVals []any, column string) (string, []any, error) {
			query := fmt.Sprintf("%s WHERE %q IN(?)", db.BuildSelectStmt(subject, subject), column)
			stmt, args, err := sqlx.In(query, bindVals)
			if err != nil {
				return "", nil, errors.Wrapf(err, "cannot build placeholders for %q", query)
			}

			return stmt, args, nil
		}

		for {
			select {
			case <-childCtx.Done():
				return childCtx.Err()
			case bulk, ok := <-bulks:
				if !ok {
					return nil
				}

				g.Go(func() error {
					objectIds := make([]any, len(bulk))
					incidentIds := make([]any, len(bulk))
					incidentsByObjId := make(map[string]*Incident)

					for i := range bulk {
						objectIds[i] = bulk[i].ObjectID
						incidentIds[i] = bulk[i].Id
						incidentsByObjId[bulk[i].ObjectID.String()] = bulk[i]
					}

					stmt, args, err := expandArgs(new(object.Object), objectIds, "id")
					if err != nil {
						return err
					}

					objRows, err := db.QueryxContext(childCtx, db.Rebind(stmt), args...)
					if err != nil {
						return errors.Wrap(err, "cannot fetch incident objects")
					}
					defer func() { _ = objRows.Close() }()

					for objRows.Next() {
						obj := object.New(db, &event.Event{Tags: make(map[string]string), ExtraTags: make(map[string]string)})
						if err = objRows.StructScan(obj); err != nil {
							return err
						}

						incidentsByObjId[obj.ID.String()].Object = obj
					}

					// Object ID tags...
					stmt, args, err = expandArgs(new(object.IdTagRow), objectIds, "object_id")
					if err != nil {
						return err
					}

					idRows, err := db.QueryxContext(childCtx, db.Rebind(stmt), args...)
					if err != nil {
						return errors.Wrap(err, "cannot fetch object id tags")
					}
					defer func() { _ = idRows.Close() }()

					for idRows.Next() {
						idtag := new(object.IdTagRow)
						if err = idRows.StructScan(idtag); err != nil {
							return err
						}

						incidentsByObjId[idtag.ObjectId.String()].Object.Tags[idtag.Tag] = idtag.Value
					}

					// Object extra tags...
					stmt, args, err = expandArgs(new(object.ExtraTagRow), objectIds, "object_id")
					if err != nil {
						return err
					}

					extraTagRows, err := db.QueryxContext(childCtx, db.Rebind(stmt), args...)
					if err != nil {
						return errors.Wrap(err, "cannot fetch object extra tags")
					}
					defer func() { _ = extraTagRows.Close() }()

					for extraTagRows.Next() {
						extraTag := new(object.ExtraTagRow)
						if err = extraTagRows.StructScan(extraTag); err != nil {
							return err
						}

						incidentsByObjId[extraTag.ObjectId.String()].Object.ExtraTags[extraTag.Tag] = extraTag.Value
					}

					// Restore all escalation states matching the current incident ids.
					stmt, args, err = expandArgs(new(EscalationState), incidentIds, "incident_id")
					if err != nil {
						return err
					}

					statesRows, err := db.QueryxContext(childCtx, db.Rebind(stmt), args...)
					if err != nil {
						return errors.Wrap(err, "cannot restore incident rule escalation states")
					}
					defer func() { _ = statesRows.Close() }()

					for statesRows.Next() {
						state := new(EscalationState)
						if err = statesRows.StructScan(state); err != nil {
							return err
						}

						for _, i := range incidentsByObjId {
							if i.ID() == state.IncidentID {
								i.EscalationState[state.RuleEscalationID] = state
								break
							}
						}
					}

					// Restore incident recipients...
					stmt, args, err = expandArgs(new(ContactRow), incidentIds, "incident_id")
					if err != nil {
						return err
					}

					recipientRows, err := db.QueryxContext(childCtx, db.Rebind(stmt), args...)
					if err != nil {
						return errors.Wrap(err, "cannot restore incident recipients")
					}
					defer func() { _ = recipientRows.Close() }()

					for recipientRows.Next() {
						contact := new(ContactRow)
						if err = recipientRows.StructScan(contact); err != nil {
							return err
						}

						for _, i := range incidentsByObjId {
							if i.ID() == contact.IncidentID {
								i.Recipients[contact.Key] = &RecipientState{Role: contact.Role}
								break
							}
						}
					}

					for _, i := range incidentsByObjId {
						i.logger = logger.With(zap.String("object", i.Object.DisplayName()), zap.String("incident", i.String()))

						i.RestoreEscalationStateRules()
						i.RetriggerEscalations(&event.Event{
							Time:    time.Now(),
							Type:    event.TypeInternal,
							Message: "Incident reevaluation at daemon startup",
						})

						object.Cache(i.Object)

						currentIncidentsMu.Lock()
						currentIncidents[i.Object] = i
						currentIncidentsMu.Unlock()
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
		currentIncident.ObjectID = obj.ID

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
		m[incident.ID()] = incident
	}
	return m
}
