package event

import (
	"encoding/json"
	"fmt"
)

type Severity int

const (
	SeverityOK Severity = 1 + iota
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

func (s *Severity) String() string {
	return severityToName[*s]
}
