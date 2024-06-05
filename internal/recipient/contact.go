package recipient

import (
	"database/sql"
	"go.uber.org/zap/zapcore"
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

// MarshalLogObject implements the zapcore.ObjectMarshaler interface.
func (c *Contact) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	// Use contact_id as key so that the type is explicit if logged as the Recipient interface.
	encoder.AddInt64("contact_id", c.ID)
	encoder.AddString("name", c.FullName)
	return nil
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
