package recipient

import (
	"go.uber.org/zap/zapcore"
	"time"
)

type Group struct {
	ID        int64      `db:"id"`
	Name      string     `db:"name"`
	Members   []*Contact `db:"-"`
	MemberIDs []int64    `db:"-"`
}

func (g *Group) GetContactsAt(t time.Time) []*Contact {
	return g.Members
}

func (g *Group) TableName() string {
	return "contactgroup"
}

func (g *Group) String() string {
	return g.Name
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (g *Group) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	// Use contact_id as key so that the type is explicit if logged as the Recipient interface.
	encoder.AddInt64("group_id", g.ID)
	encoder.AddString("name", g.Name)
	return nil
}

var _ Recipient = (*Group)(nil)
