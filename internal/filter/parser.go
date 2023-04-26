package filter

import (
	"fmt"
	"net/url"
	"strings"
)

type Parser struct {
	tag                          string
	pos, length, openParenthesis int
}

// Parse parses an object filter expression.
func Parse(expression string) (Filter, error) {
	parser := &Parser{tag: expression, length: len(expression)}
	if parser.length == 0 {
		return &All{}, nil
	}

	return parser.readFilter(0, "", nil)
}

// readFilter reads the entire filter from the Parser.tag and derives a filter.Rule from it.
// Returns an error on parsing failure.
func (p *Parser) readFilter(nestingLevel int, operator string, rules []Rule) (Rule, error) {
	negate := false
	for p.pos < p.length {
		condition, err := p.readCondition()
		if err != nil {
			return nil, err
		}

		next := p.readChar()
		if condition == nil {
			if next == "!" {
				negate = true
				continue
			}

			if operator == "" && len(rules) > 0 && (next == "&" || next == "|") {
				operator = next
				continue
			}

			if next == "" {
				break
			}

			if next == ")" {
				p.openParenthesis--

				if nestingLevel > 0 {
					next = p.nextChar()
					if next != "" && next != "&" && next != "|" && next != ")" {
						p.pos++
						return nil, p.parseError(next, "Expected logical operator")
					}

					break
				}

				return nil, p.parseError(next, "")
			}

			if next == "(" {
				if p.nextChar() == "&" || p.nextChar() == "|" {
					// When a logical operator follows directly after the opening parenthesis "(",
					// this can't be a valid expression. E.g. "!(&"
					next = p.readChar()

					return nil, p.parseError(next, "")
				}

				p.openParenthesis++

				op := ""
				if negate {
					op = "!"
				}

				rule, err := p.readFilter(nestingLevel+1, op, nil)
				if err != nil {
					return nil, err
				}

				rules = append(rules, rule)
				negate = false
				continue
			}

			if next == operator {
				continue
			}

			// When the current operator is a "!", the next one can't be a logical operator.
			if operator != "!" && (next == "&" || next == "|") {
				if operator == "&" {
					if len(rules) > 1 {
						all := &All{rules: rules}
						rules = []Rule{all}
					}

					operator = next
				} else if operator == "|" || (operator == "!" && next == "&") {
					// The last pushed filter chain
					lastRule := rules[len(rules)-1]
					// Erase it from our Rules slice
					rules = rules[:len(rules)-1]

					rule, err := p.readFilter(nestingLevel+1, next, []Filter{lastRule})
					if err != nil {
						return nil, err
					}

					rules = append(rules, rule)
				}

				continue
			}

			return nil, p.parseError(next, fmt.Sprintf("operator level %d", nestingLevel))
		} else {
			if negate {
				negate = false
				rules = append(rules, &None{rules: []Rule{condition}})
			} else {
				rules = append(rules, condition)
			}

			if next == "" {
				break
			}

			if next == ")" {
				p.openParenthesis--

				if nestingLevel > 0 {
					next = p.nextChar()
					if next != "" && next != "&" && next != "|" && next != ")" {
						p.pos++
						return nil, p.parseError(next, "Expected logical operator")
					}

					break
				}

				return nil, p.parseError(next, "")
			}

			if next == operator {
				continue
			}

			if next == "&" || next == "|" {
				if operator == "" || operator == "&" {
					if operator == "&" && len(rules) > 1 {
						all := &All{rules: rules}
						rules = []Rule{all}
					}

					operator = next
				} else if operator == "" || (operator == "!" && next == "&") {
					// The last pushed filter chain
					lastRule := rules[len(rules)-1]
					// Erase it from our Rules slice
					rules = rules[:len(rules)-1]

					rule, err := p.readFilter(nestingLevel+1, next, []Filter{lastRule})
					if err != nil {
						return nil, err
					}

					rules = append(rules, rule)
				}

				continue
			}

			return nil, p.parseError(next, "")
		}
	}

	if nestingLevel == 0 && p.pos < p.length {
		return nil, p.parseError(operator, "Did not read full filter")
	}

	if nestingLevel == 0 && p.openParenthesis > 0 {
		return nil, fmt.Errorf("invalid filter '%s', missing %d closing ')' at pos %d", p.tag, p.openParenthesis, p.pos)
	}

	if nestingLevel == 0 && p.openParenthesis < 0 {
		return nil, fmt.Errorf("invalid filter '%s', unexpected closing ')' at pos %d", p.tag, p.pos)
	}

	var chain Filter
	switch operator {
	case "&":
		chain = &All{rules: rules}
	case "|":
		chain = &Any{rules: rules}
	case "!":
		chain = &None{rules: rules}
	case "":
		if nestingLevel == 0 && rules != nil {
			// There is only one filter tag, no chain
			return rules[0], nil
		}

		chain = &All{rules: rules}
	default:
		return nil, p.parseError(operator, "")
	}

	return chain, nil
}

// readCondition reads the next filter.Rule.
// returns nil if there is no char to read and an error on parsing failure.
func (p *Parser) readCondition() (Rule, error) {
	column, err := p.readColumn()
	if err != nil || column == "" {
		return nil, err
	}

	operator := ""
	if strings.Contains("=><!", p.nextChar()) {
		operator = p.readChar()
	}

	if operator == "" {
		return NewExists(column), nil
	}

	if strings.Contains("><!", operator) {
		if p.nextChar() == "=" {
			operator += p.readChar()
		}
	}

	value, err := p.readValue()
	if err != nil {
		return nil, err
	}

	condition, err := p.createCondition(column, operator, value)
	if err != nil {
		return nil, err
	}

	return condition, nil
}

// createCondition creates a filter.Rule based on the given operator.
// returns nil when invalid operator is given.
func (p *Parser) createCondition(column string, operator string, value string) (Rule, error) {
	column = strings.TrimSpace(column)
	switch operator {
	case "=":
		if strings.Contains(value, "*") {
			return &Like{column: column, value: value}, nil
		}

		return &Equal{column: column, value: value}, nil
	case "!=":
		if strings.Contains(value, "*") {
			return &Unlike{column: column, value: value}, nil
		}

		return &UnEqual{column: column, value: value}, nil
	case ">":
		return &GreaterThan{column: column, value: value}, nil
	case ">=":
		return &GreaterThanOrEqual{column: column, value: value}, nil
	case "<":
		return &LessThan{column: column, value: value}, nil
	case "<=":
		return &LessThanOrEqual{column: column, value: value}, nil
	default:
		return nil, fmt.Errorf("invalid operator %s provided", operator)
	}
}

// readColumn reads a column name from the Parser.tag.
// returns empty string if there is no char to read.
func (p *Parser) readColumn() (string, error) {
	return url.QueryUnescape(p.readUntil("=()&|><!"))
}

// readValue reads a single value from the Parser.tag.
// returns empty string and a parsing error on invalid filter
func (p *Parser) readValue() (string, error) {
	value := p.readUntil(")&|><")
	if value == "" {
		return "", nil
	}

	if index := strings.Index(value, "("); index != -1 {
		pos := p.pos + index + 1 - len(value)
		return "", fmt.Errorf("invalid filter '%s', unexpected opening '(' at pos %d", p.tag, pos)
	}

	return url.QueryUnescape(value)
}

// readUntil reads chars until any of the given characters
// May return empty string if there is no char to read
func (p *Parser) readUntil(chars string) string {
	var buffer string
	for char := p.readChar(); char != ""; char = p.readChar() {
		if strings.Contains(chars, char) {
			p.pos--
			break
		}

		buffer += char
	}

	return buffer
}

// readChar peeks the next char of the Parser.tag and increments the Parser.pos by one
// returns empty if there is no char to read
func (p *Parser) readChar() string {
	if p.pos < p.length {
		pos := p.pos
		p.pos++

		return string(p.tag[pos])
	}

	return ""
}

// nextChar peeks the next char from the parser tag
// returns empty string if there is no char to read
func (p *Parser) nextChar() string {
	if p.pos < p.length {
		return string(p.tag[p.pos])
	}

	return ""
}

// parseError returns a formatted and detailed parser error.
// If you don't provide the char that causes the parser to fail, the char at `p.pos` is automatically used.
// By specifying the `msg` arg you can provide additional err hints that can help debugging.
func (p *Parser) parseError(invalidChar string, msg string) error {
	if invalidChar == "" {
		pos := p.pos
		if p.pos == p.length {
			pos--
		}

		invalidChar = string(p.tag[pos])
	}

	if msg != "" {
		msg = ": " + msg
	}

	return fmt.Errorf("invalid filter '%s', unexpected %s at pos %d%s", p.tag, invalidChar, p.pos, msg)
}
