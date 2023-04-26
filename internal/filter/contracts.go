package filter

// Filterable is implemented by every filterable noma types.
type Filterable interface {
	EvalEqual(key string, value string) bool
	EvalLess(key string, value string) bool
	EvalLike(key string, value string) bool
	EvalLessOrEqual(key string, value string) bool
	EvalExists(key string) bool
}

// Filter is implemented by every filter chains and filter conditions.
type Filter interface {
	Eval(filterable Filterable) bool
}
