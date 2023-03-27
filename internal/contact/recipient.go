package contact

import "time"

type Recipient interface {
	GetContactsAt(t time.Time) []*Contact
}
