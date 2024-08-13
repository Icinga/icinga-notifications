%{

package filter

import "net/url"

%}

%union {
	expr Filter
	lop LogicalOp
	cop CompOperator
	text string
}

%token <text> T_IDENTIFIER

%token <cop> T_EQUAL
%token <cop> T_UNEQUAL
%token <cop> T_LIKE
%token <cop> T_UNLIKE
%token <cop> T_LESS
%token <cop> T_GTR
%token <cop> T_LEQ
%token <cop> T_GEQ

%token <lop> T_LOR
%token <lop> T_LAND
%token <lop> T_LNOT

%type <expr> filter_rule
%type <expr> filter_chain
%type <expr> condition_expr

%type <cop> comparison_op
%type <text> identifier

%left T_LOR
%left T_LAND
%nonassoc T_EQUAL T_UNEQUAL T_LIKE T_UNLIKE
%nonassoc T_LESS T_LEQ T_GTR T_GEQ
%left T_LNOT
%left '('

%%

filter_rule: filter_chain
  {
    yylex.(*Lexer).rule = $1
  }
  | error
  {
    return 1 // We don't recover from errors, so give up parsing.
  }
  ;

filter_chain: condition_expr
  | filter_rule T_LOR filter_rule
  {
    v, err := reduceFilter($1, Any, $3)
    if err != nil {
      yylex.Error(err.Error())
      return 1
    }
    $$ = v
  }
  | filter_rule T_LAND filter_rule
  {
    v, err := reduceFilter($1, All, $3)
    if err != nil {
      yylex.Error(err.Error())
      return 1
    }
    $$ = v
  }
  ;

condition_expr: identifier
  {
    $$ = NewExists($1)
  }
  | identifier comparison_op identifier
  {
    cond, err := NewCondition($1, $2, $3)
    if err != nil {
      yylex.Error(err.Error())
      return 1
    }
    $$ = cond
  }
  | T_LNOT condition_expr
  {
    // NewChain is only going to return an error if an invalid operator is specified, and since
    // we explicitly provide the None operator, we don't expect an error to be returned.
    $$, _ = NewChain(None, $2)
  }
  | '(' filter_rule ')'
  {
    $$ = $2
  }
  ;

identifier: T_IDENTIFIER
  {
    column, err := url.QueryUnescape($1)
    if err != nil {
      yylex.Error(err.Error())
      return 1
    }
    $$ = column
  }
  ;

comparison_op: T_EQUAL
  | T_UNEQUAL
  | T_LIKE
  | T_UNLIKE
  | T_LESS
  | T_LEQ
  | T_GTR
  | T_GEQ
  ;

%%

// reduceFilter reduces the left and right filters using the specified logical operator.
//
// If the left hand side filter is already of type *Chain and the provided operator is the same as the
// operator of the left hand side filter, it will not create a new chain but instead add the right hand
// side filter to the existing chain. This avoids creating nested chains of the same operator type, such as
// &Chain{op: All, &Chain{op: All, ...}} and keeps the filter structure flat. If the left hand side filter is
// not a *Chain, it will create a new chain with the specified operator  and add both filters filter to it.
//
// Returns the resulting filter chain or an error if the creation of the chain fails.
func reduceFilter(left Filter, op LogicalOp, right Filter) (Filter, error) {
	chain, ok := left.(*Chain)
	if !ok || chain.op != op {
		var err error
		chain, err = NewChain(op, left)
		if err != nil {
			return nil, err
		}
	}
	chain.rules = append(chain.rules, right)

	return chain, nil
}
