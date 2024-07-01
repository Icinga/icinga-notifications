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

	// GetID returns the primary key value.
	GetID() PK

	// GetChangedAt returns the changed_at value.
	GetChangedAt() types.UnixMilli

	// IsDeleted returns if this entry was marked as deleted.
	IsDeleted() bool
}

// IncrementalConfigurableValidatable defines a validation method for new and updated elements within incrementalFetch.
type IncrementalConfigurableValidatable interface {
	// IncrementalValidate validates and optionally return an error, resulting in aborting this entry.
	IncrementalValidate() error
}

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

	stmt := r.db.BuildSelectStmt(typePtr, typePtr)
	stmtArgs := []any{}
	if hasChangedAt {
		stmtLogger = stmtLogger.With(zap.Time("changed_at", changedAt.Time()))
		stmt += ` WHERE "changed_at" > ?`
		stmtArgs = []any{changedAt}
	} else {
		stmt += ` WHERE "deleted" = 'n'`
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
			countDel++
			logger.Debug("Marking entry as deleted")
			(*changeConfigSetField)[t.GetID()] = nil
			continue
		}

		if t, ok := any(t).(IncrementalConfigurableValidatable); ok {
			err := t.IncrementalValidate()
			if err != nil {
				countErr++
				logger.Errorw("Cannot validate entry, skipping element", zap.Error(err))
				continue
			}
		}

		countLoad++
		logger.Debug("Loaded entry")
		(*changeConfigSetField)[t.GetID()] = t
	}

	stmtLogger = stmtLogger.With("took", time.Since(startTime))
	if countDel+countErr+countLoad > 0 {
		stmtLogger.Debugw("Fetched incremental configuration updates",
			zap.Int("deleted-elements", countDel),
			zap.Int("faulty-elements", countErr),
			zap.Int("loaded-elements", countLoad))
	} else {
		stmtLogger.Debug("No configuration updates are available")
	}

	return nil
}

// reAddUpdateFnErr is a special non-error which might be expected from incrementalApplyPending's updateFn to signal
// that the current element should be updated by being deleted through the deleteFn first and added again by the
// createFn hook function. This implies that both createFn and deleteFn are set and not a nil pointer.
var reAddUpdateFnErr = errors.New("re-adding by invoking the deletion function followed by the creation function")

// incrementalApplyPending merges the incremental change from RuntimeConfig.configChange into the main ConfigSet.
//
// The recently fetched incremental change can be of three different types:
//   - Newly created elements. Therefore, the createFn callback function will be called upon it, allowing both further
//     initialization and also aborting by returning an error.
//   - Changed elements. The updateFn callback function receives the current and the updated element, expecting the
//     implementation to synchronize the necessary changes into the current element. This hook is allowed to return an
//     error as well. However, it might also return the special reAddUpdateFnErr, resulting in the old element to be
//     deleted first and then re-added, with optional calls to the other two callbacks included.
//   - Finally, deleted elements. Additional cleanup might be performed by the deleteFn.
//
// If no specific callback action is necessary, each function can be nil. A nil updateFn results in the same behavior as
// reAddUpdateFnErr.
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
	tableName := database.TableName(new(T))
	countErr, countDelSkip, countDel, countUpdate, countNew := 0, 0, 0, 0, 0

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
			countDelSkip++
			logger.Warn("Skipping unknown marked as deleted configuration element")
		} else if newT == nil {
			logger := logger.With(zap.Object("deleted", oldT))
			if err := deleteAction(id, newT); err != nil {
				logger.Errorw("Deleting configuration element failed", zap.Error(err))
			} else {
				logger.Info("Deleted configuration element")
			}
		} else if oldExists {
			logger := logger.With(zap.Object("old", oldT), zap.Object("update", newT))
			reAdd := updateFn == nil
			if updateFn != nil {
				if err := updateFn(oldT, newT); errors.Is(err, reAddUpdateFnErr) {
					reAdd = true
				} else if err != nil {
					logger.Errorw("Updating callback failed", zap.Error(err))
					countErr++
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
			logger.Info("Updated known configuration element")
		} else {
			logger := logger.With(zap.Object("new", newT))
			if err := createAction(id, newT); err != nil {
				logger.Errorw("Creating configuration element failed", zap.Error(err))
			} else {
				logger.Info("Created new configuration element")
			}
		}
	}

	logger := r.logger.With(
		zap.String("table", tableName),
		zap.Duration("took", time.Since(startTime)))
	if countErr+countDelSkip+countDel+countUpdate+countNew > 0 {
		logger.Infow("Applied configuration updates",
			zap.Int("faulty-elements", countErr),
			zap.Int("deleted-unknown-elements", countDelSkip),
			zap.Int("deleted-elements", countDel),
			zap.Int("updated-elements", countUpdate),
			zap.Int("new-elements", countNew))
	} else {
		logger.Debug("No configuration updates available to be applied")
	}

	*changeConfigSetField = nil
}
