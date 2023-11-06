// Code generated by goyacc -v parser.output -o parser.go parser.y. DO NOT EDIT.

//line parser.y:2

package filter

import __yyfmt__ "fmt"

//line parser.y:3

import "net/url"

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
	chain, ok := rule.(*Chain)
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

//line parser.y:47
type yySymType struct {
	yys  int
	expr Filter
	text string
}

const T_EQUAL = 57346
const T_UNEQUAL = 57347
const T_LIKE = 57348
const T_UNLIKE = 57349
const T_LESS_THAN = 57350
const T_GREATER_THAN = 57351
const T_LESS_THAN_OR_EQUAL = 57352
const T_GREATER_THAN_OR_EQUAL = 57353
const T_IDENTIFIER = 57354
const PREFER_SHIFTING_LOGICAL_OP = 57355

var yyToknames = [...]string{
	"$end",
	"error",
	"$unk",
	"T_EQUAL",
	"T_UNEQUAL",
	"T_LIKE",
	"T_UNLIKE",
	"T_LESS_THAN",
	"T_GREATER_THAN",
	"T_LESS_THAN_OR_EQUAL",
	"T_GREATER_THAN_OR_EQUAL",
	"T_IDENTIFIER",
	"\"|\"",
	"\"&\"",
	"\"!\"",
	"PREFER_SHIFTING_LOGICAL_OP",
	"\"(\"",
	"\")\"",
}

var yyStatenames = [...]string{}

const yyEofCode = 1
const yyErrCode = 2
const yyInitialStackSize = 16

//line parser.y:181

//line yacctab:1
var yyExca = [...]int8{
	-1, 1,
	1, -1,
	-2, 0,
}

const yyPrivate = 57344

const yyLast = 32

var yyAct = [...]int8{
	14, 30, 22, 23, 24, 25, 26, 28, 27, 29,
	6, 16, 2, 9, 8, 16, 13, 4, 5, 7,
	17, 21, 31, 10, 11, 15, 20, 12, 18, 19,
	3, 1,
}

var yyPact = [...]int16{
	-5, -1000, 0, 0, 0, -1, -1000, -5, -1000, -1000,
	-5, -5, -1000, -5, -2, -1000, -1000, -1000, -1000, -1000,
	-17, 3, -1000, -1000, -1000, -1000, -1000, -1000, -1000, -1000,
	-1000, -1000,
}

var yyPgo = [...]int8{
	0, 31, 12, 30, 17, 27, 25, 21, 18, 0,
	19,
}

var yyR1 = [...]int8{
	0, 1, 1, 2, 2, 3, 3, 4, 5, 5,
	5, 6, 9, 8, 8, 10, 10, 7, 7, 7,
	7, 7, 7, 7, 7,
}

var yyR2 = [...]int8{
	0, 3, 1, 3, 1, 3, 1, 2, 3, 3,
	1, 1, 1, 0, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1,
}

var yyChk = [...]int16{
	-1000, -1, -2, -3, -4, -8, 15, -10, 14, 13,
	-10, -10, -5, 17, -9, -6, 12, -2, -4, -4,
	-2, -7, 4, 5, 6, 7, 8, 10, 9, 11,
	18, -9,
}

var yyDef = [...]int8{
	13, -2, 2, 4, 6, 0, 14, 13, 15, 16,
	13, 13, 7, 13, 11, 10, 12, 1, 3, 5,
	0, 0, 17, 18, 19, 20, 21, 22, 23, 24,
	8, 9,
}

var yyTok1 = [...]int8{
	1, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 15, 3, 3, 3, 3, 14, 3,
	17, 18, 3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 13,
}

var yyTok2 = [...]int8{
	2, 3, 4, 5, 6, 7, 8, 9, 10, 11,
	12, 16,
}

var yyTok3 = [...]int8{
	0,
}

var yyErrorMessages = [...]struct {
	state int
	token int
	msg   string
}{}

//line yaccpar:1

/*	parser for yacc output	*/

var (
	yyDebug        = 0
	yyErrorVerbose = false
)

type yyLexer interface {
	Lex(lval *yySymType) int
	Error(s string)
}

type yyParser interface {
	Parse(yyLexer) int
	Lookahead() int
}

type yyParserImpl struct {
	lval  yySymType
	stack [yyInitialStackSize]yySymType
	char  int
}

func (p *yyParserImpl) Lookahead() int {
	return p.char
}

func yyNewParser() yyParser {
	return &yyParserImpl{}
}

const yyFlag = -1000

func yyTokname(c int) string {
	if c >= 1 && c-1 < len(yyToknames) {
		if yyToknames[c-1] != "" {
			return yyToknames[c-1]
		}
	}
	return __yyfmt__.Sprintf("tok-%v", c)
}

func yyStatname(s int) string {
	if s >= 0 && s < len(yyStatenames) {
		if yyStatenames[s] != "" {
			return yyStatenames[s]
		}
	}
	return __yyfmt__.Sprintf("state-%v", s)
}

func yyErrorMessage(state, lookAhead int) string {
	const TOKSTART = 4

	if !yyErrorVerbose {
		return "syntax error"
	}

	for _, e := range yyErrorMessages {
		if e.state == state && e.token == lookAhead {
			return "syntax error: " + e.msg
		}
	}

	res := "syntax error: unexpected " + yyTokname(lookAhead)

	// To match Bison, suggest at most four expected tokens.
	expected := make([]int, 0, 4)

	// Look for shiftable tokens.
	base := int(yyPact[state])
	for tok := TOKSTART; tok-1 < len(yyToknames); tok++ {
		if n := base + tok; n >= 0 && n < yyLast && int(yyChk[int(yyAct[n])]) == tok {
			if len(expected) == cap(expected) {
				return res
			}
			expected = append(expected, tok)
		}
	}

	if yyDef[state] == -2 {
		i := 0
		for yyExca[i] != -1 || int(yyExca[i+1]) != state {
			i += 2
		}

		// Look for tokens that we accept or reduce.
		for i += 2; yyExca[i] >= 0; i += 2 {
			tok := int(yyExca[i])
			if tok < TOKSTART || yyExca[i+1] == 0 {
				continue
			}
			if len(expected) == cap(expected) {
				return res
			}
			expected = append(expected, tok)
		}

		// If the default action is to accept or reduce, give up.
		if yyExca[i+1] != 0 {
			return res
		}
	}

	for i, tok := range expected {
		if i == 0 {
			res += ", expecting "
		} else {
			res += " or "
		}
		res += yyTokname(tok)
	}
	return res
}

func yylex1(lex yyLexer, lval *yySymType) (char, token int) {
	token = 0
	char = lex.Lex(lval)
	if char <= 0 {
		token = int(yyTok1[0])
		goto out
	}
	if char < len(yyTok1) {
		token = int(yyTok1[char])
		goto out
	}
	if char >= yyPrivate {
		if char < yyPrivate+len(yyTok2) {
			token = int(yyTok2[char-yyPrivate])
			goto out
		}
	}
	for i := 0; i < len(yyTok3); i += 2 {
		token = int(yyTok3[i+0])
		if token == char {
			token = int(yyTok3[i+1])
			goto out
		}
	}

out:
	if token == 0 {
		token = int(yyTok2[1]) /* unknown char */
	}
	if yyDebug >= 3 {
		__yyfmt__.Printf("lex %s(%d)\n", yyTokname(token), uint(char))
	}
	return char, token
}

func yyParse(yylex yyLexer) int {
	return yyNewParser().Parse(yylex)
}

func (yyrcvr *yyParserImpl) Parse(yylex yyLexer) int {
	var yyn int
	var yyVAL yySymType
	var yyDollar []yySymType
	_ = yyDollar // silence set and not used
	yyS := yyrcvr.stack[:]

	Nerrs := 0   /* number of errors */
	Errflag := 0 /* error recovery flag */
	yystate := 0
	yyrcvr.char = -1
	yytoken := -1 // yyrcvr.char translated into internal numbering
	defer func() {
		// Make sure we report no lookahead when not parsing.
		yystate = -1
		yyrcvr.char = -1
		yytoken = -1
	}()
	yyp := -1
	goto yystack

ret0:
	return 0

ret1:
	return 1

yystack:
	/* put a state and value onto the stack */
	if yyDebug >= 4 {
		__yyfmt__.Printf("char %v in %v\n", yyTokname(yytoken), yyStatname(yystate))
	}

	yyp++
	if yyp >= len(yyS) {
		nyys := make([]yySymType, len(yyS)*2)
		copy(nyys, yyS)
		yyS = nyys
	}
	yyS[yyp] = yyVAL
	yyS[yyp].yys = yystate

yynewstate:
	yyn = int(yyPact[yystate])
	if yyn <= yyFlag {
		goto yydefault /* simple state */
	}
	if yyrcvr.char < 0 {
		yyrcvr.char, yytoken = yylex1(yylex, &yyrcvr.lval)
	}
	yyn += yytoken
	if yyn < 0 || yyn >= yyLast {
		goto yydefault
	}
	yyn = int(yyAct[yyn])
	if int(yyChk[yyn]) == yytoken { /* valid shift */
		yyrcvr.char = -1
		yytoken = -1
		yyVAL = yyrcvr.lval
		yystate = yyn
		if Errflag > 0 {
			Errflag--
		}
		goto yystack
	}

yydefault:
	/* default state action */
	yyn = int(yyDef[yystate])
	if yyn == -2 {
		if yyrcvr.char < 0 {
			yyrcvr.char, yytoken = yylex1(yylex, &yyrcvr.lval)
		}

		/* look through exception table */
		xi := 0
		for {
			if yyExca[xi+0] == -1 && int(yyExca[xi+1]) == yystate {
				break
			}
			xi += 2
		}
		for xi += 2; ; xi += 2 {
			yyn = int(yyExca[xi+0])
			if yyn < 0 || yyn == yytoken {
				break
			}
		}
		yyn = int(yyExca[xi+1])
		if yyn < 0 {
			goto ret0
		}
	}
	if yyn == 0 {
		/* error ... attempt to resume parsing */
		switch Errflag {
		case 0: /* brand new error */
			yylex.Error(yyErrorMessage(yystate, yytoken))
			Nerrs++
			if yyDebug >= 1 {
				__yyfmt__.Printf("%s", yyStatname(yystate))
				__yyfmt__.Printf(" saw %s\n", yyTokname(yytoken))
			}
			fallthrough

		case 1, 2: /* incompletely recovered error ... try again */
			Errflag = 3

			/* find a state where "error" is a legal shift action */
			for yyp >= 0 {
				yyn = int(yyPact[yyS[yyp].yys]) + yyErrCode
				if yyn >= 0 && yyn < yyLast {
					yystate = int(yyAct[yyn]) /* simulate a shift of "error" */
					if int(yyChk[yystate]) == yyErrCode {
						goto yystack
					}
				}

				/* the current p has no shift on "error", pop stack */
				if yyDebug >= 2 {
					__yyfmt__.Printf("error recovery pops state %d\n", yyS[yyp].yys)
				}
				yyp--
			}
			/* there is no state on the stack with an error shift ... abort */
			goto ret1

		case 3: /* no shift yet; clobber input char */
			if yyDebug >= 2 {
				__yyfmt__.Printf("error recovery discards %s\n", yyTokname(yytoken))
			}
			if yytoken == yyEofCode {
				goto ret1
			}
			yyrcvr.char = -1
			yytoken = -1
			goto yynewstate /* try again in the same state */
		}
	}

	/* reduction by production yyn */
	if yyDebug >= 2 {
		__yyfmt__.Printf("reduce %v in:\n\t%v\n", yyn, yyStatname(yystate))
	}

	yynt := yyn
	yypt := yyp
	_ = yypt // guard against "declared and not used"

	yyp -= int(yyR2[yyn])
	// yyp is now the index of $0. Perform the default action. Iff the
	// reduced production is ε, $1 is possibly out of range.
	if yyp+1 >= len(yyS) {
		nyys := make([]yySymType, len(yyS)*2)
		copy(nyys, yyS)
		yyS = nyys
	}
	yyVAL = yyS[yyp+1]

	/* consult goto table to find next state */
	yyn = int(yyR1[yyn])
	yyg := int(yyPgo[yyn])
	yyj := yyg + yyS[yyp].yys + 1

	if yyj >= yyLast {
		yystate = int(yyAct[yyg])
	} else {
		yystate = int(yyAct[yyj])
		if int(yyChk[yystate]) != -yyn {
			yystate = int(yyAct[yyg])
		}
	}
	// dummy call; replaced with literal code
	switch yynt {

	case 1:
		yyDollar = yyS[yypt-3 : yypt+1]
//line parser.y:92
		{
			yyVAL.expr = reduceFilter(yyDollar[1].expr, yyDollar[2].text, yyDollar[3].expr)
			yylex.(*Lexer).rule = yyVAL.expr
		}
	case 2:
		yyDollar = yyS[yypt-1 : yypt+1]
//line parser.y:97
		{
			yylex.(*Lexer).rule = yyVAL.expr
		}
	case 3:
		yyDollar = yyS[yypt-3 : yypt+1]
//line parser.y:103
		{
			yyVAL.expr = reduceFilter(yyDollar[1].expr, yyDollar[2].text, yyDollar[3].expr)
		}
	case 5:
		yyDollar = yyS[yypt-3 : yypt+1]
//line parser.y:110
		{
			yyVAL.expr = reduceFilter(yyDollar[1].expr, yyDollar[2].text, yyDollar[3].expr)
		}
	case 7:
		yyDollar = yyS[yypt-2 : yypt+1]
//line parser.y:117
		{
			if yyDollar[1].text != "" {
				// NewChain is only going to return an error if an invalid operator is specified, and since
				// we explicitly provide the None operator, we don't expect an error to be returned.
				yyVAL.expr, _ = NewChain(None, yyDollar[2].expr)
			} else {
				yyVAL.expr = yyDollar[2].expr
			}
		}
	case 8:
		yyDollar = yyS[yypt-3 : yypt+1]
//line parser.y:129
		{
			yyVAL.expr = yyDollar[2].expr
		}
	case 9:
		yyDollar = yyS[yypt-3 : yypt+1]
//line parser.y:133
		{
			cond, err := NewCondition(yyDollar[1].text, CompOperator(yyDollar[2].text), yyDollar[3].text)
			if err != nil {
				// Something went wrong, so just panic and filter.Parse will try to recover from this.
				panic(err)
			}

			yyVAL.expr = cond
		}
	case 11:
		yyDollar = yyS[yypt-1 : yypt+1]
//line parser.y:146
		{
			yyVAL.expr = NewExists(yyDollar[1].text)
		}
	case 12:
		yyDollar = yyS[yypt-1 : yypt+1]
//line parser.y:152
		{
			column, err := url.QueryUnescape(yyDollar[1].text)
			if err != nil {
				// Something went wrong, so just panic and filter.Parse will try to recover from this.
				panic(err)
			}

			yyVAL.text = column
		}
	case 13:
		yyDollar = yyS[yypt-0 : yypt+1]
//line parser.y:163
		{
			yyVAL.text = ""
		}
	}
	goto yystack /* stack new state and value */
}
