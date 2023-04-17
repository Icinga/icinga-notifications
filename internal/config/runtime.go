package config

import (
	"context"
	"database/sql"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/noma/internal/channel"
	"github.com/icinga/noma/internal/recipient"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"log"
)

// RuntimeConfig stores the runtime representation of the configuration present in the database.
type RuntimeConfig struct {
	Channels      []*channel.Channel
	ChannelByType map[string]*channel.Channel
	Contacts      []*recipient.Contact
	ContactsByID  map[int64]*recipient.Contact
	Groups        []*recipient.Group
	GroupsByID    map[int64]*recipient.Group
}

func (r *RuntimeConfig) UpdateFromDatabase(ctx context.Context, db *icingadb.DB, logger *logging.Logger) error {
	tx, err := db.BeginTxx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return err
	}
	// The transaction is only used for reading, never has to be committed.
	defer func() { _ = tx.Rollback() }()

	updateFuncs := []func(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error{
		r.UpdateChannelsFromDatabase,
		r.UpdateContactsFromDatabase,
		r.UpdateGroupsFromDatabase,
	}
	for _, f := range updateFuncs {
		if err := f(ctx, db, tx, logger); err != nil {
			return err
		}
	}

	return nil
}

func (r *RuntimeConfig) UpdateChannelsFromDatabase(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error {
	var channelPtr *channel.Channel
	stmt := db.BuildSelectStmt(channelPtr, channelPtr)
	log.Println(stmt)

	var channels []*channel.Channel
	if err := tx.SelectContext(ctx, &channels, stmt); err != nil {
		log.Println(err)
		return err
	}

	channelsByType := make(map[string]*channel.Channel)
	for _, c := range channels {
		channelLogger := logger.With(
			zap.Int64("id", c.ID),
			zap.String("name", c.Name),
			zap.String("type", c.Type),
		)
		if channelsByType[c.Type] != nil {
			channelLogger.Warnw("ignoring duplicate config for channel type")
		} else {
			channelsByType[c.Type] = c

			channelLogger.Debugw("loaded channel config")
		}
	}

	r.Channels = channels
	r.ChannelByType = channelsByType

	return nil
}

func (r *RuntimeConfig) UpdateContactsFromDatabase(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error {
	var contactPtr *recipient.Contact
	stmt := db.BuildSelectStmt(contactPtr, contactPtr)
	log.Println(stmt)

	var contacts []*recipient.Contact
	if err := tx.SelectContext(ctx, &contacts, stmt); err != nil {
		log.Println(err)
		return err
	}

	contactsByID := make(map[int64]*recipient.Contact)
	for _, c := range contacts {
		contactsByID[c.ID] = c

		logger.Debugw("loaded contact config",
			zap.Int64("id", c.ID),
			zap.String("name", c.FullName))
	}

	var addressPtr *recipient.Address
	stmt = db.BuildSelectStmt(addressPtr, addressPtr)
	log.Println(stmt)

	var addresses []*recipient.Address
	if err := tx.SelectContext(ctx, &addresses, stmt); err != nil {
		log.Println(err)
		return err
	}

	for _, a := range addresses {
		addressLogger := logger.With(
			zap.Int64("contact_id", a.ContactID),
			zap.String("type", a.Type),
			zap.String("address", a.Address),
		)
		if c := contactsByID[a.ContactID]; c != nil {
			c.Addresses = append(c.Addresses, a)

			addressLogger.Debugw("loaded contact address", zap.String("contact_name", c.FullName))
		} else {
			addressLogger.Warnw("ignoring address for unknown contact_id")
		}
	}

	r.Contacts = contacts
	r.ContactsByID = contactsByID

	return nil
}

func (r *RuntimeConfig) UpdateGroupsFromDatabase(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error {
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
		} else if c := r.ContactsByID[m.ContactId]; c == nil {
			memberLogger.Warnw("ignoring member for unknown contact_id")
		} else {
			g.Members = append(g.Members, c)

			memberLogger.Debugw("loaded contact group member",
				zap.String("contact_name", c.FullName),
				zap.String("contactgroup_name", g.Name))
		}
	}

	r.Groups = groups
	r.GroupsByID = groupsById

	return nil
}
