package filter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParser(t *testing.T) {
	t.Parallel()

	t.Run("ParseInvalidFilters", func(t *testing.T) {
		t.Parallel()

		_, err := Parse("(a=b|c=d)e=f")
		assert.EqualError(t, err, "1:10 (9): syntax error: unexpected 'column or value', expecting '|' or '&'")

		_, err = Parse("(a=b|c=d|)e=f")
		assert.EqualError(t, err, "1:10 (9): syntax error: unexpected ')', expecting 'column or value' or '!' or '('")

		_, err = Parse("col=(")
		assert.EqualError(t, err, "1:5 (4): syntax error: unexpected '(', expecting 'column or value'")

		_, err = Parse("(((x=a)&y=b")
		assert.EqualError(t, err, "1:12 (11): syntax error: unexpected EOF, expecting '|' or '&' or ')'")

		_, err = Parse("(x=a)&y=b)")
		assert.EqualError(t, err, "1:10 (9): syntax error: unexpected ')', expecting '|' or '&'")

		_, err = Parse("!(&")
		assert.EqualError(t, err, "1:3 (2): syntax error: unexpected '&', expecting 'column or value' or '!' or '('")

		_, err = Parse("foo&bar=(te(st)")
		assert.EqualError(t, err, "1:9 (8): syntax error: unexpected '(', expecting 'column or value'")

		_, err = Parse("foo&bar=te(st)")
		assert.EqualError(t, err, "1:11 (10): syntax error: unexpected '(', expecting '|' or '&'")

		_, err = Parse("foo&bar=test)")
		assert.EqualError(t, err, "1:13 (12): syntax error: unexpected ')', expecting '|' or '&'")

		_, err = Parse("!()|&()&)")
		assert.EqualError(t, err, "1:3 (2): syntax error: unexpected ')', expecting 'column or value' or '!' or '('")

		_, err = Parse("=foo")
		assert.EqualError(t, err, "1:1 (0): syntax error: unexpected '=', expecting 'column or value' or '!' or '('")

		_, err = Parse("foo>")
		assert.EqualError(t, err, "1:5 (4): syntax error: unexpected EOF, expecting 'column or value'")

		_, err = Parse("foo==")
		assert.EqualError(t, err, "1:5 (4): syntax error: unexpected '=', expecting 'column or value'")

		_, err = Parse("=>foo")
		assert.EqualError(t, err, "1:1 (0): syntax error: unexpected '=', expecting 'column or value' or '!' or '('")

		_, err = Parse("&foo")
		assert.EqualError(t, err, "1:1 (0): syntax error: unexpected '&', expecting 'column or value' or '!' or '('")

		_, err = Parse("&&foo")
		assert.EqualError(t, err, "1:1 (0): syntax error: unexpected '&', expecting 'column or value' or '!' or '('")

		_, err = Parse("(&foo=bar)")
		assert.EqualError(t, err, "1:2 (1): syntax error: unexpected '&', expecting 'column or value' or '!' or '('")

		_, err = Parse("(foo=bar|)")
		assert.EqualError(t, err, "1:10 (9): syntax error: unexpected ')', expecting 'column or value' or '!' or '('")

		_, err = Parse("((((((")
		assert.EqualError(t, err, "1:7 (6): syntax error: unexpected EOF, expecting 'column or value' or '!' or '('")

		_, err = Parse("foo&bar&col=val!=val")
		assert.EqualError(t, err, "1:17 (16): syntax error: unexpected '!=', expecting '|' or '&'")

		_, err = Parse("col%7umn")
		assert.EqualError(t, err, "1:1 (0): invalid URL escape \"%7u\"")

		_, err = Parse("((0&((((((((((((((((((((((0=0)")
		assert.EqualError(t, err, "1:31 (30): syntax error: unexpected EOF, expecting '|' or '&' or ')'")

		// IPL web filter parser accepts such invalid strings, but our Lexer doesn't.
		_, err = Parse("foo\x00")
		assert.EqualError(t, err, "1:1 (0): invalid character NUL")

		_, err = Parse("\xff")
		assert.EqualError(t, err, "0:0 (0): invalid UTF-8 encoding")
	})

	t.Run("ParseAllKindOfSimpleFilters", func(t *testing.T) {
		t.Parallel()

		rule, err := Parse("foo=bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected := &Condition{op: Equal, column: "foo", value: "bar"}
		assert.Equal(t, expected, rule)

		rule, err = Parse("foo!=bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Condition{op: UnEqual, column: "foo", value: "bar"}
		assert.Equal(t, expected, rule)

		rule, err = Parse("foo~bar*")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Condition{op: Like, column: "foo", value: "bar*"}
		assert.Equal(t, expected, rule)

		rule, err = Parse("foo!~bar*")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Condition{op: UnLike, column: "foo", value: "bar*"}
		assert.Equal(t, expected, rule)

		rule, err = Parse("foo<bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Condition{op: LessThan, column: "foo", value: "bar"}
		assert.Equal(t, expected, rule)

		rule, err = Parse("foo<=bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Condition{op: LessThanEqual, column: "foo", value: "bar"}
		assert.Equal(t, expected, rule)

		rule, err = Parse("foo>bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Condition{op: GreaterThan, column: "foo", value: "bar"}
		assert.Equal(t, expected, rule)

		rule, err = Parse("foo>=bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Condition{op: GreaterThanEqual, column: "foo", value: "bar"}
		assert.Equal(t, expected, rule)

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
		assert.Equal(t, &Chain{op: None, rules: []Filter{NewExists("foo")}}, rule)

		rule, err = Parse("foo")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.Equal(t, &Exists{column: "foo"}, rule)
	})

	t.Run("ParseChain", func(t *testing.T) {
		t.Parallel()

		var expected Filter
		rule, err := Parse("!foo=bar")
		expected = &Chain{op: None, rules: []Filter{&Condition{op: Equal, column: "foo", value: "bar"}}}
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.Equal(t, expected, rule)

		rule, err = Parse("foo=bar&bar=foo")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Chain{op: All, rules: []Filter{
			&Condition{op: Equal, column: "foo", value: "bar"},
			&Condition{op: Equal, column: "bar", value: "foo"},
		}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("foo=bar&bar=foo|col=val")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Chain{op: Any, rules: []Filter{
			&Chain{op: All, rules: []Filter{
				&Condition{op: Equal, column: "foo", value: "bar"},
				&Condition{op: Equal, column: "bar", value: "foo"},
			}},
			&Condition{op: Equal, column: "col", value: "val"},
		}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("foo=bar|bar=foo")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Chain{op: Any, rules: []Filter{
			&Condition{op: Equal, column: "foo", value: "bar"},
			&Condition{op: Equal, column: "bar", value: "foo"},
		}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("(foo=bar)")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Condition{op: Equal, column: "foo", value: "bar"}
		assert.Equal(t, expected, rule)

		rule, err = Parse("(!foo=bar)")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Chain{op: None, rules: []Filter{&Condition{op: Equal, column: "foo", value: "bar"}}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("!(foo=bar)")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Chain{op: None, rules: []Filter{&Condition{op: Equal, column: "foo", value: "bar"}}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("!(!foo=bar)")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Chain{op: None, rules: []Filter{
			&Chain{op: None, rules: []Filter{
				&Condition{op: Equal, column: "foo", value: "bar"},
			}},
		}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("!(foo=bar|bar=foo)")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Chain{op: None, rules: []Filter{
			&Chain{op: Any, rules: []Filter{
				&Condition{op: Equal, column: "foo", value: "bar"},
				&Condition{op: Equal, column: "bar", value: "foo"},
			}},
		}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("((!foo=bar)&bar!=foo)")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Chain{op: All, rules: []Filter{
			&Chain{op: None, rules: []Filter{&Condition{op: Equal, column: "foo", value: "bar"}}},
			&Condition{op: UnEqual, column: "bar", value: "foo"},
		}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("!foo&!bar")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Chain{op: All, rules: []Filter{
			&Chain{op: None, rules: []Filter{&Exists{column: "foo"}}},
			&Chain{op: None, rules: []Filter{&Exists{column: "bar"}}},
		}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("!(!foo|bar)")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Chain{op: None, rules: []Filter{
			&Chain{op: Any, rules: []Filter{
				&Chain{op: None, rules: []Filter{&Exists{column: "foo"}}},
				&Exists{column: "bar"},
			}},
		}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("!(!(foo|bar))")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Chain{op: None, rules: []Filter{
			&Chain{op: None, rules: []Filter{
				&Chain{op: Any, rules: []Filter{
					&Exists{column: "foo"},
					&Exists{column: "bar"}},
				},
			}},
		}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("foo=bar&bar!=foo")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Chain{op: All, rules: []Filter{
			&Condition{op: Equal, column: "foo", value: "bar"},
			&Condition{op: UnEqual, column: "bar", value: "foo"},
		}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("!(foo=bar|bar=foo)&(foo!=bar|bar!=foo)")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Chain{op: All, rules: []Filter{
			&Chain{op: None, rules: []Filter{
				&Chain{op: Any, rules: []Filter{
					&Condition{op: Equal, column: "foo", value: "bar"},
					&Condition{op: Equal, column: "bar", value: "foo"},
				}},
			}},
			&Chain{op: Any, rules: []Filter{
				&Condition{op: UnEqual, column: "foo", value: "bar"},
				&Condition{op: UnEqual, column: "bar", value: "foo"},
			}},
		}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("foo=bar&bar!=foo&john>doe|doe<john&column!=value|column=value")
		assert.Nil(t, err, "There should be no errors but got: %s", err)

		expected = &Chain{op: Any, rules: []Filter{
			&Chain{op: All, rules: []Filter{
				&Condition{op: Equal, column: "foo", value: "bar"},
				&Condition{op: UnEqual, column: "bar", value: "foo"},
				&Condition{op: GreaterThan, column: "john", value: "doe"},
			}},
			&Chain{op: All, rules: []Filter{
				&Condition{op: LessThan, column: "doe", value: "john"},
				&Condition{op: UnEqual, column: "column", value: "value"},
			}},
			&Condition{op: Equal, column: "column", value: "value"},
		}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("foo!~bar&bar~foo|col=val&val!~col|col~val&yes!=no|yes~no&no~yes|foo&!test")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Chain{op: Any, rules: []Filter{
			&Chain{op: All, rules: []Filter{
				&Condition{op: UnLike, column: "foo", value: "bar"},
				&Condition{op: Like, column: "bar", value: "foo"},
			}},
			&Chain{op: All, rules: []Filter{
				&Condition{op: Equal, column: "col", value: "val"},
				&Condition{op: UnLike, column: "val", value: "col"},
			}},
			&Chain{op: All, rules: []Filter{
				&Condition{op: Like, column: "col", value: "val"},
				&Condition{op: UnEqual, column: "yes", value: "no"},
			}},
			&Chain{op: All, rules: []Filter{
				&Condition{op: Like, column: "yes", value: "no"},
				&Condition{op: Like, column: "no", value: "yes"},
			}},
			&Chain{op: All, rules: []Filter{
				NewExists("foo"),
				&Chain{op: None, rules: []Filter{NewExists("test")}},
			}},
		}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("foo=bar&bar!=foo&(john>doe|doe<john&column!=value)|column=value")
		assert.Nil(t, err, "There should be no errors but got: %s", err)

		expected = &Chain{op: Any, rules: []Filter{
			&Chain{op: All, rules: []Filter{
				&Condition{op: Equal, column: "foo", value: "bar"},
				&Condition{op: UnEqual, column: "bar", value: "foo"},
				&Chain{op: Any, rules: []Filter{
					&Condition{op: GreaterThan, column: "john", value: "doe"},
					&Chain{op: All, rules: []Filter{
						&Condition{op: LessThan, column: "doe", value: "john"},
						&Condition{op: UnEqual, column: "column", value: "value"},
					}},
				}},
			}},
			&Condition{op: Equal, column: "column", value: "value"},
		}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("foo=bar&bar!=foo|(john>doe|doe<john&column!=value)&column=value")
		assert.Nil(t, err, "There should be no errors but got: %s", err)

		expected = &Chain{op: Any, rules: []Filter{
			// The first two filter conditions
			&Chain{op: All, rules: []Filter{
				&Condition{op: Equal, column: "foo", value: "bar"},
				&Condition{op: UnEqual, column: "bar", value: "foo"},
			}},
			&Chain{op: All, rules: []Filter{
				&Chain{op: Any, rules: []Filter{ // Represents the filter conditions within the parentheses
					&Condition{op: GreaterThan, column: "john", value: "doe"},
					&Chain{op: All, rules: []Filter{
						&Condition{op: LessThan, column: "doe", value: "john"},
						&Condition{op: UnEqual, column: "column", value: "value"},
					}},
				}},
				// The last filter condition
				&Condition{op: Equal, column: "column", value: "value"},
			}},
		}}
		assert.Equal(t, expected, rule)

		rule, err = Parse("foo=bar&bar!=foo|(john>doe|doe<john&(column!=value|value!~column))&column=value")
		assert.Nil(t, err, "There should be no errors but got: %s", err)

		expected = &Chain{op: Any, rules: []Filter{
			// The first two filter conditions
			&Chain{op: All, rules: []Filter{
				&Condition{op: Equal, column: "foo", value: "bar"},
				&Condition{op: UnEqual, column: "bar", value: "foo"},
			}},
			&Chain{op: All, rules: []Filter{
				&Chain{op: Any, rules: []Filter{ // Represents the filter conditions within the parentheses
					&Condition{op: GreaterThan, column: "john", value: "doe"},
					&Chain{op: All, rules: []Filter{
						&Condition{op: LessThan, column: "doe", value: "john"},
						&Chain{op: Any, rules: []Filter{ // Represents the filter conditions within the nested parentheses
							&Condition{op: UnEqual, column: "column", value: "value"},
							&Condition{op: UnLike, column: "value", value: "column"},
						}},
					}},
				}},
				// The last filter condition
				&Condition{op: Equal, column: "column", value: "value"},
			}},
		}}
		assert.Equal(t, expected, rule)
	})

	t.Run("UrlEncodedFilter", func(t *testing.T) {
		t.Parallel()

		rule, err := Parse("col%3Cumn<val%3Cue")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected := &Condition{op: LessThan, column: "col<umn", value: "val<ue"}
		assert.Equal(t, expected, rule)

		rule, err = Parse("col%7Cumn")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.Equal(t, &Exists{column: "col|umn"}, rule)

		rule, err = Parse("col%7Cumn=val%7Cue")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Condition{op: Equal, column: "col|umn", value: "val|ue"}
		assert.Equal(t, expected, rule)

		rule, err = Parse("col%7Cumn!=val%7Cue")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.Equal(t, &Condition{op: UnEqual, column: "col|umn", value: "val|ue"}, rule)

		rule, err = Parse("col%7Cumn~val%7Cue")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Condition{op: Like, column: "col|umn", value: "val|ue"}
		assert.Equal(t, expected, rule)

		rule, err = Parse("col%7Cumn!~val%7Cue")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		assert.Equal(t, &Condition{op: UnLike, column: "col|umn", value: "val|ue"}, rule)

		rule, err = Parse("col%26umn<=val%26ue")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Condition{op: LessThanEqual, column: "col&umn", value: "val&ue"}
		assert.Equal(t, expected, rule)

		rule, err = Parse("col%28umn>val%28ue")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Condition{op: GreaterThan, column: "col(umn", value: "val(ue"}
		assert.Equal(t, expected, rule)

		rule, err = Parse("col%29umn>=val%29ue")
		assert.Nil(t, err, "There should be no errors but got: %s", err)
		expected = &Condition{op: GreaterThanEqual, column: "col)umn", value: "val)ue"}
		assert.Equal(t, expected, rule)
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
	f.Add("\xff")
	f.Add("col%7umn")
	f.Add("foo\u0000")
	f.Add("==")
	f.Add("&&")
	f.Add(" ") // End of invalid filters!
	f.Add("foo=bar")
	f.Add("foo!=bar")
	f.Add("foo~bar*")
	f.Add("foo!~bar*")
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
		rule, err := Parse(expr)
		t.Logf("Parsing filter expression %q - ERROR: %v", expr, err)

		if strings.Count(expr, "(") != strings.Count(expr, ")") {
			assert.Error(t, err)
			assert.Nil(t, rule)
		} else if err == nil && !strings.ContainsAny(expr, "!&|!>~<=") {
			assert.IsType(t, new(Exists), rule)
		}
	})
}
