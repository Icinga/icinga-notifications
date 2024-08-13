//go:generate go tool goyacc -v parser.output -o parser.go parser.y

package filter

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"text/scanner"
)

// LexerErrCode is the error code used by the lexer to indicate that it encountered an error
// while lexing the input filter string and that the parser should stop parsing.
const LexerErrCode = math.MaxInt

// tokenFriendlyNames contains a list of all the defined parser tokens and their respective
// friendly names used to output in error messages.
var tokenFriendlyNames = []struct {
	have string
	want string
}{
	{"$end", "EOF"},
	{"$unk", "unknown"},
	{"T_IDENTIFIER", "'column or value'"},
	{"T_EQUAL", "'='"},
	{"T_UNEQUAL", "'!='"},
	{"T_LIKE", "'~'"},
	{"T_UNLIKE", "'!~'"},
	{"T_LESS", "'<'"},
	{"T_GTR", "'>'"},
	{"T_LEQ", "'<='"},
	{"T_GEQ", "'>='"},
	{"T_LOR", "'|'"},
	{"T_LAND", "'&'"},
	{"T_LNOT", "'!'"},
}

// init just sets the global yyErrorVerbose variable to true.
func init() {
	// Enable parsers error verbose to get more context of the parsing failures
	yyErrorVerbose = true

	for i, t := range yyToknames {
		// Replace all parser token names by their corresponding friendly names.
		for _, td := range tokenFriendlyNames {
			if t == td.have {
				yyToknames[i] = td.want
				break
			}
		}
	}
	tokenFriendlyNames = nil // Free up memory, we don't need this anymore.
}

// Parse wraps the auto generated yyParse function.
// It parses the given filter string and returns on success a Filter instance.
func Parse(expr string) (rule Filter, err error) {
	lex := new(Lexer)
	lex.IsIdentRune = isIndentRune
	lex.Init(strings.NewReader(expr))

	// Configure the scanner mode to identify oly specific tokens we are interested in.
	lex.Mode = scanner.ScanIdents | scanner.ScanFloats | scanner.ScanChars | scanner.ScanStrings
	// It's a rare case that the scanner actually will fail to scan the input string, but in these cases it will just
	// output to stdErr, and we won't be able to notice this. Hence, we've to register our own error handler!
	lex.Scanner.Error = func(_ *scanner.Scanner, msg string) { lex.Error(msg) }

	defer func() {
		// All the grammar rules panics when encountering any errors while reducing the filter rules, so try
		// to recover from it and return an error instead. Since we're using a named return values, we can set
		// the err value even in deferred function. See https://go.dev/blog/defer-panic-and-recover
		if r := recover(); r != nil {
			err = errors.New(fmt.Sprint(r))
		}

		if err != nil {
			// The lexer may contain some incomplete filter rules constructed before the parser panics, so reset it.
			rule = nil
		}
	}()

	if yyParse(lex) != 0 {
		// If the parser returns a non-zero value, it means that it encountered an error while parsing.
		// The error is already set in the lexer, so we can just return it.
		if lex.err != nil {
			return nil, lex.err
		}

		// If no error was set, but the parser returned a non-zero value, we can return a generic error.
		return nil, fmt.Errorf("failed to parse filter expression: %s", expr)
	}

	return lex.rule, nil
}

// Lexer is the lexer used by the parser to tokenize the input filter string.
//
// It embeds the scanner.Scanner to use its functionality and implements the Lex method
// to provide the tokens to the parser. The Lexer also holds the current filter rule being constructed
// by the parser and the last error encountered during lexing or parsing.
type Lexer struct {
	scanner.Scanner

	rule Filter // rule is the current filter rule being constructed by the parser.
	err  error  // err is the last error encountered by the lexer or parser.
}

func (l *Lexer) Lex(yyval *yySymType) int {
	tok := l.Scan()
	if l.err != nil {
		return LexerErrCode
	}

	if tok == scanner.Ident {
		yyval.text = l.TokenText()
		return T_IDENTIFIER
	}

	switch tok {
	case '|':
		yyval.lop = All
		return T_LOR
	case '&':
		yyval.lop = Any
		return T_LAND
	case '~':
		yyval.cop = Like
		return T_LIKE
	case '=':
		yyval.cop = Equal
		return T_EQUAL
	case '!':
		next := l.Peek()
		switch next {
		case '=', '~':
			// Since we manually picked the next char input, we also need to advance the internal scanner
			// states by calling Scan. Otherwise, the same rune will be scanned multiple times.
			l.Scan()

			if next == '~' {
				yyval.cop = UnLike
				return T_UNLIKE
			} else {
				yyval.cop = UnEqual
				return T_UNEQUAL
			}
		default:
			yyval.lop = None
			return T_LNOT
		}
	case '<':
		if next := l.Peek(); next == '=' {
			yyval.cop = LessThanEqual
			// Since we manually picked the next char input, we also need to advance the internal scanner
			// states by calling Scan. Otherwise, the same rune will be scanned multiple times.
			l.Scan()

			return T_LEQ
		}

		yyval.cop = LessThan
		return T_LESS
	case '>':
		if next := l.Peek(); next == '=' {
			yyval.cop = GreaterThanEqual
			// Since we manually picked the next char input, we also need to advance the internal scanner
			// states by calling Scan. Otherwise, the same rune will be scanned multiple times.
			l.Scan()

			return T_GEQ
		}

		yyval.cop = GreaterThan
		return T_GTR
	}

	return int(tok)
}

// Error receives any syntax/semantic errors produced by the parser.
//
// The parser never returns an error when it fails to parse, but will forward the errors to our lexer with some
// additional context instead. This function then wraps the provided err and adds line, column number and offset
// to the error string. Error is equivalent to "yyerror" in the original yacc.
func (l *Lexer) Error(s string) {
	// Don't overwrite the error if it was already set, since we want to keep the first error encountered.
	if l.err == nil {
		// Always reset the current filter rule when encountering an error.
		l.rule = nil
		l.err = fmt.Errorf("%d:%d (%d): %s", l.Line, l.Column, l.Offset, s)
	}
}

// isIndentRune provides custom implementation of scanner.IsIdentRune.
// This function determines whether a given character is allowed to be part of an identifier.
func isIndentRune(ch rune, _ int) bool {
	return ch != '!' && ch != '&' && ch != '|' && ch != '~' && ch != '<' && ch != '>' &&
		ch != '=' && ch != '(' && ch != ')'
}
