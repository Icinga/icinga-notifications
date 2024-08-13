package filter

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const unknown string = "unknown"

var errEval = errors.New("evaluation error")

func TestFilter(t *testing.T) {
	t.Parallel()

	filterable := &filterableType{
		key:   "domain",
		value: "example.com",
	}

	t.Run("InvalidOperator", func(t *testing.T) {
		chain, err := NewChain(LogicalOp('0'), nil)
		assert.Nil(t, chain)
		assert.EqualError(t, err, "invalid logical operator provided: '0'")

		condition, err := NewCondition("column", "unknown", "value")
		assert.Nil(t, condition)
		assert.EqualError(t, err, "invalid comparison operator provided: \"unknown\"")
	})

	t.Run("EvaluationError", func(t *testing.T) {
		t.Parallel()

		testInvalidData := []struct {
			Expression string
		}{
			{"domain=" + unknown},
			{"domain!=" + unknown},
			{"domain<" + unknown},
			{"domain<=" + unknown},
			{"domain>" + unknown},
			{"domain>=" + unknown},
			{"domain~" + unknown},
			{"domain!~" + unknown},
			{"!(domain!=" + unknown + ")"},
			{"domain=" + unknown + "&domain<=test.example.com"},
			{"domain<=" + unknown + "|domain<=test.example.com"},
		}

		for _, td := range testInvalidData {
			f, err := Parse(td.Expression)
			assert.NoError(t, err)

			matched, err := f.Eval(filterable)
			assert.EqualError(t, err, errEval.Error())
			assert.Equal(t, matched, false, "unexpected filter result for %q", td.Expression)
		}
	})

	t.Run("EvaluateFilter", func(t *testing.T) {
		t.Parallel()

		testdata := []struct {
			Expression string
			Expected   bool
		}{
			{"domain=example.com", true},
			{"domain!=example.com", false},
			{"domain=test.example.com", false},
			{"name!=example.com", false},
			{"domain", true},
			{"name", false},
			{"display_name", false},
			{"!name", true},
			{"domain~example*", true},
			{"domain!~example*", false},
			{"domain~example*&!domain", false},
			{"domain>a", true},
			{"domain<a", false},
			{"domain>z", false},
			{"domain<z", true},
			{"domain>=example&domain<=test.example.com", true},
			{"domain<=example|domain<=test.example.com", true},
			{"domain<=example|domain>=test.example.com", false},
		}

		for _, td := range testdata {
			f, err := Parse(td.Expression)
			if assert.NoError(t, err, "parsing %q should not return an error", td.Expression) {
				matched, err := f.Eval(filterable)
				assert.NoError(t, err)
				assert.Equal(t, td.Expected, matched, "unexpected filter result for %q", td.Expression)
			}
		}
	})
}

type filterableType struct {
	key   string
	value string
}

func (f *filterableType) EvalEqual(_ string, value string) (bool, error) {
	if value == unknown {
		return false, errEval
	}

	return strings.EqualFold(f.value, value), nil
}

func (f *filterableType) EvalLess(_ string, value string) (bool, error) {
	if value == unknown {
		return false, errEval
	}

	return f.value < value, nil
}

func (f *filterableType) EvalLike(_ string, value string) (bool, error) {
	if value == unknown {
		return false, errEval
	}

	regex := regexp.MustCompile("^example.*$")
	return regex.MatchString(f.value), nil
}

func (f *filterableType) EvalLessOrEqual(_ string, value string) (bool, error) {
	if value == unknown {
		return false, errEval
	}

	return f.value <= value, nil
}

func (f *filterableType) EvalExists(key string) bool {
	return f.key == key
}
