package listener

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
)

// ErrFilterEval is returned when an error occurs during the evaluation of a filter.
var ErrFilterEval = stderrors.New("filter evaluation error")

// ParseQueryFilter parses the given JSON query string filter and returns a filter.Filter object that
// can be used to evaluate events against the filter criteria.
func ParseQueryFilter(qs string) (any, error) {
	var result any
	if err := json.Unmarshal([]byte(qs), &result); err != nil {
		return nil, fmt.Errorf("invalid JSON query string filter: %w", err)
	}
	return result, nil
}

// EvaluateQueryFilter evaluates the given filter against the provided filterable map.
//
// It returns true if the filter matches, false otherwise, along with any error encountered during evaluation.
// The filter syntax is based on JSON objects and arrays, where:
// - JSON objects are evaluated as AND conditions (all key-value pairs must match).
// - JSON arrays are evaluated as OR conditions (at least one element must match).
//
// The JSON object keys can be set to null to express a "NOT EXISTS" condition, meaning that the key must not exist
// in the filterable map for the filter to match. For example, given the following JSON filter:
//
//	{"key1":"value1","key2":null}
//
// It will match if "key1" exists in the filterable map with the value "value1" and "key2" is not present at all.
func EvaluateQueryFilter(filter any, filterable map[string]string) (bool, error) {
	switch fv := filter.(type) {
	case []any:
		for _, v := range fv {
			if _, ok := v.(map[string]any); !ok {
				return false, fmt.Errorf("invalid JSON filter type: %T: %w", v, ErrFilterEval)
			}
			if matched, err := EvaluateQueryFilter(v, filterable); err != nil {
				return false, err
			} else if matched {
				return true, nil
			}
		}
		return false, nil

	case map[string]any:
		var seenPositiveCond bool
		var matched *bool
		for k, v := range fv {
			// If v is not a scalar type (string, null), return an error
			switch v.(type) {
			case string, nil: // valid types
			default:
				return false, fmt.Errorf("invalid JSON filter type: %T: %w", v, ErrFilterEval)
			}

			seenPositiveCond = seenPositiveCond || v != nil
			if matched != nil && !*matched {
				// If a previous condition has already failed, and we have at least one positive condition, we can
				// short-circuit and return false, otherwise we need to exhaust all conditions till the very end.
				if seenPositiveCond {
					return false, nil
				}
			} else if vv, keyExists := filterable[k]; !keyExists && v != nil {
				matched = new(false)
			} else if keyExists && (v == nil || v != vv) {
				matched = new(false)
			} else if matched == nil {
				matched = new(true)
			}
		}
		if !seenPositiveCond {
			// Do not allow users to guess the existence of keys in the filterable map by using "NOT EXISTS"
			// conditions without any positive conditions. This is a security measure to prevent information leakage.
			return false, fmt.Errorf(
				"invalid filter: 'NOT EXISTS' condition must be combined with at least one positive condition: %w",
				ErrFilterEval)
		}
		return matched != nil && *matched, nil

	default:
		return false, fmt.Errorf("invalid JSON filter type: %T: %w", fv, ErrFilterEval)
	}
}
