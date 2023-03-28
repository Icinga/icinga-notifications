package recipient

import "time"

type Group struct {
	Name    string
	Members []*Contact
}

func (g *Group) GetContactsAt(t time.Time) []*Contact {
	return g.Members
}

var _ Recipient = (*Group)(nil)
