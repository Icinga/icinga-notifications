package recipient

import (
	"database/sql"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config/baseconf"
	"github.com/icinga/icinga-notifications/internal/timeperiod"
	"go.uber.org/zap/zapcore"
	"time"
)

type Schedule struct {
	baseconf.IncrementalPkDbEntry[int64] `db:",inline"`

	Name string `db:"name"`

	Rotations        []*Rotation `db:"-"`
	rotationResolver rotationResolver
}

// RefreshRotations updates the internally cached rotations.
//
// This must be called after the Rotations member was updated for the change to become active.
func (s *Schedule) RefreshRotations() {
	s.rotationResolver.update(s.Rotations)
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (s *Schedule) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	// Use schedule_id as key so that the type is explicit if logged as the Recipient interface.
	encoder.AddInt64("schedule_id", s.ID)
	encoder.AddString("name", s.Name)
	return nil
}

type Rotation struct {
	baseconf.IncrementalPkDbEntry[int64] `db:",inline"`

	ScheduleID    int64             `db:"schedule_id"`
	ActualHandoff types.UnixMilli   `db:"actual_handoff"`
	Priority      sql.NullInt32     `db:"priority"`
	Name          string            `db:"name"`
	Members       []*RotationMember `db:"-"`
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (r *Rotation) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddInt64("id", r.ID)
	encoder.AddInt64("schedule_id", r.ScheduleID)
	if r.Priority.Valid {
		encoder.AddInt32("priority", r.Priority.Int32)
	}
	encoder.AddString("name", r.Name)
	return nil
}

type RotationMember struct {
	baseconf.IncrementalPkDbEntry[int64] `db:",inline"`

	RotationID        int64                       `db:"rotation_id"`
	Contact           *Contact                    `db:"-"`
	ContactID         sql.NullInt64               `db:"contact_id"`
	ContactGroup      *Group                      `db:"-"`
	ContactGroupID    sql.NullInt64               `db:"contactgroup_id"`
	TimePeriodEntries map[int64]*timeperiod.Entry `db:"-"`
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (r *RotationMember) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddInt64("id", r.ID)
	encoder.AddInt64("rotation_id", r.RotationID)
	if r.ContactID.Valid {
		encoder.AddInt64("contact_id", r.ContactID.Int64)
	}
	if r.ContactGroupID.Valid {
		encoder.AddInt64("contact_group_id", r.ContactGroupID.Int64)
	}
	return nil
}

// GetContactsAt returns the contacts that are active in the schedule at the given time.
func (s *Schedule) GetContactsAt(t time.Time) []*Contact {
	return s.rotationResolver.getContactsAt(t)
}

func (s *Schedule) String() string {
	return s.Name
}

var _ Recipient = (*Schedule)(nil)
