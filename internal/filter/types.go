package filter

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/icinga/icinga-go-library/types"
	"github.com/icinga/icinga-notifications/internal/pool"
	"github.com/icinga/icinga-notifications/internal/utils"
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
	op    CompOperator
	attrs any
	value any
}

// Eval evaluates this Condition based on its operator.
// Returns true when the filter evaluates to true false otherwise.
func (c *Condition) Eval(filterable Filterable) (bool, error) {
	if !filterable.EvalExists(c.attrs) {
		return false, nil
	}

	switch c.op {
	case Equal:
		match, err := filterable.EvalEqual(c.attrs, c.value)
		if err != nil {
			return false, err
		}

		return match, nil
	case UnEqual:
		match, err := filterable.EvalEqual(c.attrs, c.value)
		if err != nil {
			return false, err
		}

		return !match, nil
	case Like:
		match, err := filterable.EvalLike(c.attrs, c.value)
		if err != nil {
			return false, err
		}

		return match, nil
	case UnLike:
		match, err := filterable.EvalLike(c.attrs, c.value)
		if err != nil {
			return false, err
		}

		return !match, nil
	case LessThan:
		match, err := filterable.EvalLess(c.attrs, c.value)
		if err != nil {
			return false, err
		}

		return match, nil
	case LessThanEqual:
		match, err := filterable.EvalLessOrEqual(c.attrs, c.value)
		if err != nil {
			return false, err
		}

		return match, nil
	case GreaterThan:
		match, err := filterable.EvalLessOrEqual(c.attrs, c.value)
		if err != nil {
			return false, err
		}

		return !match, nil
	case GreaterThanEqual:
		match, err := filterable.EvalLess(c.attrs, c.value)
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

// Attributes returns the list of attributes this condition refers to.
func (c *Condition) Attributes() any { return c.attrs }

// Value returns the value of this Condition.
func (c *Condition) Value() any {
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

// UnmarshalJSON is a helper function to unmarshal a JSON representation of a filter into a [Filter] interface.
//
// It recursively parses the JSON data to deduce the filter type ([Chain] or [Condition]) based on the `op` field
// and constructs the appropriate filter structure.
//
// Returns nil if JSON null value is provided, and an error if the JSON is invalid or if required fields are missing.
func UnmarshalJSON(data []byte) (Filter, error) {
	if string(data) == "null" {
		return nil, nil
	}

	message := map[string]json.RawMessage{}
	if err := types.UnmarshalJSON(data, &message); err != nil {
		return nil, err
	}

	opBytes, opExists := message["op"]
	if !opExists {
		return nil, fmt.Errorf("missing required field: op")
	}

	var op string
	if err := types.UnmarshalJSON(opBytes, &op); err != nil {
		return nil, err
	}

	if isLogicalOp(op) {
		rulesBytes, exists := message["rules"]
		if !exists {
			return nil, fmt.Errorf("missing required field: rules")
		}

		var rules []json.RawMessage
		if err := json.Unmarshal(rulesBytes, &rules); err != nil {
			return nil, err
		}
		chain := &Chain{op: LogicalOp(op)}
		for _, rawRule := range rules {
			filter, err := UnmarshalJSON(rawRule)
			if err != nil {
				return nil, err
			}
			chain.rules = append(chain.rules, filter)
		}
		return chain, nil
	}

	if isCompOperator(op) {
		condition := &Condition{op: CompOperator(op)}
		var attrs []string
		if attrsBytes, exists := message["attributes"]; !exists {
			return nil, fmt.Errorf("missing required filter condition field: attributes")
		} else if err := types.UnmarshalJSON(attrsBytes, &attrs); err != nil {
			return nil, err
		}

		var value any // The JSON value might represent any type, so we can't directly unmarshal it into a string.
		if rawValue, exists := message["value"]; !exists {
			if rawRegex, exists := message["regex"]; !exists {
				return nil, fmt.Errorf("missing required filter condition field: value or regex")
			} else if err := types.UnmarshalJSON(rawRegex, &value); err != nil {
				return nil, err
			}
			switch condition.op {
			case Equal:
				condition.op = Like
			case UnEqual:
				condition.op = UnLike
			default:
				return nil, fmt.Errorf("regex field is only supported for equality operators (= and !=), but got operator %q", condition.op)
			}
		} else if err := types.UnmarshalJSON(rawValue, &value); err != nil {
			return nil, err
		}
		condition.value = value

		jpp := pool.GetJSONPathParser()
		defer pool.PutJSONPathParser(jpp)

		var errs []error
		var invalidAttrs []string
		for _, attr := range attrs {
			if _, err := jpp.Parse(utils.PrefixWithJSONPathRootSelector(attr)); err != nil {
				errs = append(errs, fmt.Errorf("invalid JSONPath expression %q: %w", attr, err))
				invalidAttrs = append(invalidAttrs, attr)
			}
		}

		if len(errs) > 0 {
			for _, path := range invalidAttrs {
				attrs = slices.DeleteFunc(attrs, func(p string) bool { return p == path })
			}
			// If all provided attrs are invalid, we shouldn't load this rule at all, so bail out with an error.
			if len(attrs) == 0 {
				return nil, errors.Join(errors.New("all provided JSONPath expressions are invalid"), errors.Join(errs...))
			}
			// Otherwise, we have already removed all invalid attrs from the condition but there are still some valid
			// attrs left, so we can still load this rule. Logging the errors of the invalid attrs would be preferred
			// here instead of dropping them silently, we don't have access to a logger at this point though.
		}
		condition.attrs = attrs

		return condition, nil
	}
	return nil, fmt.Errorf("unknown filter operator: %s", op)
}

// isLogicalOp checks if the provided operator is a valid logical operator.
func isLogicalOp(op string) bool {
	switch LogicalOp(op) {
	case All, Any, None:
		return true
	default:
		return false
	}
}

// isCompOperator checks if the provided operator is a valid comparison operator.
func isCompOperator(op string) bool {
	switch CompOperator(op) {
	case Equal, UnEqual, GreaterThan, LessThan, GreaterThanEqual, LessThanEqual:
		return true
	default:
		return false
	}
}
