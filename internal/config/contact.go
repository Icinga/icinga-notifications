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

	if r.ContactsByID != nil {
		// mark no longer existing contacts for deletion
		for id := range r.ContactsByID {
			if _, ok := contactsByID[id]; !ok {
				contactsByID[id] = nil
			}
		}
	}

	r.pending.ContactsByID = contactsByID

	return nil
}

func (r *RuntimeConfig) applyPendingContacts(logger *logging.Logger) {
	if r.ContactsByID == nil {
		r.ContactsByID = make(map[int64]*recipient.Contact)
	}

	for id, pendingContact := range r.pending.ContactsByID {
		if pendingContact == nil {
			delete(r.ContactsByID, id)
		} else if currentContact := r.ContactsByID[id]; currentContact != nil {
			currentContact.FullName = pendingContact.FullName
			currentContact.Username = pendingContact.Username
		} else {
			r.ContactsByID[id] = pendingContact
		}
	}

	r.pending.ContactsByID = nil
}
