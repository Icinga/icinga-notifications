package notification

import (
	"database/sql/driver"
	"fmt"
)

type State int

const (
	StateNull State = iota
	StateSuppressed
	StatePending
	StateSent
	StateFailed
)

var statTypeByName = map[string]State{
	"suppressed": StateSuppressed,
	"pending":    StatePending,
	"sent":       StateSent,
	"failed":     StateFailed,
}

var stateTypeToName = func() map[State]string {
	stateTypes := make(map[State]string)
	for name, eventType := range statTypeByName {
		stateTypes[eventType] = name
	}
	return stateTypes
}()

// Scan implements the sql.Scanner interface.
// Supports SQL NULL.
func (n *State) Scan(src any) error {
	if src == nil {
		*n = StateNull
		return nil
	}

	var name string
	switch val := src.(type) {
	case string:
		name = val
	case []byte:
		name = string(val)
	default:
		return fmt.Errorf("unable to scan type %T into NotificationState", src)
	}

	historyType, ok := statTypeByName[name]
	if !ok {
		return fmt.Errorf("unknown notification state type %q", name)
	}

	*n = historyType

	return nil
}

func (n State) Value() (driver.Value, error) {
	if n == StateNull {
		return nil, nil
	}

	return n.String(), nil
}

func (n *State) String() string {
	return stateTypeToName[*n]
}
