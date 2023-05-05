package recipient

import (
	"fmt"
	"github.com/icinga/icingadb/pkg/types"
	"github.com/icinga/noma/internal/utils"
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
