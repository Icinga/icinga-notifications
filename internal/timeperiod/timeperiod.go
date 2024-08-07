package timeperiod

import (
	"database/sql"
	"fmt"
	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/config/baseconf"
	"github.com/pkg/errors"
	"github.com/teambition/rrule-go"
	"go.uber.org/zap/zapcore"
	"time"
)

type TimePeriod struct {
	baseconf.IncrementalPkDbEntry[int64] `db:",inline"`

	Name    string   `db:"-"`
	Entries []*Entry `db:"-"`
}

func (p *TimePeriod) IncrementalInitAndValidate() error {
	if p.Name == "" {
		p.Name = fmt.Sprintf("Time Period #%d", p.ID)
	}
	return nil
}

func (p *TimePeriod) TableName() string {
	return "timeperiod"
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (p *TimePeriod) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddInt64("id", p.ID)
	encoder.AddString("name", p.Name)
	return nil
}

// Contains returns whether a point in time t is covered by this time period, i.e. there is an entry covering it.
func (p *TimePeriod) Contains(t time.Time) bool {
	for _, e := range p.Entries {
		if e.Contains(t) {
			return true
		}
	}

	return false
}

// NextTransition returns a time strictly after the given base time when the time period may be entered or exited.
//
// It is guaranteed that for any time t with base < t < p.NextTransition(base), p.Contains(t) == p.Contains(base),
// i.e. the earliest time a change happens, is at the returned time. Note that for simplicity of the implementation,
// this specification does not require that a transition happens at the returned time.
func (p *TimePeriod) NextTransition(base time.Time) time.Time {
	transition := base.Add(24 * time.Hour)
	for _, e := range p.Entries {
		next := e.NextTransition(base)
		if next.Before(transition) && !next.IsZero() {
			// When the next transition of the previous entry is after the
			// current one's next transition, prefer the current one.
			transition = next
		}
	}

	return transition
}

type Entry struct {
	baseconf.IncrementalPkDbEntry[int64] `db:",inline"`

	TimePeriodID     int64           `db:"timeperiod_id"`
	StartTime        types.UnixMilli `db:"start_time"`
	EndTime          types.UnixMilli `db:"end_time"`
	Timezone         string          `db:"timezone"`
	RRule            sql.NullString  `db:"rrule"` // RFC5545 RRULE
	RotationMemberID sql.NullInt64   `db:"rotation_member_id"`

	initialized bool
	rrule       *rrule.RRule
}

// IncrementalInitAndValidate implements the config.IncrementalConfigurableInitAndValidatable interface.
func (e *Entry) IncrementalInitAndValidate() error {
	return e.Init()
}

// TableName implements the contracts.TableNamer interface.
func (e *Entry) TableName() string {
	return "timeperiod_entry"
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (e *Entry) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddInt64("id", e.ID)
	encoder.AddInt64("timeperiod_id", e.TimePeriodID)
	encoder.AddTime("start", e.StartTime.Time())
	encoder.AddTime("end", e.EndTime.Time())
	encoder.AddString("timezone", e.Timezone)
	if e.RRule.Valid {
		encoder.AddString("rrule", e.RRule.String)
	}
	if e.RotationMemberID.Valid {
		encoder.AddInt64("rotation_member_id", e.RotationMemberID.Int64)
	}
	return nil
}

// Init prepares the Entry for use after being read from the database.
//
// This includes loading the timezone information and parsing the recurrence rule if present.
func (e *Entry) Init() error {
	if e.initialized {
		return nil
	}

	loc, err := time.LoadLocation(e.Timezone)
	if err != nil {
		return errors.Wrapf(err, "timeperiod entry has an invalid timezone %q", e.Timezone)
	}

	// Timestamps in the database are stored with millisecond resolution while RRULE only operates on seconds.
	// Truncate to whole seconds in case there is sub-second precision.
	// Additionally, set the location so that all times in this entry are consistent with the timezone of the entry.
	e.StartTime = types.UnixMilli(e.StartTime.Time().Truncate(time.Second).In(loc))
	e.EndTime = types.UnixMilli(e.EndTime.Time().Truncate(time.Second).In(loc))

	if e.RRule.Valid {
		option, err := rrule.StrToROptionInLocation(e.RRule.String, loc)
		if err != nil {
			return err
		}

		if option.Dtstart.IsZero() {
			option.Dtstart = e.StartTime.Time()
		}

		rule, err := rrule.NewRRule(*option)
		if err != nil {
			return err
		}

		e.rrule = rule
	}

	e.initialized = true
	return nil
}

// Contains returns whether a point in time t is covered by this entry.
//
// This function may only be called after a successful call to Init().
func (e *Entry) Contains(t time.Time) bool {
	if !e.initialized {
		panic("timeperiod.Entry: called Contains() before Init()")
	}

	if t.Before(e.StartTime.Time()) {
		return false
	}

	if t.Before(e.EndTime.Time()) {
		return true
	}

	if e.rrule == nil {
		return false
	}

	lastStart := e.rrule.Before(t, true)
	lastEnd := lastStart.Add(e.EndTime.Time().Sub(e.StartTime.Time()))
	// Whether the date time is between the last recurrence start and the last recurrence end
	return (t.Equal(lastStart) || t.After(lastStart)) && t.Before(lastEnd)
}

// NextTransition returns the next recurrence start or end of this entry relative to the given time inclusively.
// This function returns also time.Time's zero value if there is no transition that starts/ends at/after the
// specified time.
//
// This function may only be called after a successful call to Init().
func (e *Entry) NextTransition(t time.Time) time.Time {
	if !e.initialized {
		panic("timeperiod.Entry: called NextTransition() before Init()")
	}

	if t.Before(e.StartTime.Time()) {
		// The passed time is before the configured event start time
		return e.StartTime.Time()
	}

	if t.Before(e.EndTime.Time()) {
		return e.EndTime.Time()
	}

	if e.rrule == nil {
		return time.Time{}
	}

	lastStart := e.rrule.Before(t, true)
	lastEnd := lastStart.Add(e.EndTime.Time().Sub(e.StartTime.Time()))
	if (t.Equal(lastStart) || t.After(lastStart)) && t.Before(lastEnd) {
		// Base time is after the last transition begin but before the last transition end
		return lastEnd
	}

	return e.rrule.After(t, false)
}
