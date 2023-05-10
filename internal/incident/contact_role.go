package incident

import (
	"database/sql/driver"
	"fmt"
)

type ContactRole int

const (
	RoleNone ContactRole = iota
	RoleRecipient
	RoleSubscriber
	RoleManager
)

var contactRoleByName = map[string]ContactRole{
	"recipient":  RoleRecipient,
	"subscriber": RoleSubscriber,
	"manager":    RoleManager,
}

var contactRoleToName = func() map[ContactRole]string {
	cr := make(map[ContactRole]string)
	for name, role := range contactRoleByName {
		cr[role] = name
	}
	return cr
}()

// Scan implements the sql.Scanner interface.
func (c *ContactRole) Scan(src any) error {
	if c == nil {
		*c = RoleNone
		return nil
	}

	var name string
	switch val := src.(type) {
	case string:
		name = val
	case []byte:
		name = string(val)
	default:
		return fmt.Errorf("unable to scan type %T into ContactRole", src)
	}

	role, ok := contactRoleByName[name]
	if !ok {
		return fmt.Errorf("unknown contact role %q", name)
	}

	*c = role

	return nil
}

// Value implements the driver.Valuer interface.
func (c ContactRole) Value() (driver.Value, error) {
	if c == RoleNone {
		return nil, nil
	}

	return c.String(), nil
}

func (c *ContactRole) String() string {
	return contactRoleToName[*c]
}
