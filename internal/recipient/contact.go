package recipient

import (
	"time"
)

type Contact struct {
	FullName  string
	Username  string
	Addresses []*Address
}

func (c *Contact) GetContactsAt(t time.Time) []*Contact {
	return []*Contact{c}
}

var _ Recipient = (*Contact)(nil)

type Address struct {
	Type    string
	Address string
}
