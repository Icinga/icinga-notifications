package contracts

import "fmt"

type Incident interface {
	fmt.Stringer

	ID() int64
	ObjectDisplayName() string
	SeverityString() string
}
