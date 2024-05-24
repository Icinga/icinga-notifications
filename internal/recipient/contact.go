package recipient

import (
	"database/sql"
	"time"
)

type Contact struct {
	ID               int64          `db:"id"`
	FullName         string         `db:"full_name"`
	Username         sql.NullString `db:"username"`
	DefaultChannelID int64          `db:"default_channel_id"`
	Addresses        []*Address     `db:"-"`
}

func (c *Contact) String() string {
	return c.FullName
}

func (c *Contact) GetContactsAt(t time.Time) []*Contact {
	return []*Contact{c}
}

var _ Recipient = (*Contact)(nil)

type Address struct {
	ID        int64  `db:"id"`
	ContactID int64  `db:"contact_id"`
	Type      string `db:"type"`
	Address   string `db:"address"`
}

func (a *Address) TableName() string {
	return "contact_address"
}
