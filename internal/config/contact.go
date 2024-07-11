package config

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"slices"
)

// applyPendingContacts synchronizes changed contacts
func (r *RuntimeConfig) applyPendingContacts() {
	incrementalApplyPending(
		r,
		&r.Contacts, &r.configChange.Contacts,
		nil,
		func(curElement, update *recipient.Contact) error {
			curElement.ChangedAt = update.ChangedAt
			curElement.FullName = update.FullName
			curElement.Username = update.Username
			curElement.DefaultChannelID = update.DefaultChannelID
			return nil
		},
		nil)

	incrementalApplyPending(
		r,
		&r.ContactAddresses, &r.configChange.ContactAddresses,
		func(newElement *recipient.Address) error {
			contact, ok := r.Contacts[newElement.ContactID]
			if !ok {
				return fmt.Errorf("contact address refers unknown contact %d", newElement.ContactID)
			}

			contact.Addresses = append(contact.Addresses, newElement)
			return nil
		},
		func(curElement, update *recipient.Address) error {
			if curElement.ContactID != update.ContactID {
				return errRemoveAndAddInstead
			}

			curElement.ChangedAt = update.ChangedAt
			curElement.Type = update.Type
			curElement.Address = update.Address
			return nil
		},
		func(delElement *recipient.Address) error {
			contact, ok := r.Contacts[delElement.ContactID]
			if !ok {
				return nil
			}

			contact.Addresses = slices.DeleteFunc(contact.Addresses, func(address *recipient.Address) bool {
				return address.ID == delElement.ID
			})
			return nil
		})
}
