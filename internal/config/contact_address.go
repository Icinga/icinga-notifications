package config

import (
	"context"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/noma/internal/recipient"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"
	"log"
)

func (r *RuntimeConfig) fetchContactAddresses(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error {
	var addressPtr *recipient.Address
	stmt := db.BuildSelectStmt(addressPtr, addressPtr)
	log.Println(stmt)

	var addresses []*recipient.Address
	if err := tx.SelectContext(ctx, &addresses, stmt); err != nil {
		log.Println(err)
		return err
	}

	addressesById := make(map[int64]*recipient.Address)
	for _, a := range addresses {
		addressesById[a.ID] = a
		logger.Debugw("loaded contact_address config",
			zap.Int64("id", a.ID),
			zap.Int64("contact_id", a.ContactID),
			zap.String("type", a.Type),
			zap.String("address", a.Address),
		)
	}

	if r.ContactAddresses != nil {
		// mark no longer existing contacts for deletion
		for id := range r.ContactAddresses {
			if _, ok := addressesById[id]; !ok {
				addressesById[id] = nil
			}
		}
	}

	r.pending.ContactAddresses = addressesById

	return nil
}

func (r *RuntimeConfig) applyPendingContactAddresses(logger *logging.Logger) {
	if r.ContactAddresses == nil {
		r.ContactAddresses = make(map[int64]*recipient.Address)
	}

	for id, pendingAddress := range r.pending.ContactAddresses {
		currentAddress := r.ContactAddresses[id]

		if pendingAddress == nil {
			r.removeContactAddress(logger, currentAddress)
		} else if currentAddress != nil {
			r.updateContactAddress(logger, currentAddress, pendingAddress)
		} else {
			r.addContactAddress(logger, pendingAddress)
		}
	}

	r.pending.ContactAddresses = nil
}

func (r *RuntimeConfig) addContactAddress(logger *logging.Logger, addr *recipient.Address) {
	contact := r.Contacts[addr.ContactID]
	if contact != nil {
		if i := slices.Index(contact.Addresses, addr); i < 0 {
			contact.Addresses = append(contact.Addresses, addr)

			logger.Debugw("added new address to contact",
				zap.Any("contact", contact),
				zap.Any("address", addr))
		}
	}

	r.ContactAddresses[addr.ID] = addr
}

func (r *RuntimeConfig) updateContactAddress(logger *logging.Logger, addr, pending *recipient.Address) {
	contactChanged := addr.ContactID != pending.ContactID

	if contactChanged {
		r.removeContactAddress(logger, addr)
	}

	addr.ContactID = pending.ContactID
	addr.Type = pending.Type
	addr.Address = pending.Address

	if contactChanged {
		r.addContactAddress(logger, addr)
	}

	logger.Debugw("updated contact address", zap.Any("address", addr))
}

func (r *RuntimeConfig) removeContactAddress(logger *logging.Logger, addr *recipient.Address) {
	if contact := r.Contacts[addr.ContactID]; contact != nil {
		if i := slices.Index(contact.Addresses, addr); i >= 0 {
			contact.Addresses = slices.Delete(contact.Addresses, i, i+1)

			logger.Debugw("removed address from contact",
				zap.Any("contact", contact),
				zap.Any("address", addr))
		}
	}

	delete(r.ContactAddresses, addr.ID)
}
