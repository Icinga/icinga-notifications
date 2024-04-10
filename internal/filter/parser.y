%{

package filter

import "net/url"

// reduceFilter reduces the given filter rules into a single filter chain (initiated with the provided operator).
// When the operator type of the second argument (Filter) is not of type filter.Any or the given operator is not
// of type filter.All, this will just create a new chain with the new op and append all the filter rules to it.
// Otherwise, it will pop the last pushed rule of that chain (second argument) and append it to the new *And chain.
//
// Example: `foo=bar|bar~foo&col!~val`
// The second argument `left` is supposed to be a filter.Any Chain contains the first two conditions.
// We then call this function when the parser is processing the logical `&` op and the Unlike condition,
// and what this function will do is logically re-group the conditions into `foo=bar|(bar~foo&col!~val)`.
func reduceFilter(op string, left Filter, right Filter) Filter {
	chain, ok := left.(*Chain)
	if ok && chain.op == Any && LogicalOp(op) == All {
		// Retrieve the last pushed filter Condition and append it to the new "And" chain instead
		back := chain.pop()
		// Chain#pop can return a filter Chain, and since we are only allowed to regroup two filter conditions,
		// we must traverse the last element of every single popped Chain till we reach a filter condition.
		for back != nil {
			if backChain, ok := back.(*Chain); !ok || backChain.grouped {
				// If the popped element is not of type filter Chain or the filter chain is parenthesized,
				// we don't need to continue here, so break out of the loop.
				break
			}

			// Re-add the just popped item before stepping into it and popping its last item.
			chain.add(back)

			chain = back.(*Chain)
			back = chain.pop()
		}

		andChain, _ := NewChain(All, back)
		// We don't need to regroup an already grouped filter chain, since braces gain
		// a higher precedence than any logical operators.
		if anyChain, ok := right.(*Chain); ok && anyChain.op == Any && !chain.grouped && !anyChain.grouped {
			andChain.add(anyChain.top())
			// Prepend the newly created All chain
			anyChain.rules = append([]Filter{andChain}, anyChain.rules...)

			chain.add(anyChain)
		} else {
			andChain.add(right)
			chain.add(andChain)
		}

		return left
	}

	// If the given operator is the same as the already existing chains operator (*chain),
	// we don't need to create another chain of the same operator type. Avoids something
	// like &Chain{op: All, &Chain{op: All, ...}}
	if chain == nil || chain.op != LogicalOp(op) {
		var err error
		chain, err = NewChain(LogicalOp(op), left)
		if err != nil {
			// Just panic, filter.Parse will try to recover from this.
			panic(err)
		}
	}

	chain.add(right)

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

%token <text> T_EQUAL
%token <text> T_UNEQUAL
%token <text> T_LIKE
%token <text> T_UNLIKE
%token <text> T_LESS_THAN
%token <text> T_GREATER_THAN
%token <text> T_LESS_THAN_OR_EQUAL
%token <text> T_GREATER_THAN_OR_EQUAL
%token <text> T_IDENTIFIER

%type <text> "|" "&"
%type <text> "!"

// This is just used for declaring explicit precedence and resolves shift/reduce conflicts
// in `filter_chain` and `conditions_expr` rules.
%nonassoc PREFER_SHIFTING_LOGICAL_OP

%nonassoc T_EQUAL T_UNEQUAL T_LIKE T_UNLIKE
%nonassoc T_LESS_THAN T_LESS_THAN_OR_EQUAL T_GREATER_THAN T_GREATER_THAN_OR_EQUAL

%left "|" "&"
%left "!"
%left "("
%right ")"

%%

filter_rule: filter_chain logical_op filter_chain
	{
		$$ = reduceFilter($2, $1, $3)
		yylex.(*Lexer).rule = $$
	}
	| filter_chain %prec PREFER_SHIFTING_LOGICAL_OP
	{
		yylex.(*Lexer).rule = $$
	}
	| filter_rule logical_op filter_chain
	{
		$$ = reduceFilter($2, $1, $3)
		yylex.(*Lexer).rule = $$
	}
	;

filter_chain: conditions_expr logical_op maybe_negated_condition_expr
	{
		$$ = reduceFilter($2, $1, $3)
	}
	| conditions_expr %prec PREFER_SHIFTING_LOGICAL_OP
	;

conditions_expr: maybe_negated_condition_expr logical_op maybe_negated_condition_expr
	{
		$$ = reduceFilter($2, $1, $3)
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

condition_expr: "(" filter_rule ")"
	{
		$$ = $2
		if chain, ok := $$.(*Chain); ok {
		    chain.grouped = true
		}
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
	{
		column, err := url.QueryUnescape($1)
		if err != nil {
			// Something went wrong, so just panic and filter.Parse will try to recover from this.
			panic(err)
		}

		$$ = column
	}
	;

optional_negation: /* empty */ { $$ = "" }
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

%%
