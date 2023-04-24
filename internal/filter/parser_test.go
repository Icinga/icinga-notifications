package filter

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestParser(t *testing.T) {
	t.Parallel()

	t.Run("MissingLogicalOperatorsAfterConditionsAreDetected", func(t *testing.T) {
		parser := NewParser()
		_, err := parser.Parse("(a=b|c=d)e=f")

		expected := "invalid filter '(a=b|c=d)e=f', unexpected e at pos 10: Expected logical operator"
		assert.EqualError(t, err, expected, "Errors should be the same")
	})

	t.Run("MissingLogicalOperatorsAfterOperatorsAreDetected", func(t *testing.T) {
		parser := NewParser()
		_, err := parser.Parse("(a=b|c=d|)e=f")

		expected := "invalid filter '(a=b|c=d|)e=f', unexpected e at pos 11: Expected logical operator"
		assert.EqualError(t, err, expected, "Errors should be the same")
	})
}

func TestFilter(t *testing.T) {
	t.Parallel()

	t.Run("ParserIdentifiesAllKindOfFilters", func(t *testing.T) {
		parser := NewParser()
		rule, err := parser.Parse("foo=bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &Equal{}, rule)

		rule, err = parser.Parse("foo!=bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &UnEqual{}, rule)

		rule, err = parser.Parse("foo=bar*")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &Like{}, rule)

		rule, err = parser.Parse("foo!=bar*")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &Unlike{}, rule)

		rule, err = parser.Parse("foo<bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &LessThan{}, rule)

		rule, err = parser.Parse("foo<=bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &LessThanOrEqual{}, rule)

		rule, err = parser.Parse("foo>bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &GreaterThan{}, rule)

		rule, err = parser.Parse("foo>=bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &GreaterThanOrEqual{}, rule)

		rule, err = parser.Parse("foo=bar&bar=foo")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &All{}, rule)

		rule, err = parser.Parse("foo=bar|bar=foo")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &Any{}, rule)

		rule, err = parser.Parse("!(foo=bar|bar=foo)")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &None{}, rule)

		rule, err = parser.Parse("!foo")
		assert.Nil(t, err, "There should be no errors but got: %s", err)

		assert.Equal(t, &None{rules: []Rule{NewExists("foo")}}, rule)

		rule, err = parser.Parse("foo")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.Equal(t, NewExists("foo"), rule)

		rule, err = parser.Parse("!(foo=bar|bar=foo)&(foo=bar|bar=foo)")
		assert.Nil(t, err, "There should be no errors but got: %s", err)

		expected := &All{rules: []Rule{
			&None{rules: []Rule{
				&Equal{Condition: NewCondition("foo", "bar")},
				&Equal{Condition: NewCondition("bar", "foo")},
			}},
			&Any{rules: []Rule{
				&Equal{Condition: NewCondition("foo", "bar")},
				&Equal{Condition: NewCondition("bar", "foo")},
			}},
		}}
		assert.Equal(t, expected, rule)
	})

	t.Run("ParserIdentifiesSingleCondition", func(t *testing.T) {
		parser := NewParser()
		rule, err := parser.Parse("foo=bar")

		assert.Nil(t, err, "There should be no errors but got: %s", err)

		expected := &Equal{Condition: NewCondition("foo", "bar")}
		assert.Equal(t, expected, rule, "Parser doesn't parse single condition correctly")
	})
}
