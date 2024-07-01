package config

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"slices"
)

func (r *RuntimeConfig) applyPendingContacts() {
	incrementalApplyPending(
		r,
		&r.Contacts, &r.configChange.Contacts,
		nil,
		func(curElement, update *recipient.Contact) error {
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
				return reAddUpdateFnErr
			}

			curElement.Type = update.Type
			curElement.Address = update.Address
			return nil
		},
		func(delElement *recipient.Address) error {
			contact, ok := r.Contacts[delElement.ContactID]
			if !ok {
				return fmt.Errorf("contact address refers unknown contact %d", delElement.ContactID)
			}

			contact.Addresses = slices.DeleteFunc(contact.Addresses, func(address *recipient.Address) bool {
				return address.ID == delElement.ID
			})
			return nil
		})
}
