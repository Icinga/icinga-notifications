package config

import (
	"context"
	"errors"
	"fmt"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/types"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"time"
)

// IncrementalConfigurable specifies Getter methods required for types supporting incremental configuration loading.
type IncrementalConfigurable[PK comparable] interface {
	zapcore.ObjectMarshaler

	// GetPrimaryKey returns the primary key value.
	GetPrimaryKey() PK

	// GetChangedAt returns the changed_at value.
	GetChangedAt() types.UnixMilli

	// IsDeleted returns if this entry was marked as deleted.
	IsDeleted() bool
}

// IncrementalConfigurableInitAndValidatable defines a single method for new and updated elements to allow both
// initialization and validation, to be used within incrementalFetch.
type IncrementalConfigurableInitAndValidatable interface {
	// IncrementalInitAndValidate allows both to initialize and validates with an optional error.
	//
	// If an error is returned, the incrementalFetch function aborts the element in question.
	IncrementalInitAndValidate() error
}

// incrementalFetchBacklog is an offset for past changes to consider for incrementalFetch.
//
// It may happen that updates do not arrive in the database in order of their changed_at value. As this value is set by
// Icinga Notifications Web on some webserver, there might be a delta between setting the timestamp and the entry
// getting committed in the database. This delta may not be evenly distributed, especially when the web part processes
// multiple changes concurrently.
//
// Fetched elements can be compared based on their changed_at value and be ignored if it has not changed.
const incrementalFetchBacklog = 10 * time.Minute

// incrementalFetch queries all recently changed elements of BaseT and stores them in changeConfigSetField.
//
// The RuntimeConfig.configChangeTimestamps map contains the last known timestamp for each BaseT table. Only those
// elements where the changed_at SQL column is greater than the stored timestamp will be fetched and stored in the
// temporary RuntimeConfig.configChange ConfigSet. Later on, incrementalApplyPending merges it into the main ConfigSet.
func incrementalFetch[
	BaseT any,
	PK comparable,
	T interface {
		*BaseT
		IncrementalConfigurable[PK]
	},
](ctx context.Context, tx *sqlx.Tx, r *RuntimeConfig, changeConfigSetField *map[PK]T) error {
	startTime := time.Now()

	var typePtr T

	tableName := database.TableName(typePtr)
	changedAt, hasChangedAt := r.configChangeTimestamps[tableName]

	stmtLogger := r.logger.With(zap.String("table", tableName))

	var (
		stmt     = r.db.BuildSelectStmt(typePtr, typePtr)
		stmtArgs []any
	)
	if hasChangedAt {
		changedAtBacklog := types.UnixMilli(changedAt.Time().Add(-incrementalFetchBacklog))
		stmtLogger = stmtLogger.With(
			zap.Time("last_known_changed_at", changedAt.Time()),
			zap.Time("compared_changed_at", changedAtBacklog.Time()))
		stmt += ` WHERE "changed_at" > ?`
		stmtArgs = []any{changedAtBacklog}
	}

	stmt = r.db.Rebind(stmt + ` ORDER BY "changed_at"`)
	stmtLogger = stmtLogger.With(zap.String("query", stmt))

	var ts []T
	if err := tx.SelectContext(ctx, &ts, stmt, stmtArgs...); err != nil {
		stmtLogger.Errorw("Cannot execute query to fetch incremental config updates", zap.Error(err))
		return err
	}

	*changeConfigSetField = make(map[PK]T)
	countDel, countErr, countLoad := 0, 0, 0
	for _, t := range ts {
		r.configChangeTimestamps[tableName] = t.GetChangedAt()

		logger := r.logger.With(zap.String("table", tableName), zap.Inline(t))

		if t.IsDeleted() {
			if !hasChangedAt {
				// This is a special case for the first synchronization or each run when nothing is stored in the
				// database yet. Unfortunately, it is not possible to add a "WHERE "deleted" = 'n'" to the query above
				// as newer deleted elements would be skipped in the first run, but being read in a subsequent run.
				logger.Debug("Skipping deleted element as no prior configuration is loaded")
				continue
			}

			countDel++
			logger.Debug("Marking entry as deleted")
			(*changeConfigSetField)[t.GetPrimaryKey()] = nil
			continue
		}

		if t, ok := any(t).(IncrementalConfigurableInitAndValidatable); ok {
			err := t.IncrementalInitAndValidate()
			if err != nil {
				countErr++
				logger.Errorw("Cannot validate entry, skipping element", zap.Error(err))
				continue
			}
		}

		countLoad++
		logger.Debug("Loaded entry")
		(*changeConfigSetField)[t.GetPrimaryKey()] = t
	}

	stmtLogger = stmtLogger.With("took", time.Since(startTime))
	if countDel > 0 || countErr > 0 || countLoad > 0 {
		stmtLogger.Debugw("Fetched incremental configuration updates",
			zap.Int("deleted_elements", countDel),
			zap.Int("faulty_elements", countErr),
			zap.Int("loaded_elements", countLoad))
	} else {
		stmtLogger.Debug("No configuration updates are available")
	}

	return nil
}

// errRemoveAndAddInstead is a special non-error which might be expected from incrementalApplyPending's updateFn to
// signal that the current element should be updated by being deleted through the deleteFn first and added again by the
// createFn hook function.
var errRemoveAndAddInstead = errors.New("re-adding by invoking the deletion function followed by the creation function")

// incrementalApplyPending merges the incremental change from RuntimeConfig.configChange into the main ConfigSet.
//
// The recently fetched incremental change can be of three different types:
//   - Newly created elements. Therefore, the createFn callback function will be called upon it, allowing both further
//     initialization and also aborting by returning an error.
//   - Changed elements. The updateFn callback function receives the current and the updated element, expecting the
//     implementation to synchronize the necessary changes into the current element. This hook is allowed to return an
//     error as well. However, it might also return the special errRemoveAndAddInstead, resulting in the old element to
//     be deleted first and then re-added, with optional calls to the other two callbacks included.
//   - Finally, deleted elements. Additional cleanup might be performed by the deleteFn.
//
// If no specific callback action is necessary, each function can be nil. A nil updateFn results in the same behavior as
// errRemoveAndAddInstead.
func incrementalApplyPending[
	BaseT any,
	PK comparable,
	T interface {
		*BaseT
		IncrementalConfigurable[PK]
	},
](
	r *RuntimeConfig,
	configSetField, changeConfigSetField *map[PK]T,
	createFn func(newElement T) error,
	updateFn func(curElement, update T) error,
	deleteFn func(delElement T) error,
) {
	startTime := time.Now()
	tableName := database.TableName(T(nil))
	countErr, countDel, countUpdate, countNew := 0, 0, 0, 0

	if *configSetField == nil {
		*configSetField = make(map[PK]T)
	}

	createAction := func(id PK, newT T) error {
		if createFn != nil {
			if err := createFn(newT); err != nil {
				countErr++
				return fmt.Errorf("creation callback error, %w", err)
			}
		}
		(*configSetField)[id] = newT
		countNew++
		return nil
	}

	deleteAction := func(id PK, oldT T) error {
		defer delete(*configSetField, id)
		countDel++
		if deleteFn != nil {
			if err := deleteFn(oldT); err != nil {
				countErr++
				return fmt.Errorf("deletion callback error, %w", err)
			}
		}
		return nil
	}

	for id, newT := range *changeConfigSetField {
		oldT, oldExists := (*configSetField)[id]

		logger := r.logger.With(
			zap.String("table", tableName),
			zap.Any("id", id))

		if newT == nil && !oldExists {
			logger.Debug("Skipping unknown element marked as deleted")
		} else if newT == nil {
			logger := logger.With(zap.Object("deleted", oldT))
			if err := deleteAction(id, oldT); err != nil {
				logger.Errorw("Deleting configuration element failed", zap.Error(err))
			} else {
				logger.Debug("Deleted configuration element")
			}
		} else if oldExists {
			logger := logger.With(zap.Object("old", oldT), zap.Object("update", newT))

			if oldT.GetChangedAt().Time().Equal(newT.GetChangedAt().Time()) {
				logger.Debugw("Skipping known element with unchanged changed_at timestamp",
					zap.Time("changed_at", oldT.GetChangedAt().Time()))
				continue
			}

			reAdd := updateFn == nil
			if updateFn != nil {
				if err := updateFn(oldT, newT); errors.Is(err, errRemoveAndAddInstead) {
					reAdd = true
				} else if err != nil {
					logger.Errorw("Updating callback failed", zap.Error(err))
					countErr++
					continue
				}
			}
			if reAdd {
				logger.Debug("Invoking update by removing and re-adding element")
				if err := deleteAction(id, oldT); err != nil {
					logger.Errorw("Deleting the old element during re-adding failed", zap.Error(err))
					continue
				}
				if err := createAction(id, newT); err != nil {
					logger.Errorw("Creating the new element during re-adding failed", zap.Error(err))
					continue
				}
			}

			countUpdate++
			logger.Debug("Updated known configuration element")
		} else {
			logger := logger.With(zap.Object("new", newT))
			if err := createAction(id, newT); err != nil {
				logger.Errorw("Creating configuration element failed", zap.Error(err))
			} else {
				logger.Debug("Created new configuration element")
			}
		}
	}

	*changeConfigSetField = nil
	appliedChanges := countErr > 0 || countDel > 0 || countUpdate > 0 || countNew > 0

	logger := r.logger.With(
		zap.String("table", tableName),
		zap.Duration("took", time.Since(startTime)))
	if appliedChanges {
		logger.Infow("Applied configuration updates",
			zap.Int("faulty_elements", countErr),
			zap.Int("deleted_elements", countDel),
			zap.Int("updated_elements", countUpdate),
			zap.Int("new_elements", countNew))
		r.configChangeAvailable = true
	} else {
		logger.Debug("No configuration updates available to be applied")
	}
}
