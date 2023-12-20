package common

import (
	"database/sql/driver"
	"fmt"
)

type NotificationState int

const (
	NotificationStateNull NotificationState = iota
	NotificationStatePending
	NotificationStateSent
	NotificationStateFailed
)

var notificationStatTypeByName = map[string]NotificationState{
	"pending": NotificationStatePending,
	"sent":    NotificationStateSent,
	"failed":  NotificationStateFailed,
}

var notificationStateTypeToName = func() map[NotificationState]string {
	stateTypes := make(map[NotificationState]string)
	for name, eventType := range notificationStatTypeByName {
		stateTypes[eventType] = name
	}
	return stateTypes
}()

// Scan implements the sql.Scanner interface.
// Supports SQL NULL.
func (n *NotificationState) Scan(src any) error {
	if src == nil {
		*n = NotificationStateNull
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

	historyType, ok := notificationStatTypeByName[name]
	if !ok {
		return fmt.Errorf("unknown notification state type %q", name)
	}

	*n = historyType

	return nil
}

func (n NotificationState) Value() (driver.Value, error) {
	if n == NotificationStateNull {
		return nil, nil
	}

	return n.String(), nil
}

func (n *NotificationState) String() string {
	return notificationStateTypeToName[*n]
}
