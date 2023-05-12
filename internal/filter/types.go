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

func (a *All) ExtractConditions() []Condition {
	return extractConditions(a.rules)
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

func (a *Any) ExtractConditions() []Condition {
	return extractConditions(a.rules)
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

func (n *None) ExtractConditions() []Condition {
	return extractConditions(n.rules)
}

// Condition represents a single filter condition.
type Condition struct {
	Column string
	Value  string
}

func NewCondition(column string, value string) Condition {
	return Condition{
		Column: column,
		Value:  value,
	}
}

func (e Condition) ExtractConditions() []Condition {
	return []Condition{e}
}

type Exists struct {
	Condition
}

func NewExists(column string) *Exists {
	return &Exists{Condition{Column: column}}
}

func (e *Exists) Eval(filterable Filterable) (bool, error) {
	return filterable.EvalExists(e.Column), nil
}

type Equal struct {
	Condition
}

func (e *Equal) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalEqual(e.Column, e.Value)
	if err != nil {
		return false, err
	}

	return match, nil
}

type UnEqual struct {
	Condition
}

func (u *UnEqual) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalEqual(u.Column, u.Value)
	if err != nil {
		return false, err
	}

	return filterable.EvalExists(u.Column) && !match, nil
}

type Like struct {
	Condition
}

func (l *Like) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalLike(l.Column, l.Value)
	if err != nil {
		return false, err
	}

	return match, nil
}

type Unlike struct {
	Condition
}

func (u *Unlike) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalLike(u.Column, u.Value)
	if err != nil {
		return false, err
	}

	return filterable.EvalExists(u.Column) && !match, nil
}

type LessThan struct {
	Condition
}

func (less *LessThan) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalLess(less.Column, less.Value)
	if err != nil {
		return false, err
	}

	return match, nil
}

type LessThanOrEqual struct {
	Condition
}

func (loe *LessThanOrEqual) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalLessOrEqual(loe.Column, loe.Value)
	if err != nil {
		return false, err
	}

	return match, nil
}

type GreaterThan struct {
	Condition
}

func (g *GreaterThan) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalLessOrEqual(g.Column, g.Value)
	if err != nil {
		return false, err
	}

	return filterable.EvalExists(g.Column) && !match, nil
}

type GreaterThanOrEqual struct {
	Condition
}

func (goe *GreaterThanOrEqual) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalLess(goe.Column, goe.Value)
	if err != nil {
		return false, err
	}

	return filterable.EvalExists(goe.Column) && !match, nil
}

// extractConditions extracts filter conditions from the specified filter rules.
func extractConditions(rules []Filter) []Condition {
	var conditions []Condition
	for _, rule := range rules {
		conditions = append(conditions, rule.ExtractConditions()...)
	}

	return conditions
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
