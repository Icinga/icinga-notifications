package recipient

import (
	"fmt"
	"time"
)

type Recipient interface {
	fmt.Stringer

	GetContactsAt(t time.Time) []*Contact
}
