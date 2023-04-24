package filter

import (
	"fmt"
	"strings"
)

type Parser struct {
	tag    string
	pos    int
	length int
}

func NewParser() *Parser {
	return &Parser{}
}

// Parse parses an object filter expression.
func (p *Parser) Parse(expression string) (Rule, error) {
	p.tag = expression
	if len(p.tag) == 0 {
		return &All{}, nil
	}

	p.pos = 0
	p.length = len(p.tag)

	return p.readFilter(0, "", nil, true)
}

func (p *Parser) readFilter(nestingLevel int, operator string, rules []Rule, explicit bool) (Rule, error) {
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
				if nestingLevel > 0 {
					if !explicit {
						p.pos--
					} else {
						next = p.nextChar()
						if next != "" && next != "&" && next != "|" && next != ")" {
							p.pos++
							return nil, p.parseError(next, "Expected logical operator")
						}
					}

					break
				}

				return nil, p.parseError(next, "")
			}

			if next == "(" {
				op := ""
				if negate {
					op = "!"
				}

				rule, err := p.readFilter(nestingLevel+1, op, nil, true)
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

			if next == "&" || next == "|" {
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

					rule, err := p.readFilter(nestingLevel+1, next, []Rule{lastRule}, false)
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
				if nestingLevel > 0 {
					if !explicit {
						p.pos--
					} else {
						next = p.nextChar()
						if next != "" && next != "&" && next != "|" && next != ")" {
							p.pos++
							return nil, p.parseError(next, "Expected logical operator")
						}
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

					rule, err := p.readFilter(nestingLevel+1, next, []Rule{lastRule}, false)
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

	var chain Rule
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

// readCondition reads the next filter.Conditional.
// returns nil if there is no char to read and an error on parsing failure.
func (p *Parser) readCondition() (Rule, error) {
	column := p.readColumn()
	if column == "" {
		return nil, nil
	}

	for _, operator := range "<>" {
		if pos := strings.Index(column, string(operator)); pos != -1 {
			if p.nextChar() == "=" {
				break
			}

			value := column[pos+1:]
			column = column[0:pos]

			condition, err := p.createCondition(column, string(operator), value)
			if err != nil {
				return nil, err
			}

			return condition, nil
		}
	}

	operator := ""
	if strings.Contains("=><!", p.nextChar()) {
		operator = p.readChar()
	}

	if operator == "" {
		return NewExists(column), nil
	}

	if operator == "=" {
		last := column[len(column)-1:]
		if last == ">" || last == "<" {
			operator = last + operator
			column = column[0 : len(column)-1]
		}
	} else if strings.Contains("><!", operator) {
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

// createCondition creates a filter.Conditional based on the given operator.
// returns nil when invalid operator is given.
func (p *Parser) createCondition(column string, operator string, value string) (Rule, error) {
	column = strings.TrimSpace(column)
	switch operator {
	case "=":
		if strings.Contains(value, "*") {
			return &Like{Condition: NewCondition(column, value)}, nil
		}

		return &Equal{Condition: NewCondition(column, value)}, nil
	case "!=":
		if strings.Contains(value, "*") {
			return &Unlike{Condition: NewCondition(column, value)}, nil
		}

		return &UnEqual{Condition: NewCondition(column, value)}, nil
	case ">":
		return &GreaterThan{Condition: NewCondition(column, value)}, nil
	case ">=":
		return &GreaterThanOrEqual{Condition: NewCondition(column, value)}, nil
	case "<":
		return &LessThan{Condition: NewCondition(column, value)}, nil
	case "<=":
		return &LessThanOrEqual{Condition: NewCondition(column, value)}, nil
	default:
		return nil, fmt.Errorf("invalid operator %s provided", operator)
	}
}

// readColumn reads a column name from the Parser.tag.
// returns empty string if there is no char to read.
func (p *Parser) readColumn() string {
	return p.readUntil("=()&|><!")
}

// readValue reads a single value from the Parser.tag.
// returns empty string and a parsing error on invalid filter
func (p *Parser) readValue() (string, error) {
	var val string
	if p.nextChar() == "(" {
		p.readChar()
		val = p.readUntil(")")

		if p.readChar() != ")" {
			return "", p.parseError("", "Expected ')'")
		}
	} else {
		val = p.readUntil(")&|><")
	}

	return val, nil
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
		if p.pos > p.length {
			p.pos--
		}

		invalidChar = string(p.tag[p.pos])
	}

	if msg != "" {
		msg = ": " + msg
	}

	return fmt.Errorf("invalid filter '%s', unexpected %s at pos %d%s", p.tag, invalidChar, p.pos, msg)
}
