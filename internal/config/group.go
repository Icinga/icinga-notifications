package config

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"slices"
)

// applyPendingGroups synchronizes changed groups.
func (r *RuntimeConfig) applyPendingGroups() {
	incrementalApplyPending(
		r,
		&r.Groups, &r.configChange.Groups,
		nil,
		func(curElement, update *recipient.Group) error {
			curElement.ChangedAt = update.ChangedAt
			curElement.Name = update.Name
			return nil
		},
		nil)

	incrementalApplyPending(
		r,
		&r.groupMembers, &r.configChange.groupMembers,
		func(newElement *recipient.GroupMember) error {
			group, ok := r.Groups[newElement.GroupId]
			if !ok {
				return fmt.Errorf("group member refers unknown group %d", newElement.GroupId)
			}

			contact, ok := r.Contacts[newElement.ContactId]
			if !ok {
				return fmt.Errorf("group member refers unknown contact %d", newElement.ContactId)
			}

			group.Members = append(group.Members, contact)
			return nil
		},
		func(element, update *recipient.GroupMember) error {
			// The only fields in GroupMember are - next to ChangedAt and Deleted - GroupId and ContactId. As those two
			// fields are the primary key, changing one would result in another primary key, which is technically not an
			// update anymore.
			return fmt.Errorf("group membership entry cannot change")
		},
		func(delElement *recipient.GroupMember) error {
			group, ok := r.Groups[delElement.GroupId]
			if !ok {
				return nil
			}

			group.Members = slices.DeleteFunc(group.Members, func(contact *recipient.Contact) bool {
				return contact.ID == delElement.ContactId
			})
			return nil
		})
}
