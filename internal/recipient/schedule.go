package recipient

import (
	"database/sql"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"time"
)

type Schedule struct {
	ID         int64  `db:"id"`
	Name       string `db:"name"`
	Members    []*Member
	MemberRows []*ScheduleMemberRow
}

type Member struct {
	TimePeriod   *timeperiod.TimePeriod
	Contact      *Contact
	ContactGroup *Group
}

type ScheduleMemberRow struct {
	ScheduleID   int64         `db:"schedule_id"`
	TimePeriodID int64         `db:"timeperiod_id"`
	ContactID    sql.NullInt64 `db:"contact_id"`
	GroupID      sql.NullInt64 `db:"contactgroup_id"`
}

func (s *ScheduleMemberRow) TableName() string {
	return "schedule_member"
}

// GetContactsAt returns the contacts that are active in the schedule at the given time.
func (s *Schedule) GetContactsAt(t time.Time) []*Contact {
	var contacts []*Contact

	for _, m := range s.Members {
		if m.TimePeriod.Contains(t) {
			if m.Contact != nil {
				contacts = append(contacts, m.Contact)
			}

			if m.ContactGroup != nil {
				contacts = append(contacts, m.ContactGroup.Members...)
			}
		}
	}

	return contacts
}

func (s *Schedule) String() string {
	return s.Name
}

var _ Recipient = (*Schedule)(nil)
