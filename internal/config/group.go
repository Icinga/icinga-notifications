package config

import (
	"context"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/noma/internal/recipient"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"log"
)

func (r *RuntimeConfig) fetchGroups(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error {
	var groupPtr *recipient.Group
	stmt := db.BuildSelectStmt(groupPtr, groupPtr)
	log.Println(stmt)

	var groups []*recipient.Group
	if err := tx.SelectContext(ctx, &groups, stmt); err != nil {
		log.Println(err)
		return err
	}

	groupsById := make(map[int64]*recipient.Group)
	for _, g := range groups {
		groupsById[g.ID] = g

		logger.Debugw("loaded group config",
			zap.Int64("id", g.ID),
			zap.String("name", g.Name))
	}

	type ContactgroupMember struct {
		GroupId   int64 `db:"contactgroup_id"`
		ContactId int64 `db:"contact_id"`
	}

	var memberPtr *ContactgroupMember
	stmt = db.BuildSelectStmt(memberPtr, memberPtr)
	log.Println(stmt)

	var members []*ContactgroupMember
	if err := tx.SelectContext(ctx, &members, stmt); err != nil {
		log.Println(err)
		return err
	}

	for _, m := range members {
		memberLogger := logger.With(
			zap.Int64("contact_id", m.ContactId),
			zap.Int64("contactgroup_id", m.GroupId),
		)
		if g := groupsById[m.GroupId]; g == nil {
			memberLogger.Warnw("ignoring member for unknown contactgroup_id")
		} else if c := r.pending.Contacts[m.ContactId]; c == nil {
			memberLogger.Warnw("ignoring member for unknown contact_id")
		} else {
			g.Members = append(g.Members, c)

			memberLogger.Debugw("loaded contact group member",
				zap.String("contact_name", c.FullName),
				zap.String("contactgroup_name", g.Name))
		}
	}

	r.pending.Groups = groupsById

	return nil
}
