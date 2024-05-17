//go:generate go tool stringer -type=State -linecomment -output=state_string.go

package notification

import (
	"database/sql/driver"
	"fmt"
)

// State represents the state of a notification.
type State uint8

const (
	StateNull State = iota // null

	StateSuppressed // suppressed
	StatePending    // pending
	StateSent       // sent
	StateFailed     // failed

	_stateMax // internal
)

// Scan implements the sql.Scanner interface.
// Supports SQL NULL.
func (n *State) Scan(src any) error {
	if src == nil {
		*n = StateNull
		return nil
	}

	var statStr string
	switch val := src.(type) {
	case string:
		statStr = val
	case []byte:
		statStr = string(val)
	default:
		return fmt.Errorf("unable to scan type %T into NotificationState", src)
	}

	state, err := ParseState(statStr)
	if err != nil {
		return fmt.Errorf("unknown notification state type %q", statStr)
	}

	*n = state
	return nil
}

// Value implements the [driver.Valuer] interface.
// Supports SQL NULL.
func (n State) Value() (driver.Value, error) {
	if n != StateNull {
		return n.String(), nil
	}
	return nil, nil
}

// ParseState parses the given string and returns the corresponding State value.
//
// Returns an error if the string does not match any known State value.
func ParseState(str string) (State, error) {
	for s := range _stateMax {
		if s.String() == str {
			return s, nil
		}
	}
	return StateNull, fmt.Errorf("unknown notification state type %q", str)
}
