package event

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

type Severity int

const (
	SeverityNone Severity = iota
	SeverityOK
	SeverityDebug
	SeverityInfo
	SeverityNotice
	SeverityWarning
	SeverityErr
	SeverityCrit
	SeverityAlert
	SeverityEmerg
)

var severityByName = map[string]Severity{
	"ok":      SeverityOK,
	"debug":   SeverityDebug,
	"info":    SeverityInfo,
	"notice":  SeverityNotice,
	"warning": SeverityWarning,
	"err":     SeverityErr,
	"crit":    SeverityCrit,
	"alert":   SeverityAlert,
	"emerg":   SeverityEmerg,
}

var severityToName = func() map[Severity]string {
	m := make(map[Severity]string)
	for name, severity := range severityByName {
		m[severity] = name
	}
	return m
}()

func (s *Severity) MarshalJSON() ([]byte, error) {
	if name, ok := severityToName[*s]; ok {
		return json.Marshal(name)
	} else {
		return json.Marshal(nil)
	}
}

func (s *Severity) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = SeverityNone
		return nil
	}

	var name string
	err := json.Unmarshal(data, &name)
	if err != nil {
		return err
	}

	severity, ok := severityByName[name]
	if !ok {
		return fmt.Errorf("unknown severity %q", name)
	}

	*s = severity
	return nil
}

// Scan implements the sql.Scanner interface.
// Supports SQL NULL.
func (s *Severity) Scan(src any) error {
	if src == nil {
		*s = SeverityNone
		return nil
	}

	var name string
	switch val := src.(type) {
	case string:
		name = val
	case []byte:
		name = string(val)
	default:
		return fmt.Errorf("unable to scan type %T into Severity", src)
	}

	severity, ok := severityByName[name]
	if !ok {
		return fmt.Errorf("unknown severity %q", string(name))
	}

	*s = severity

	return nil
}

// Value implements the driver.Valuer interface.
// Supports SQL NULL.
func (s Severity) Value() (driver.Value, error) {
	if s == SeverityNone {
		return nil, nil
	}

	return s.String(), nil
}

func (s *Severity) String() string {
	return severityToName[*s]
}
