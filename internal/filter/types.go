package filter

import (
	"fmt"
)

// LogicalOp is a type used for grouping the logical operators of a filter string.
type LogicalOp string

const (
	// None represents a filter chain type that matches when none of its ruleset matches.
	None LogicalOp = "!"
	// All represents a filter chain type that matches when all of its ruleset matches.
	All LogicalOp = "&"
	// Any represents a filter chain type that matches when at least one of its ruleset matches.
	Any LogicalOp = "|"
)

// Chain is a filter type that wraps other filter rules and itself.
// Therefore, it implements the Filter interface to allow it to be part of its ruleset.
// It supports also adding and popping filter rules individually.
type Chain struct {
	op    LogicalOp // The filter chain operator to be used to evaluate the rules
	rules []Filter
}

func NewChain(op LogicalOp, rules ...Filter) (*Chain, error) {
	switch op {
	case None, All, Any:
		return &Chain{rules: rules, op: op}, nil
	default:
		return nil, fmt.Errorf("invalid logical operator provided: %q", op)
	}
}

// Eval evaluates the filter rule sets recursively based on their operator type.
func (c *Chain) Eval(filterable Filterable) (bool, error) {
	switch c.op {
	case None:
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
	case All:
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
	case Any:
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

func (c *Chain) ExtractConditions() []*Condition {
	var conditions []*Condition
	for _, rule := range c.rules {
		conditions = append(conditions, rule.ExtractConditions()...)
	}

	return conditions
}

// pop pops the last filter from the rules slice (if not empty) and returns it.
func (c *Chain) pop() Filter {
	var rule Filter
	if l := len(c.rules); l > 0 {
		rule, c.rules = c.rules[l-1], c.rules[:l-1]
	}

	return rule
}

// top picks and erases the first element from its rules and returns it.
func (c *Chain) top() Filter {
	var rule Filter
	if len(c.rules) > 0 {
		rule, c.rules = c.rules[0], c.rules[1:]
	}

	return rule
}

// add adds the given filter rules to the current chain.
func (c *Chain) add(rules ...Filter) {
	c.rules = append(c.rules, rules...)
}

// CompOperator is a type used for grouping the individual comparison operators of a filter string.
type CompOperator string

// List of the supported comparison operators.
const (
	Equal            CompOperator = "="
	UnEqual          CompOperator = "!="
	Like             CompOperator = "~"
	UnLike           CompOperator = "!~"
	LessThan         CompOperator = "<"
	LessThanEqual    CompOperator = "<="
	GreaterThan      CompOperator = ">"
	GreaterThanEqual CompOperator = ">="
)

// Condition represents a single filter condition.
// It provides an implementation of the Filter interface for each of the supported CompOperator.
// All it's fields are read-only and aren't supposed to change at runtime. For read access, you can
// check the available exported methods.
type Condition struct {
	op     CompOperator
	column string
	value  string
}

// NewCondition initiates a new Condition instance from the given data.
// Returns error if invalid CompOperator is provided.
func NewCondition(column string, op CompOperator, value string) (Filter, error) {
	switch op {
	case Equal, UnEqual, Like, UnLike, LessThan, LessThanEqual, GreaterThan, GreaterThanEqual:
		return &Condition{op: op, column: column, value: value}, nil
	default:
		return nil, fmt.Errorf("invalid comparison operator provided: %q", op)
	}
}

// Eval evaluates this Condition based on its operator.
// Returns true when the filter evaluates to true false otherwise.
func (c *Condition) Eval(filterable Filterable) (bool, error) {
	if !filterable.EvalExists(c.column) {
		return false, nil
	}

	switch c.op {
	case Equal:
		match, err := filterable.EvalEqual(c.column, c.value)
		if err != nil {
			return false, err
		}

		return match, nil
	case UnEqual:
		match, err := filterable.EvalEqual(c.column, c.value)
		if err != nil {
			return false, err
		}

		return !match, nil
	case Like:
		match, err := filterable.EvalLike(c.column, c.value)
		if err != nil {
			return false, err
		}

		return match, nil
	case UnLike:
		match, err := filterable.EvalLike(c.column, c.value)
		if err != nil {
			return false, err
		}

		return !match, nil
	case LessThan:
		match, err := filterable.EvalLess(c.column, c.value)
		if err != nil {
			return false, err
		}

		return match, nil
	case LessThanEqual:
		match, err := filterable.EvalLessOrEqual(c.column, c.value)
		if err != nil {
			return false, err
		}

		return match, nil
	case GreaterThan:
		match, err := filterable.EvalLessOrEqual(c.column, c.value)
		if err != nil {
			return false, err
		}

		return !match, nil
	case GreaterThanEqual:
		match, err := filterable.EvalLess(c.column, c.value)
		if err != nil {
			return false, err
		}

		return !match, nil
	default:
		return false, fmt.Errorf("invalid comparison operator provided: %q", c.op)
	}
}

func (c *Condition) ExtractConditions() []*Condition {
	return []*Condition{c}
}

// Column returns the column of this Condition.
func (c *Condition) Column() string {
	return c.column
}

// Value returns the value of this Condition.
func (c *Condition) Value() string {
	return c.value
}

type Exists struct {
	column string
}

func (e *Exists) ExtractConditions() []*Condition {
	return nil
}

func NewExists(column string) *Exists {
	return &Exists{column: column}
}

func (e *Exists) Eval(filterable Filterable) (bool, error) {
	return filterable.EvalExists(e.column), nil
}

var (
	_ Filter = (*Chain)(nil)
	_ Filter = (*Exists)(nil)
	_ Filter = (*Condition)(nil)
)
