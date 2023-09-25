package config

import (
	"errors"
	"fmt"
	"github.com/icinga/icinga-notifications/internal/channel"
	"github.com/icinga/icinga-notifications/internal/recipient"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
)

// debugVerify performs a set of config validity/consistency checks that can be used for debugging.
func (r *RuntimeConfig) debugVerify() error {
	r.RLock()
	defer r.RUnlock()

	if r.Channels == nil {
		return errors.New("RuntimeConfig.Channels is nil")
	} else {
		for id, channel := range r.Channels {
			err := r.debugVerifyChannel(id, channel)
			if err != nil {
				return fmt.Errorf("RuntimeConfig.Channels[%d] is invalid: %w", id, err)
			}
		}
	}

	if r.Contacts == nil {
		return errors.New("RuntimeConfig.Contacts is nil")
	} else {
		for id, contact := range r.Contacts {
			err := r.debugVerifyContact(id, contact)
			if err != nil {
				return fmt.Errorf("RuntimeConfig.Contacts[%d] is invalid: %w", id, err)
			}
		}
	}

	if r.ContactAddresses == nil {
		return errors.New("RuntimeConfig.ContactAddresss is nil")
	} else {
		for id, address := range r.ContactAddresses {
			err := r.debugVerifyContactAddress(id, address)
			if err != nil {
				return fmt.Errorf("RuntimeConfig.ContactAddresss[%d] is invalid: %w", id, err)
			}
		}
	}

	if r.Groups == nil {
		return errors.New("RuntimeConfig.Groups is nil")
	} else {
		for id, group := range r.Groups {
			err := r.debugVerifyGroup(id, group)
			if err != nil {
				return fmt.Errorf("RuntimeConfig.Groups[%d] is invalid: %w", id, err)
			}
		}
	}

	if r.TimePeriods == nil {
		return errors.New("RuntimeConfig.TimePeriods is nil")
	} else {
		for id, period := range r.TimePeriods {
			err := r.debugVerifyTimePeriod(id, period)
			if err != nil {
				return fmt.Errorf("RuntimeConfig.TimePeriods[%d] is invalid: %w", id, err)
			}
		}
	}

	if r.Schedules == nil {
		return errors.New("RuntimeConfig.Schedules is nil")
	} else {
		for id, schedule := range r.Schedules {
			err := r.debugVerifySchedule(id, schedule)
			if err != nil {
				return fmt.Errorf("RuntimeConfig.Schedules[%d] is invalid: %w", id, err)
			}
		}
	}

	if r.Rules == nil {
		return errors.New("RuntimeConfig.Rules is nil")
	} else {
		for id, rule := range r.Rules {
			err := r.debugVerifyRule(id, rule)
			if err != nil {
				return fmt.Errorf("RuntimeConfig.Rules[%d]: %w", id, err)
			}
		}
	}

	return nil
}

func (r *RuntimeConfig) debugVerifyChannel(id int64, channel *channel.Channel) error {
	if channel.ID != id {
		return fmt.Errorf("channel %p has id %d but is referenced as %d", channel, channel.ID, id)
	}

	if other := r.Channels[id]; other != channel {
		return fmt.Errorf("channel %p is inconsistent with RuntimeConfig.Channels[%d] = %p", channel, id, other)
	}

	return nil
}

func (r *RuntimeConfig) debugVerifyContact(id int64, contact *recipient.Contact) error {
	if contact.ID != id {
		return fmt.Errorf("contact has ID %d but is referenced as %d", contact.ID, id)
	}

	if other := r.Contacts[id]; other != contact {
		return fmt.Errorf("contact %p is inconsistent with RuntimeConfig.Contacts[%d] = %p", contact, id, other)
	}

	if r.Channels[contact.DefaultChannelID] == nil {
		return fmt.Errorf("contact %q references non-existent default channel id %d", contact, contact.DefaultChannelID)
	}

	for i, address := range contact.Addresses {
		if address == nil {
			return fmt.Errorf("Addresses[%d] is nil", i)
		}

		if address.ContactID != id {
			return fmt.Errorf("Addresses[%d] has ContactID = %d instead of %d", i, address.ContactID, id)
		}

		err := r.debugVerifyContactAddress(address.ID, address)
		if err != nil {
			return fmt.Errorf("Addresses[%d]: %w", i, err)
		}
	}

	return nil
}

func (r *RuntimeConfig) debugVerifyContactAddress(id int64, address *recipient.Address) error {
	if address.ID != id {
		return fmt.Errorf("address has ID %d but is referenced as %d", address.ID, id)
	}

	if other := r.ContactAddresses[id]; other != address {
		return fmt.Errorf("address %p is inconsistent with RuntimeConfig.ContactAddresses[%d] = %p", address, id, other)
	}

	return nil
}

func (r *RuntimeConfig) debugVerifyGroup(id int64, group *recipient.Group) error {
	if group.ID != id {
		return fmt.Errorf("group has ID %d but is referenced as %d", group.ID, id)
	}

	if other := r.Groups[id]; other != group {
		return fmt.Errorf("group %p is inconsistent with RuntimeConfig.Groups[%d] = %p", group, id, other)
	}

	for i, member := range group.Members {
		if member == nil {
			return fmt.Errorf("Members[%d] is nil", i)
		}

		err := r.debugVerifyContact(member.ID, member)
		if err != nil {
			return fmt.Errorf("Members[%d]: %w", i, err)
		}
	}

	return nil
}

func (r *RuntimeConfig) debugVerifyTimePeriod(id int64, period *timeperiod.TimePeriod) error {
	if period.ID != id {
		return fmt.Errorf("time period has ID %d but is referenced as %d", period.ID, id)
	}

	if other := r.TimePeriods[id]; other != period {
		return fmt.Errorf("time period %p is inconsistent with RuntimeConfig.TimePeriods[%d] = %p", period, id, other)
	}

	for i, entry := range period.Entries {
		if entry == nil {
			return fmt.Errorf("Entries[%d] is nil", i)
		}
	}

	return nil
}

func (r *RuntimeConfig) debugVerifySchedule(id int64, schedule *recipient.Schedule) error {
	if schedule.ID != id {
		return fmt.Errorf("schedule has ID %d but is referenced as %d", schedule.ID, id)
	}

	if other := r.Schedules[id]; other != schedule {
		return fmt.Errorf("schedule %p is inconsistent with RuntimeConfig.Schedules[%d] = %p", schedule, id, other)
	}

	for i, member := range schedule.Members {
		if member == nil {
			return fmt.Errorf("Members[%d] is nil", i)
		}

		if member.TimePeriod == nil {
			return fmt.Errorf("Members[%d].TimePeriod is nil", i)
		}

		if member.Contact == nil && member.ContactGroup == nil {
			return fmt.Errorf("Members[%d] has neither Contact nor ContactGroup set", i)
		}

		if member.Contact != nil && member.ContactGroup != nil {
			return fmt.Errorf("Members[%d] has both Contact and ContactGroup set", i)
		}

		if member.Contact != nil {
			err := r.debugVerifyContact(member.Contact.ID, member.Contact)
			if err != nil {
				return fmt.Errorf("Contact: %w", err)
			}
		}

		if member.ContactGroup != nil {
			err := r.debugVerifyGroup(member.ContactGroup.ID, member.ContactGroup)
			if err != nil {
				return fmt.Errorf("ContactGroup: %w", err)
			}
		}
	}

	return nil
}

func (r *RuntimeConfig) debugVerifyRule(id int64, rule *rule.Rule) error {
	if rule.ID != id {
		return fmt.Errorf("rule has ID %d but is referenced as %d", rule.ID, id)
	}

	if other := r.Rules[id]; other != rule {
		return fmt.Errorf("rule %p is inconsistent with RuntimeConfig.Rules[%d] = %p", rule, id, other)
	}

	if rule.TimePeriodID.Valid && rule.TimePeriod == nil {
		return fmt.Errorf("rule has a TimePeriodID but TimePeriod is nil")
	}

	if rule.TimePeriod != nil {
		err := r.debugVerifyTimePeriod(rule.TimePeriodID.Int64, rule.TimePeriod)
		if err != nil {
			return fmt.Errorf("TimePeriod: %w", err)
		}
	}

	if rule.ObjectFilterExpr.Valid && rule.ObjectFilter == nil {
		return fmt.Errorf("rule has a ObjectFilterExpr but ObjectFilter is nil")
	}

	for escalationID, escalation := range rule.Escalations {
		if escalation == nil {
			return fmt.Errorf("Escalations[%d] is nil", escalationID)
		}

		if escalation.ID != escalationID {
			return fmt.Errorf("Escalations[%d]: ecalation has ID %d but is referenced as %d",
				escalationID, escalation.ID, escalationID)
		}

		if escalation.RuleID != rule.ID {
			return fmt.Errorf("Escalations[%d] (ID=%d) has RuleID = %d while being referenced from rule %d",
				escalationID, escalation.ID, escalation.RuleID, rule.ID)
		}

		if escalation.ConditionExpr.Valid && escalation.Condition == nil {
			return fmt.Errorf("Escalations[%d] (ID=%d) has ConditionExpr but Condition is nil", escalationID, escalation.ID)
		}

		// TODO: verify fallback

		for i, escalationRecpient := range escalation.Recipients {
			if escalationRecpient == nil {
				return fmt.Errorf("Escalations[%d].Recipients[%d] is nil", escalationID, i)
			}

			if escalationRecpient.EscalationID != escalation.ID {
				return fmt.Errorf("Escalation[%d].Recipients[%d].EscalationID = %d does not match Escalations[%d].ID = %d",
					escalationID, i, escalationRecpient.EscalationID, escalationID, escalation.ID)
			}

			switch rec := escalationRecpient.Recipient.(type) {
			case *recipient.Contact:
				if rec == nil {
					return fmt.Errorf("Escalations[%d].Recipients[%d].Recipient (Contact) is nil", escalationID, i)
				}

				err := r.debugVerifyContact(escalationRecpient.ContactID.Int64, rec)
				if err != nil {
					return fmt.Errorf("Escalations[%d].Recipients[%d].Recipient (Contact): %w", escalationID, i, err)
				}

			case *recipient.Group:
				if rec == nil {
					return fmt.Errorf("Escalations[%d].Recipients[%d].Recipient (Group) is nil", escalationID, i)
				}

				err := r.debugVerifyGroup(escalationRecpient.GroupID.Int64, rec)
				if err != nil {
					return fmt.Errorf("Escalations[%d].Recipients[%d].Recipient (Group): %w", escalationID, i, err)
				}

			case *recipient.Schedule:
				if rec == nil {
					return fmt.Errorf("Escalations[%d].Recipients[%d].Recipient (Schedule) is nil", escalationID, i)
				}

				err := r.debugVerifySchedule(escalationRecpient.ScheduleID.Int64, rec)
				if err != nil {
					return fmt.Errorf("Escalations[%d].Recipients[%d].Recipient (Schedule): %w", escalationID, i, err)
				}

			default:
				return fmt.Errorf("Escalations[%d].Recipients[%d].Recipient has invalid type %T", escalationID, i, rec)
			}
		}
	}

	return nil
}
