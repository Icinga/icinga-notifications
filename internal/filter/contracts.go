package filter

// Filterable is implemented by every filterable type.
type Filterable interface {
	EvalEqual(key, value any) (bool, error)
	EvalLess(key, value any) (bool, error)
	EvalLike(key, value any) (bool, error)
	EvalLessOrEqual(key, value any) (bool, error)
	EvalExists(key any) bool
}

// Filter is implemented by every filter chains and filter conditions.
type Filter interface {
	Eval(filterable Filterable) (bool, error)
	ExtractConditions() []*Condition
}
