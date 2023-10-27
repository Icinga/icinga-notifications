%{

package filter

// reduceFilter reduces the given filter rules into a single filter chain (initiated with the provided operator).
// When the operator type of the first argument (Filter) is not of type filter.Any or the given operator is not
// of type filter.All, this will just create a new chain with the new op and append all the filter rules to it.
// Otherwise, it will pop the last pushed rule of that chain (first argument) and append it to the new *And chain.
//
// Example: `foo=bar|bar~foo&col!~val`
// The first argument `rule` is supposed to be a filter.Any Chain containing the first two conditions.
// We then call this function when the parser is processing the logical `&` op and the Unlike condition,
// and what this function will do is logically re-group the conditions into `foo=bar|(bar~foo&col!~val)`.
func reduceFilter(rule Filter, op string, rules ...Filter) Filter {
	chain, ok := rule.(*Chain);
	if ok && chain.op == Any && LogicalOp(op) == All {
	    // Retrieve the last pushed condition and append it to the new "And" chain instead
	    andChain, _ := NewChain(All, chain.pop())
	    andChain.add(rules...)

	    chain.add(andChain)

	    return chain
	}

	// If the given operator is the same as the already existsing chains operator (*chain),
	// we don't need to create another chain of the same operator type. Avoids something
	// like &Chain{op: All, &Chain{op: All, ...}}
	if chain == nil || chain.op != LogicalOp(op) {
	    newChain, err := NewChain(LogicalOp(op), rule)
	    if err != nil {
		// Just panic, filter.Parse will try to recover from this.
		panic(err)
	    }

	    chain = newChain
	}

	chain.add(rules...)

	return chain
}
%}

%union {
	expr Filter
	text string
}

%type <expr> filter_rule
%type <expr> filter_chain
%type <expr> conditions_expr
%type <expr> maybe_negated_condition_expr
%type <expr> condition_expr
%type <expr> exists_expr

%type <text> comparison_op
%type <text> optional_negation
%type <text> identifier
%type <text> logical_op

%token T_EQUAL "="
%token T_UNEQUAL "!" T_EQUAL
%token T_LIKE "~"
%token T_UNLIKE "!" T_LIKE
%token T_LESS_THAN "<"
%token T_GREATER_THAN ">"
%token T_LESS_THAN_OR_EQUAL "<" T_EQUAL
%token T_GREATER_THAN_OR_EQUAL ">" T_EQUAL
%token <text> T_STRING
%token <text> T_IDENTIFIER

%type <text> T_EQUAL
%type <text> T_UNEQUAL
%type <text> T_LIKE
%type <text> T_UNLIKE
%type <text> T_LESS_THAN
%type <text> T_GREATER_THAN
%type <text> T_LESS_THAN_OR_EQUAL
%type <text> T_GREATER_THAN_OR_EQUAL

%type <text> "!" "&" "|"

// This is just used for declaring explicit precedence and resolves shift/reduce conflicts
// in `filter_chain` and `conditions_expr` rules.
%nonassoc PREFER_SHIFTING_LOGICAL_OP

%nonassoc T_EQUAL T_UNEQUAL T_LIKE T_UNLIKE
%nonassoc T_LESS_THAN T_LESS_THAN_OR_EQUAL T_GREATER_THAN T_GREATER_THAN_OR_EQUAL

%left "!" "&" "|"
%left "("
%right ")"

%%

filter_rule: filter_chain logical_op filter_chain
	{
		$$ = reduceFilter($1, $2, $3)
		yylex.(*Lexer).rule = $$
	}
	| filter_chain
	{
		yylex.(*Lexer).rule = $$
	}
	;

filter_chain: conditions_expr logical_op maybe_negated_condition_expr
	{
		$$ = reduceFilter($1, $2, $3)
	}
	| conditions_expr %prec PREFER_SHIFTING_LOGICAL_OP
	;

conditions_expr: maybe_negated_condition_expr logical_op maybe_negated_condition_expr
	{
		$$ = reduceFilter($1, $2, $3)
	}
	| maybe_negated_condition_expr %prec PREFER_SHIFTING_LOGICAL_OP
	;

maybe_negated_condition_expr: optional_negation condition_expr
	{
		if $1 != "" {
			// NewChain is only going to return an error if an invalid operator is specified, and since
			// we explicitly provide the None operator, we don't expect an error to be returned.
			$$, _ = NewChain(None, $2)
		} else {
			$$ = $2
		}
	}
	;

condition_expr: "(" filter_chain ")"
	{
		$$ = $2
	}
	| identifier comparison_op identifier
	{
		cond, err := NewCondition($1, CompOperator($2), $3)
		if err != nil {
			// Something went wrong, so just panic and filter.Parse will try to recover from this.
			panic(err)
		}

		$$ = cond
	}
	| exists_expr
	;

exists_expr: identifier
	{
		$$ = NewExists($1)
	}
	;

identifier: T_IDENTIFIER
	| T_STRING
	;

optional_negation:  /* empty */ { $$ = "" }
	| "!"
	;

logical_op: "&"
	| "|"
	;

comparison_op: T_EQUAL
	| T_UNEQUAL
	| T_LIKE
	| T_UNLIKE
	| T_LESS_THAN
	| T_LESS_THAN_OR_EQUAL
	| T_GREATER_THAN
	| T_GREATER_THAN_OR_EQUAL
	;
