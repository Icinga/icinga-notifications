package filter

// Filterable is implemented by every filterable noma types.
type Filterable interface {
	EvalEqual(key string, value string) (bool, error)
	EvalLess(key string, value string) (bool, error)
	EvalLike(key string, value string) (bool, error)
	EvalLessOrEqual(key string, value string) (bool, error)
	EvalExists(key string) bool
}

// Filter is implemented by every filter chains and filter conditions.
type Filter interface {
	Eval(filterable Filterable) (bool, error)
}
