package filter

import (
	"fmt"
)

// LogicalOp is a type used for grouping the logical operators of a filter string.
type LogicalOp string

const (
	// NONE represents a filter chain type that matches when none of its ruleset matches.
	NONE LogicalOp = "!"
	// ALL represents a filter chain type that matches when all of its ruleset matches.
	ALL LogicalOp = "&"
	// ANY represents a filter chain type that matches when at least one of its ruleset matches.
	ANY LogicalOp = "|"
)

// Chain is a filter type that wraps other filter rules and itself.
// Therefore, it implements the Filter interface to allow it to be part of its ruleset.
// It supports also adding and popping filter rules individually.
type Chain struct {
	op    LogicalOp // The filter chain operator to be used to evaluate the rules
	rules []Filter
}

// Eval evaluates the filter rule sets recursively based on their operator type.
func (c *Chain) Eval(filterable Filterable) (bool, error) {
	switch c.op {
	case NONE:
		for _, rule := range c.rules {
			matched, err := rule.Eval(filterable)
			if err != nil {
				return false, err
			}

			if matched {
				return false, nil
			}
		}

		return true, nil
	case ALL:
		for _, rule := range c.rules {
			matched, err := rule.Eval(filterable)
			if err != nil {
				return false, err
			}

			if !matched {
				return false, nil
			}
		}

		return true, nil
	case ANY:
		for _, rule := range c.rules {
			matched, err := rule.Eval(filterable)
			if err != nil {
				return false, err
			}

			if matched {
				return true, nil
			}
		}

		return false, nil
	default:
		return false, fmt.Errorf("invalid logical operator provided: %q", c.op)
	}
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
	match, err := filterable.EvalLessOrEqual(g.column, g.value)
	if err != nil {
		return false, err
	}

	return filterable.EvalExists(g.column) && !match, nil
}

type GreaterThanOrEqual Condition

func (goe *GreaterThanOrEqual) Eval(filterable Filterable) (bool, error) {
	match, err := filterable.EvalLess(goe.column, goe.value)
	if err != nil {
		return false, err
	}

	return filterable.EvalExists(goe.column) && !match, nil
}

var (
	_ Filter = (*Chain)(nil)
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
