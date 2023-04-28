package filter

// All represents a filter chain type that matches when all of its Rules matches.
type All struct {
	rules []Filter
}

func (a *All) Eval(filterable Filterable) (bool, error) {
	for _, rule := range a.rules {
		matched, err := rule.Eval(filterable)
		if err != nil {
			return false, err
		}

		if !matched {
			return false, nil
		}
	}

	return true, nil
}

// Any represents a filter chain type that matches when at least one of its Rules matches.
type Any struct {
	rules []Filter
}

func (a *Any) Eval(filterable Filterable) (bool, error) {
	for _, rule := range a.rules {
		matched, err := rule.Eval(filterable)
		if err != nil {
			return false, err
		}

		if matched {
			return true, nil
		}
	}

	return false, nil
}

// None represents a filter chain type that matches when none of its Rules matches.
type None struct {
	rules []Filter
}

func (n *None) Eval(filterable Filterable) (bool, error) {
	for _, rule := range n.rules {
		matched, err := rule.Eval(filterable)
		if err != nil {
			return false, err
		}

		if matched {
			return false, nil
		}
	}

	return true, nil
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

func (e *Exists) Eval(filterable Filterable) (bool, error) {
	return filterable.EvalExists(e.column), nil
}

type Equal Condition

func (e *Equal) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalEqual(e.column, e.value)
	if err != nil {
		return false, err
	}

	return match, nil
}

type UnEqual Condition

func (u *UnEqual) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalEqual(u.column, u.value)
	if err != nil {
		return false, err
	}

	return filterable.EvalExists(u.column) && !match, nil
}

type Like Condition

func (l *Like) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalLike(l.column, l.value)
	if err != nil {
		return false, err
	}

	return match, nil
}

type Unlike Condition

func (u *Unlike) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalLike(u.column, u.value)
	if err != nil {
		return false, err
	}

	return filterable.EvalExists(u.column) && !match, nil
}

type LessThan Condition

func (less *LessThan) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalLess(less.column, less.value)
	if err != nil {
		return false, err
	}

	return match, nil
}

type LessThanOrEqual Condition

func (loe *LessThanOrEqual) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalLessOrEqual(loe.column, loe.value)
	if err != nil {
		return false, err
	}

	return match, nil
}

type GreaterThan Condition

func (g *GreaterThan) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalLess(g.column, g.value)
	if err != nil {
		return false, err
	}

	return filterable.EvalExists(g.column) && !match, nil
}

type GreaterThanOrEqual Condition

func (goe *GreaterThanOrEqual) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalLessOrEqual(goe.column, goe.value)
	if err != nil {
		return false, err
	}

	return filterable.EvalExists(goe.column) && !match, nil
}

var (
	_ Filter = (*All)(nil)
	_ Filter = (*Any)(nil)
	_ Filter = (*None)(nil)

	_ Filter = (*Exists)(nil)
	_ Filter = (*Equal)(nil)
	_ Filter = (*UnEqual)(nil)
	_ Filter = (*Like)(nil)
	_ Filter = (*Unlike)(nil)
	_ Filter = (*LessThan)(nil)
	_ Filter = (*LessThanOrEqual)(nil)
	_ Filter = (*GreaterThan)(nil)
	_ Filter = (*GreaterThanOrEqual)(nil)
)
