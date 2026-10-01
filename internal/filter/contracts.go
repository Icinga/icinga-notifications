package filter

import "fmt"

// Filterable is implemented by every filterable type.
type Filterable interface {
	EvalEqual(key []string, value any) (bool, error)
	EvalLess(key []string, value any) (bool, error)
	EvalLike(key []string, value any) (bool, error)
	EvalLessOrEqual(key []string, value any) (bool, error)
	EvalExists(key []string) bool
}

// Filter is implemented by every filter chains and filter conditions.
type Filter interface {
	fmt.Stringer

	Eval(filterable Filterable) (bool, error)
	ExtractConditions() []*Condition
}
