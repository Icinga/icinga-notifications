package object

import (
	"github.com/icinga/noma/internal/filter"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestFilter(t *testing.T) {
	obj := &Object{
		Tags: map[string]string{
			"host": "db1.example.com",
		},
		Metadata: map[int64]*SourceMetadata{
			1: {ExtraTags: map[string]string{
				"hostgroup/database-server":     "",
				"hostgroup/Nuremberg (Germany)": "",
			}},
			2: {ExtraTags: map[string]string{
				"country": "DE",
			}},
		},
	}

	testdata := []struct {
		Expression string
		Expected   bool
	}{
		{"host=db1.example.com", true},
		{"host=db2.example.com", false},
		{"Host=db1.example.com", false},
		{"host", true},
		{"Host", false},
		{"service", false},
		{"!service", true},
		{"host=*.example.com&hostgroup/database-server", true},
		{"host=*.example.com&!hostgroup/database-server", false},
		{"!service&(country=DE&hostgroup/database-server)", true},
		{"!service&!(country=AT|country=CH)", true},
		{"hostgroup/Nuremberg %28Germany%29", true},
		{"host>a", true},
		{"host>z", false},
		{"host>=db1&host<=db2", true},
	}

	for _, td := range testdata {
		f, err := filter.Parse(td.Expression)
		if assert.NoError(t, err, "parsing %q should not return an error", td.Expression) {
			matched, err := f.Eval(obj)
			assert.NoError(t, err)
			assert.Equal(t, td.Expected, matched, "unexpected filter result for %q", td.Expression)
		}
	}
}
