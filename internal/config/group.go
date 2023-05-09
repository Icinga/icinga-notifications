package config

import (
	"context"
	"github.com/icinga/noma/internal/recipient"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

func (r *RuntimeConfig) fetchGroups(ctx context.Context, tx *sqlx.Tx) error {
	var groupPtr *recipient.Group
	stmt := r.db.BuildSelectStmt(groupPtr, groupPtr)
	r.logger.Debugf("Executing query %q", stmt)

	var groups []*recipient.Group
	if err := tx.SelectContext(ctx, &groups, stmt); err != nil {
		r.logger.Errorln(err)
		return err
	}

	groupsById := make(map[int64]*recipient.Group)
	for _, g := range groups {
		groupsById[g.ID] = g

		r.logger.Debugw("loaded group config",
			zap.Int64("id", g.ID),
			zap.String("name", g.Name))
	}

	type ContactgroupMember struct {
		GroupId   int64 `db:"contactgroup_id"`
		ContactId int64 `db:"contact_id"`
	}

	var memberPtr *ContactgroupMember
	stmt = r.db.BuildSelectStmt(memberPtr, memberPtr)
	r.logger.Debugf("Executing query %q", stmt)

	var members []*ContactgroupMember
	if err := tx.SelectContext(ctx, &members, stmt); err != nil {
		r.logger.Errorln(err)
		return err
	}

	for _, m := range members {
		memberLogger := r.logger.With(
			zap.Int64("contact_id", m.ContactId),
			zap.Int64("contactgroup_id", m.GroupId),
		)

		if g := groupsById[m.GroupId]; g == nil {
			memberLogger.Warnw("ignoring member for unknown contactgroup_id")
		} else {
			g.MemberIDs = append(g.MemberIDs, m.ContactId)

			memberLogger.Debugw("loaded contact group member",
				zap.String("contactgroup_name", g.Name))
		}
	}

	if r.Groups != nil {
		// mark no longer existing groups for deletion
		for id := range r.Groups {
			if _, ok := groupsById[id]; !ok {
				groupsById[id] = nil
			}
		}
	}

	r.pending.Groups = groupsById

	return nil
}

func (r *RuntimeConfig) applyPendingGroups() {
	if r.Groups == nil {
		r.Groups = make(map[int64]*recipient.Group)
	}

	for id, pendingGroup := range r.pending.Groups {
		if pendingGroup == nil {
			delete(r.Groups, id)
		} else {
			pendingGroup.Members = make([]*recipient.Contact, 0, len(pendingGroup.MemberIDs))
			for _, contactID := range pendingGroup.MemberIDs {
				if c := r.Contacts[contactID]; c != nil {
					pendingGroup.Members = append(pendingGroup.Members, c)
				}
			}

			if currentGroup := r.Groups[id]; currentGroup != nil {
				*currentGroup = *pendingGroup
			} else {
				r.Groups[id] = pendingGroup
			}
		}
	}

	r.pending.Groups = nil
}
