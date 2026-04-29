package pool

import (
	"sync"

	"github.com/theory/jsonpath"
)

// jsonPathParserPool is a pool of JSONPath parsers to avoid the overhead of creating new parsers for each evaluation.
//
// JSONPath parsers are used to evaluate JSONPath expressions of event rule filters against the events.
// Since the parser doesn't cache any state specific to any given expressions, it can be safely reused
// across multiple evaluations, and thus we can use a pool to reduce the overhead of creating new parsers
// for each evaluation.
var jsonPathParserPool = sync.Pool{
	New: func() any {
		return jsonpath.NewParser()
	},
}

// GetJSONPathParser retrieves a JSONPath parser from the pool.
//
// The caller is responsible for returning the parser to the pool after use by calling [PutJSONPathParser].
func GetJSONPathParser() *jsonpath.Parser {
	return jsonPathParserPool.Get().(*jsonpath.Parser) //nolint:forcetypeassert
}

// PutJSONPathParser returns a JSONPath parser to the pool.
//
// The caller should call this function after using a parser retrieved from the pool to allow
// it to be reused for future evaluations.
func PutJSONPathParser(parser *jsonpath.Parser) {
	jsonPathParserPool.Put(parser)
}
