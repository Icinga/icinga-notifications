package recipient

import (
	"fmt"
	"github.com/icinga/icinga-go-library/types"
	"go.uber.org/zap/zapcore"
	"time"
)

type Recipient interface {
	fmt.Stringer
	zapcore.ObjectMarshaler

	GetContactsAt(t time.Time) []*Contact
}

type Key struct {
	// Only one of the members is allowed to be a non-zero value.
	ContactID  types.Int `db:"contact_id"`
	GroupID    types.Int `db:"contactgroup_id"`
	ScheduleID types.Int `db:"schedule_id"`
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
//
// This allows us to use `zap.Inline(Key)` or `zap.Object("recipient", Key)` wherever fine-grained
// logging context is needed, without having to add all the individual fields ourselves each time.
// https://pkg.go.dev/go.uber.org/zap/zapcore#ObjectMarshaler
func (r Key) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	if r.ContactID.Valid {
		encoder.AddInt64("contact_id", r.ContactID.Int64)
	} else if r.GroupID.Valid {
		encoder.AddInt64("group_id", r.GroupID.Int64)
	} else if r.ScheduleID.Valid {
		encoder.AddInt64("schedule_id", r.ScheduleID.Int64)
	}

	return nil
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
		return Key{ContactID: types.MakeInt(v.ID, types.TransformZeroIntToNull)}
	case *Group:
		return Key{GroupID: types.MakeInt(v.ID, types.TransformZeroIntToNull)}
	case *Schedule:
		return Key{ScheduleID: types.MakeInt(v.ID, types.TransformZeroIntToNull)}
	default:
		panic(fmt.Sprintf("unexpected recipient type: %T", r))
	}
}
