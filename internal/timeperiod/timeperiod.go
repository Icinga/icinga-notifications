package timeperiod

import "time"

type TimePeriod struct {
	Name    string
	Entries []*Entry
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
	// TODO: implement this in a more useful way once RRULE evaluation is implemented
	return base.Add(time.Second).Truncate(time.Second)
}

type Entry struct {
	Start, End time.Time

	// temporarily used for prototyping purposes
	RepeatEvery time.Duration

	// for future use
	TimeZone       string // or *time.Location
	RecurrenceRule string // RFC5545 RRULE
}

// Contains returns whether a point in time t is covered by this entry.
func (e *Entry) Contains(t time.Time) bool {
	if t.Before(e.Start) {
		return false
	}

	if t.Before(e.End) {
		return true
	}

	// TODO: replace with RRULE evaluation
	// trivial repetition pattern for testing
	if e.RepeatEvery > 0 && t.Sub(e.Start)%e.RepeatEvery < e.End.Sub(e.Start) {
		return true
	}

	return false
}
