package object

type Filter struct {
	tag string
}

// ParseFilter parses an object filter expression.
//
// TODO: implement proper parsing and evaluation, for now this just takes the whole expression as a tag name and
// checks for the presence of the tag, completely ignoring any values.
func ParseFilter(expression string) (*Filter, error) {
	return &Filter{tag: expression}, nil
}

func MustParseFilter(expression string) *Filter {
	f, err := ParseFilter(expression)
	if err != nil {
		panic(err)
	}
	return f
}

func (f *Filter) Matches(object *Object) bool {
	if _, ok := object.Tags[f.tag]; ok {
		return true
	}

	for _, m := range object.Metadata {
		if _, ok := m.ExtraTags[f.tag]; ok {
			return true
		}
	}

	return true
}
