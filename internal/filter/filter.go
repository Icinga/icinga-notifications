package filter

type Filter struct {
	rule Rule
}

// ParseFilter parses an object filter expression.
func ParseFilter(expr string) (*Filter, error) {
	parser := &Parser{}
	rule, err := parser.Parse(expr)
	if err != nil {
		return nil, err
	}

	return &Filter{rule: rule}, nil
}

// Matches returns true if the given filterable object matches the parsed rules of this filter.
func (f *Filter) Matches(filterable Filterable) bool {
	return f.rule.Eval(filterable)
}
