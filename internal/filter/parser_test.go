package filter

import (
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestParser(t *testing.T) {
	t.Parallel()

	t.Run("MissingLogicalOperatorsAfterConditionsAreDetected", func(t *testing.T) {
		_, err := Parse("(a=b|c=d)e=f")

		expected := "invalid filter '(a=b|c=d)e=f', unexpected e at pos 10: Expected logical operator"
		assert.EqualError(t, err, expected, "Errors should be the same")
	})

	t.Run("MissingLogicalOperatorsAfterOperatorsAreDetected", func(t *testing.T) {
		_, err := Parse("(a=b|c=d|)e=f")

		expected := "invalid filter '(a=b|c=d|)e=f', unexpected e at pos 11: Expected logical operator"
		assert.EqualError(t, err, expected, "Errors should be the same")
	})

	t.Run("ParserIdentifiesInvalidExpression", func(t *testing.T) {
		_, err := Parse("col=(")
		assert.EqualError(t, err, "invalid filter 'col=(', unexpected ( at pos 5", "Errors should be the same")

		_, err = Parse("(((x=a)&y=b")
		assert.EqualError(t, err, "invalid filter '(((x=a)&y=b', missing 2 closing ')' at pos 11", "Errors should be the same")

		_, err = Parse("(x=a)&y=b)")
		assert.EqualError(t, err, "invalid filter '(x=a)&y=b)', unexpected ) at pos 10", "Errors should be the same")

		_, err = Parse("!(&")
		assert.EqualError(t, err, "invalid filter '!(&', unexpected & at pos 3", "Errors should be the same")

		_, err = Parse("!(!&")
		assert.EqualError(t, err, "invalid filter '!(!&', unexpected & at pos 4: operator level 1", "Errors should be the same")

		_, err = Parse("!(|test")
		assert.EqualError(t, err, "invalid filter '!(|test', unexpected | at pos 3", "Errors should be the same")

		_, err = Parse("foo&bar=(te(st)")
		assert.EqualError(t, err, "invalid filter 'foo&bar=(te(st)', unexpected ( at pos 9", "Errors should be the same")

		_, err = Parse("foo&bar=te(st)")
		assert.EqualError(t, err, "invalid filter 'foo&bar=te(st)', unexpected ( at pos 11", "Errors should be the same")

		_, err = Parse("foo&bar=test)")
		assert.EqualError(t, err, "invalid filter 'foo&bar=test)', unexpected ) at pos 13", "Errors should be the same")

		_, err = Parse("!()|&()&)")
		assert.EqualError(t, err, "invalid filter '!()|&()&)', unexpected closing ')' at pos 9", "Errors should be the same")
	})
}

func TestFilter(t *testing.T) {
	t.Parallel()

	t.Run("ParserIdentifiesAllKindOfFilters", func(t *testing.T) {
		rule, err := Parse("foo=bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &Equal{}, rule)

		rule, err = Parse("foo!=bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &UnEqual{}, rule)

		rule, err = Parse("foo=bar*")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &Like{}, rule)

		rule, err = Parse("foo!=bar*")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &Unlike{}, rule)

		rule, err = Parse("foo<bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &LessThan{}, rule)

		rule, err = Parse("foo<=bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &LessThanOrEqual{}, rule)

		rule, err = Parse("foo>bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &GreaterThan{}, rule)

		rule, err = Parse("foo>=bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &GreaterThanOrEqual{}, rule)

		rule, err = Parse("foo=bar&bar=foo")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &Chain{}, rule)

		rule, err = Parse("foo=bar|bar=foo")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &Chain{}, rule)

		rule, err = Parse("!(foo=bar|bar=foo)")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.IsType(t, &Chain{}, rule)

		rule, err = Parse("!foo")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.Equal(t, &Chain{op: NONE, rules: []Filter{NewExists("foo")}}, rule)

		rule, err = Parse("foo")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.Equal(t, &Exists{column: "foo"}, rule)

		rule, err = Parse("!(foo=bar|bar=foo)&(foo=bar|bar=foo)")
		assert.Nil(t, err, "There should be no errors but got: %s", err)

		expected := &Chain{op: ALL, rules: []Filter{
			&Chain{op: NONE, rules: []Filter{
				&Equal{column: "foo", value: "bar"},
				&Equal{column: "bar", value: "foo"},
			}},
			&Chain{op: ANY, rules: []Filter{
				&Equal{column: "foo", value: "bar"},
				&Equal{column: "bar", value: "foo"},
			}},
		}}
		assert.Equal(t, expected, rule)
	})

	t.Run("ParserIdentifiesSingleCondition", func(t *testing.T) {
		rule, err := Parse("foo=bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)

		expected := &Equal{column: "foo", value: "bar"}
		assert.Equal(t, expected, rule, "Parser doesn't parse single condition correctly")
	})

	t.Run("UrlEncodedFilterExpression", func(t *testing.T) {
		rule, err := Parse("col%3Cumn<val%3Cue")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.Equal(t, &LessThan{column: "col<umn", value: "val<ue"}, rule)

		rule, err = Parse("col%7Cumn=val%7Cue")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.Equal(t, &Equal{column: "col|umn", value: "val|ue"}, rule)

		rule, err = Parse("col%26umn<=val%26ue")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.Equal(t, &LessThanOrEqual{column: "col&umn", value: "val&ue"}, rule)

		rule, err = Parse("col%28umn>val%28ue")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.Equal(t, &GreaterThan{column: "col(umn", value: "val(ue"}, rule)

		rule, err = Parse("col%29umn>=val%29ue")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.Equal(t, &GreaterThanOrEqual{column: "col)umn", value: "val)ue"}, rule)
	})
}

func FuzzParser(f *testing.F) {
	f.Add("(a=b|c=d)e=f")
	f.Add("(a=b|c=d|)e=f")
	f.Add("col=(")
	f.Add("(((x=a)&y=b")
	f.Add("(x=a)&y=b)")
	f.Add("!(&")
	f.Add("!(|test")
	f.Add("foo&bar=(te(st)")
	f.Add("foo&bar=te(st)")
	f.Add("foo&bar=test)")
	f.Add("foo=bar")
	f.Add("foo!=bar")
	f.Add("foo=bar*")
	f.Add("foo!=bar*")
	f.Add("foo<bar")
	f.Add("foo<=bar")
	f.Add("foo>bar")
	f.Add("foo>=bar")
	f.Add("foo=bar&bar=foo")
	f.Add("foo=bar|bar=foo")
	f.Add("!(foo=bar|bar=foo)")
	f.Add("!foo")
	f.Add("foo")
	f.Add("!(foo=bar|bar=foo)&(foo=bar|bar=foo)")
	f.Add("foo=bar")
	f.Add("col%3Cumn<val%3Cue")
	f.Add("col%7Cumn=val%7Cue")
	f.Add("col%26umn<=val%26ue")
	f.Add("col%28umn>val%28ue")
	f.Add("col%29umn>val%29ue")

	f.Fuzz(func(t *testing.T, expr string) {
		_, err := Parse(expr)

		if strings.Count(expr, "(") != strings.Count(expr, ")") {
			assert.Error(t, err)
		}
	})
}
