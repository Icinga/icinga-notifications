package recipient

import "time"

type Group struct {
	ID        int64  `db:"id"`
	Name      string `db:"name"`
	Members   []*Contact
	MemberIDs []int64
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

var _ Recipient = (*Group)(nil)
