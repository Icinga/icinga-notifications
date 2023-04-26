package filter

// All represents a filter chain type that matches when all of its Rules matches.
type All struct {
	rules []Rule
}

func (a *All) Eval(filterable Filterable) bool {
	for _, rule := range a.rules {
		if !rule.Eval(filterable) {
			return false
		}
	}

	return true
}

// Any represents a filter chain type that matches when at least one of its Rules matches.
type Any struct {
	rules []Rule
}

func (a *Any) Eval(filterable Filterable) bool {
	for _, rule := range a.rules {
		if rule.Eval(filterable) {
			return true
		}
	}

	return false
}

// None represents a filter chain type that matches when none of its Rules matches.
type None struct {
	rules []Rule
}

func (n *None) Eval(filterable Filterable) bool {
	for _, rule := range n.rules {
		if rule.Eval(filterable) {
			return false
		}
	}

	return true
}

// Condition represents a single filter condition.
type Condition struct {
	column string
	value  string
}

func NewCondition(column string, value string) *Condition {
	return &Condition{
		column: column,
		value:  value,
	}
}

type Exists Condition

func NewExists(column string) *Exists {
	return &Exists{column: column}
}

func (e *Exists) Eval(filterable Filterable) bool {
	return filterable.EvalExists(e.column)
}

type Equal Condition

func (e *Equal) Eval(filterable Filterable) bool {
	return filterable.EvalEqual(e.column, e.value)
}

type UnEqual Condition

func (e *UnEqual) Eval(filterable Filterable) bool {
	return filterable.EvalExists(e.column) && !filterable.EvalEqual(e.column, e.value)
}

type Like Condition

func (e *Like) Eval(filterable Filterable) bool {
	return filterable.EvalLike(e.column, e.value)
}

type Unlike Condition

func (e *Unlike) Eval(filterable Filterable) bool {
	return filterable.EvalExists(e.column) && !filterable.EvalLike(e.column, e.value)
}

type LessThan Condition

func (e *LessThan) Eval(filterable Filterable) bool {
	return filterable.EvalLess(e.column, e.value)
}

type LessThanOrEqual Condition

func (e *LessThanOrEqual) Eval(filterable Filterable) bool {
	return filterable.EvalLessOrEqual(e.column, e.value)
}

type GreaterThan Condition

func (e *GreaterThan) Eval(filterable Filterable) bool {
	return filterable.EvalExists(e.column) && !filterable.EvalLess(e.column, e.value)
}

type GreaterThanOrEqual Condition

func (e *GreaterThanOrEqual) Eval(filterable Filterable) bool {
	return filterable.EvalExists(e.column) && !filterable.EvalLessOrEqual(e.column, e.value)
}

var (
	_ Rule = (*All)(nil)
	_ Rule = (*Any)(nil)
	_ Rule = (*None)(nil)

	_ Rule = (*Exists)(nil)
	_ Rule = (*Equal)(nil)
	_ Rule = (*UnEqual)(nil)
	_ Rule = (*Like)(nil)
	_ Rule = (*Unlike)(nil)
	_ Rule = (*LessThan)(nil)
	_ Rule = (*LessThanOrEqual)(nil)
	_ Rule = (*GreaterThan)(nil)
	_ Rule = (*GreaterThanOrEqual)(nil)
)
