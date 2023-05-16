package recipient

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/utils"
	"github.com/icinga/icingadb/pkg/types"
	"time"
)

type Recipient interface {
	fmt.Stringer

	GetContactsAt(t time.Time) []*Contact
}

type Key struct {
	// Only one of the members is allowed to be a non-zero value.
	ContactID  types.Int `db:"contact_id"`
	GroupID    types.Int `db:"contactgroup_id"`
	ScheduleID types.Int `db:"schedule_id"`
}

// MarshalText implements the encoding.TextMarshaler interface to allow JSON marshaling of map[Key]T.
func (r Key) MarshalText() (text []byte, err error) {
	if r.ContactID.Valid {
		return []byte(fmt.Sprintf("contact_id=%d", r.ContactID.Int64)), nil
	} else if r.GroupID.Valid {
		return []byte(fmt.Sprintf("group_id=%d", r.GroupID.Int64)), nil
	} else if r.ScheduleID.Valid {
		return []byte(fmt.Sprintf("schedule_id=%d", r.ScheduleID.Int64)), nil
	} else {
		return nil, nil
	}
}

func ToKey(r Recipient) Key {
	switch v := r.(type) {
	case *Contact:
		return Key{ContactID: utils.ToDBInt(v.ID)}
	case *Group:
		return Key{GroupID: utils.ToDBInt(v.ID)}
	case *Schedule:
		return Key{ScheduleID: utils.ToDBInt(v.ID)}
	default:
		panic(fmt.Sprintf("unexpected recipient type: %T", r))
	}
}
