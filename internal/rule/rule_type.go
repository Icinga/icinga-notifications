package rule

import (
	"database/sql/driver"
	"fmt"
)

type Type int

const (
	TypeEscalation Type = iota
	TypeRouting
)

var typeByName = map[string]Type{
	"escalation": TypeEscalation,
	"routing":    TypeRouting,
}

var typeToName = func() map[Type]string {
	types := make(map[Type]string)
	for name, eventType := range typeByName {
		types[eventType] = name
	}
	return types
}()

// Scan implements the sql.Scanner interface.
func (t *Type) Scan(src any) error {
	var name string
	switch val := src.(type) {
	case string:
		name = val
	case []byte:
		name = string(val)
	default:
		return fmt.Errorf("unable to scan type %T into rule.Type", src)
	}

	ruleType, ok := typeByName[name]
	if !ok {
		return fmt.Errorf("unknown rule type %q", name)
	}

	*t = ruleType

	return nil
}

func (t Type) Value() (driver.Value, error) {
	return t.String(), nil
}

func (t *Type) String() string {
	return typeToName[*t]
}
