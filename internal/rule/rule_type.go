//go:generate go tool stringer -linecomment -type Type -output rule_type_string.go

package rule

import (
	"database/sql/driver"
	"fmt"
)

// Type represents the type of event rule.
type Type uint8

const (
	TypeEscalation Type = iota // escalation
	TypeRouting                // routing

	_typeMax // internal
)

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

	ruleType, err := ParseRuleType(name)
	if err != nil {
		return err
	}

	*t = ruleType
	return nil
}

func (t Type) Value() (driver.Value, error) {
	return t.String(), nil
}

// ParseRuleType parses a string into a Type.
func ParseRuleType(str string) (Type, error) {
	for t := range _typeMax {
		if t.String() == str {
			return t, nil
		}
	}
	return 0, fmt.Errorf("unknown rule type %q", str)
}
