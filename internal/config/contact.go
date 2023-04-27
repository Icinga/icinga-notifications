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

func (r *RuntimeConfig) fetchContacts(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error {
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

	r.pending.ContactsByID = contactsByID

	return nil
}
