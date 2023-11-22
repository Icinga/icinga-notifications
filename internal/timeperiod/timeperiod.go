package timeperiod

import (
	"github.com/teambition/rrule-go"
	"time"
)

type TimePeriod struct {
	ID      int64 `db:"id"`
	Name    string
	Entries []*Entry
}

func (p *TimePeriod) TableName() string {
	return "timeperiod"
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

// Entry represents a single TimePeriod segment and defines its own start and end time.
// You should initialize it by calling its Init method before using any of its other methods.
type Entry struct {
	Start, End time.Time

	// for future use
	TimeZone string // or *time.Location

	RecurrenceRule string // RFC5545 RRULE
	rrule          *rrule.RRule
}

// Init initializes the rrule instance from the configured rrule string
// Returns error when it fails to initiate an RRule instance from the configured rrule string (if any).
func (e *Entry) Init() error {
	if e.rrule != nil || e.RecurrenceRule == "" {
		return nil
	}

	option, err := rrule.StrToROptionInLocation(e.RecurrenceRule, e.Start.Location())
	if err != nil {
		return err
	}

	if option.Dtstart.IsZero() {
		option.Dtstart = e.Start
	}

	rule, err := rrule.NewRRule(*option)
	if err != nil {
		return err
	}

	e.rrule = rule

	return nil
}

// Contains returns whether a point in time t is covered by this entry.
// It panics when it's used before initializing the rrule instance while a rrule string is configured.
func (e *Entry) Contains(t time.Time) bool {
	e.assertInitialized()

	if t.Before(e.Start) {
		return false
	}

	if t.Before(e.End) {
		return true
	}

	if e.rrule == nil {
		return false
	}

	lastStart := e.rrule.Before(t, true)
	lastEnd := lastStart.Add(e.End.Sub(e.Start))
	// Whether the date time is between the last recurrence start and the last recurrence end
	return (t.Equal(lastStart) || t.After(lastStart)) && t.Before(lastEnd)
}

// NextTransition returns the next recurrence start or end of this entry relative to the given time inclusively.
// This function returns also time.Time's zero value if there is no transition that starts/ends at/after the
// specified time. It panics when it's used before initializing the rrule instance while a rrule string is configured.
func (e *Entry) NextTransition(t time.Time) time.Time {
	e.assertInitialized()

	if t.Before(e.Start) {
		// The passed time is before the configured event start time
		return e.Start
	}

	if t.Before(e.End) {
		return e.End
	}

	if e.rrule == nil {
		return time.Time{}
	}

	lastStart := e.rrule.Before(t, true)
	lastEnd := lastStart.Add(e.End.Sub(e.Start))
	if (t.Equal(lastStart) || t.After(lastStart)) && t.Before(lastEnd) {
		// Base time is after the last transition begin but before the last transition end
		return lastEnd
	}

	return e.rrule.After(t, false)
}

// assertInitialized checks whether the current time period entry is properly initialized.
// It panics if it's not, otherwise, it's a no-op.
func (e *Entry) assertInitialized() {
	if e.rrule == nil && e.RecurrenceRule != "" {
		panic("time period entry not initialized")
	}
}
