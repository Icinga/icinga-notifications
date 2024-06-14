package object

import (
	"context"
	"fmt"
	"github.com/icinga/icinga-go-library/com"
	"github.com/icinga/icinga-go-library/database"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"sync"
)

var (
	cache   = make(map[string]*Object)
	cacheMu sync.Mutex
)

// RestoreMutedObjects restores all muted objects and their extra / ID tags from the database.
// Note, this function only retrieves muted objects without non-recovered incident.
//
// Returns an error on any database failure and panics when trying to cache an object that's already in the cache store.
func RestoreMutedObjects(ctx context.Context, db *database.DB) error {
	objects := make(chan *Object)
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		defer close(objects)

		clause := `WHERE mute_reason IS NOT NULL AND NOT EXISTS((SELECT 1 FROM incident WHERE object_id = object.id AND recovered_at IS NULL))`
		query := fmt.Sprintf("%s %s", db.BuildSelectStmt(new(Object), new(Object)), clause)
		err := utils.ExecAndApply[Object](ctx, db, query, nil, func(o *Object) {
			o.db = db
			o.Tags = map[string]string{}
			o.ExtraTags = map[string]string{}

			select {
			case objects <- o:
			case <-ctx.Done(): // The ctx error is going to be handled by utils.ExecAndApply.
			}
		})

		return errors.Wrap(err, "cannot restore muted objects without an active incident")
	})

	g.Go(func() error {
		bulks := com.Bulk(ctx, objects, db.Options.MaxPlaceholdersPerStatement, com.NeverSplit[*Object])

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case bulk, ok := <-bulks:
				if !ok {
					return nil
				}

				g.Go(func() error {
					var ids []types.Binary
					objectsMap := map[string]*Object{}
					for _, obj := range bulk {
						objectsMap[obj.ID.String()] = obj
						ids = append(ids, obj.ID)
					}

					// Restore object ID tags matching the given object ids
					err := utils.ForEachRow[IdTagRow](ctx, db, "object_id", ids, func(ir *IdTagRow) {
						objectsMap[ir.ObjectId.String()].Tags[ir.Tag] = ir.Value
					})
					if err != nil {
						return errors.Wrap(err, "cannot restore objects ID tags")
					}

					// Restore object extra tags matching the given object ids
					err = utils.ForEachRow[ExtraTagRow](ctx, db, "object_id", ids, func(et *ExtraTagRow) {
						objectsMap[et.ObjectId.String()].ExtraTags[et.Tag] = et.Value
					})
					if err != nil {
						return errors.Wrap(err, "cannot restore objects extra tags")
					}

					addObjectsToCache(objectsMap)

					return nil
				})
			}
		}
	})

	return g.Wait()
}

// addObjectsToCache adds the objects from the given map to the global object cache store.
// Panics when trying to cache an object that's already in the cache store.
func addObjectsToCache(objects map[string]*Object) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	for _, o := range objects {
		if obj, ok := cache[o.ID.String()]; ok {
			panic(fmt.Sprintf("Object %q is already in the cache", obj.DisplayName()))
		}

		cache[o.ID.String()] = o
	}
}
