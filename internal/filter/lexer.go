//go:generate goyacc -v parser.output -o parser.go parser.y

package filter

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"text/scanner"
)

// identifiersMatcher contains a compiled regexp and is used by the Lexer to match filter identifiers.
// Currently, it allows to match any character except a LogicalOp and CompOperator.
var identifiersMatcher = regexp.MustCompile("[^!&|~<>=()]")

// init just sets the global yyErrorVerbose variable to true.
func init() {
	// Enable parsers error verbose to get more context of the parsing failures
	yyErrorVerbose = true
}

// Parse wraps the auto generated yyParse function.
// It parses the given filter string and returns on success a Filter instance.
func Parse(expr string) (rule Filter, err error) {
	lex := new(Lexer)
	lex.IsIdentRune = isIdentRune
	lex.Init(strings.NewReader(expr))

	// Set the scanner mode to recognize only identifiers. This way all unrecognized tokens will be returned
	// just as they are, and our Lexer#Lex() method will then recognize whatever valid input is required.
	// Note: This is in fact not necessary, as our custom function `isIdentRune` accepts any token that matches the
	// regex pattern `identifiersMatcher`, so the scanner would never match all the scanner.GoTokens except ScanIdents.
	lex.Mode = scanner.ScanIdents

	// scanner.Init sets the error function to nil, therefore, we have to register
	// our error function after the scanner initialization.
	lex.Scanner.Error = lex.ScanError

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

	yyParse(lex)

	rule, err = lex.rule, lex.err
	return
}

// Lexer is used to tokenize the filter input into a set of literals.
// This is just a wrapper around the Scanner type and implements the yyLexer interface used by the parser.
type Lexer struct {
	scanner.Scanner

	rule Filter
	err  error
}

func (l *Lexer) Lex(yyval *yySymType) int {
	token := l.Scan()
	lit := l.TokenText()
	yyval.text = lit
	if token == scanner.Ident {
		return T_IDENTIFIER
	}

	if token == scanner.String {
		return T_STRING
	}

	switch lit {
	case "&":
		return '&'
	case "|":
		return '|'
	case "~":
		return T_LIKE
	case "=":
		return T_EQUAL
	case "(":
		return '('
	case ")":
		return ')'
	case "!":
		next := l.Peek()
		switch next {
		case '=', '~':
			yyval.text = "!" + string(next)
			// Since we manually picked the next char input, we also need to advance the internal scanner
			// states by calling Scan. Otherwise, the same rune will be scanned multiple times.
			l.Scan()

			if next == '~' {
				return T_UNLIKE
			} else {
				return T_UNEQUAL
			}
		default:
			return '!'
		}
	case "<":
		if next := l.Peek(); next == '=' {
			yyval.text = "<="
			// Since we manually picked the next char input, we also need to advance the internal scanner
			// states by calling Scan. Otherwise, the same rune will be scanned multiple times.
			l.Scan()

			return T_LESS_THAN_OR_EQUAL
		}

		return T_LESS_THAN
	case ">":
		if next := l.Peek(); next == '=' {
			yyval.text = ">="
			// Since we manually picked the next char input, we also need to advance the internal scanner
			// states by calling Scan. Otherwise, the same rune will be scanned multiple times.
			l.Scan()

			return T_GREATER_THAN_OR_EQUAL
		}

		return T_GREATER_THAN
	}

	// No more inputs to scan that we are interested in.
	// Scan returns EOF as well if there's no more token to stream, but we just want to be explicit.
	return scanner.EOF
}

// Error receives any syntax/semantic errors produced by the parser.
//
// The parser never returns an error when it fails to parse, but will forward the errors to our lexer with some
// additional context instead. This function then wraps the provided err and adds line, column number and offset
// to the error string. Error is equivalent to "yyerror" in the original yacc.
func (l *Lexer) Error(s string) {
	l.err = fmt.Errorf("%d:%d (%d): %s", l.Line, l.Column, l.Offset, s)

	// Always reset the current filter rule when encountering an error.
	l.rule = nil
}

// ScanError is used to capture all errors the Scanner encounters.
//
// It's a rare case that the scanner actually will fail to scan the input string, but in these cases it will just
// output to std.Err and we won't be able to notice this. Hence, this function is registered by the filter.Parse
// function after the Lexer initialisation.
func (l *Lexer) ScanError(_ *scanner.Scanner, msg string) { l.Error(msg) }

// isIdentRune provides custom implementation of scanner.IsIdentRune.
// This function determines whether a given character is allowed to be part of an identifier.
func isIdentRune(ch rune, _ int) bool { return identifiersMatcher.MatchString(string(ch)) }
