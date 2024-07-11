package recipient

import (
	"github.com/icinga/icinga-notifications/internal/config/baseconf"
	"go.uber.org/zap/zapcore"
	"time"
)

type Group struct {
	baseconf.IncrementalPkDbEntry[int64] `db:",inline"`

	Name    string     `db:"name"`
	Members []*Contact `db:"-"`
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

// GroupMemberKey represents the combined primary key of GroupMember.
type GroupMemberKey struct {
	GroupId   int64 `db:"contactgroup_id"`
	ContactId int64 `db:"contact_id"`
}

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (g *GroupMemberKey) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddInt64("contactgroup_id", g.GroupId)
	encoder.AddInt64("contact_id", g.ContactId)
	return nil
}

type GroupMember struct {
	GroupMemberKey              `db:",inline"`
	baseconf.IncrementalDbEntry `db:",inline"`
}

func (g *GroupMember) TableName() string {
	return "contactgroup_member"
}

// GetPrimaryKey is required by the config.IncrementalConfigurable interface.
func (g *GroupMember) GetPrimaryKey() GroupMemberKey {
	return g.GroupMemberKey
}

var _ Recipient = (*Group)(nil)
