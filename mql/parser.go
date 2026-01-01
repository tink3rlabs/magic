package mql

import (
	"fmt"
	"strings"
	"text/scanner"
)

type Parser struct {
	s     scanner.Scanner
	tok   rune
	text  string
	input string
	pos   scanner.Position
}

func NewParser(input string) *Parser {
	var s scanner.Scanner
	s.Init(strings.NewReader(input))
	s.Mode = scanner.ScanIdents | scanner.ScanStrings | scanner.ScanInts
	p := &Parser{s: s, input: input}
	p.pos = s.Pos()
	p.next()
	return p
}

func (p *Parser) next() {
	p.tok = p.s.Scan()
	p.text = p.s.TokenText()
	p.pos = p.s.Pos()
}

func (p *Parser) Parse() (Expr, error) {
	return p.parseOr()
}

func (p *Parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.text == "OR" {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "OR", Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseAnd() (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.text == "AND" {
		p.next()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "AND", Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseUnary() (Expr, error) {
	if p.text == "NOT" {
		p.next()
		expr, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return &NotExpr{Expr: expr}, nil
	}
	return p.parsePrimary()
}

func (p *Parser) parsePrimary() (Expr, error) {
	if p.text == "(" {
		p.next()
		expr, err := p.Parse()
		if err != nil {
			return nil, err
		}
		if p.text != ")" {
			return nil, fmt.Errorf("expected ')'")
		}
		p.next()
		return &GroupExpr{Inner: expr}, nil
	}
	return p.parseTerm()
}

func (p *Parser) parseTerm() (Expr, error) {
	// Build the key by reading tokens. Keys can contain dots (e.g., "ci.repo")
	// The loop continues while we see dots, building up the full key name.
	// It stops when we hit the operator (":", "IN", or "NOT")
	var key strings.Builder
	key.WriteString(p.text)
	p.next()

	// Continue reading tokens for the key while we see dots
	// This handles keys like "ci.repo" or "namespace.name"
	for p.text == "." {
		key.WriteString(p.text)
		p.next()
		if p.tok == scanner.Ident || p.tok == scanner.Int {
			key.WriteString(p.text)
			p.next()
		}
	}

	switch p.text {
	case ":":
		// The colon token ends at p.pos.Offset (1-based), which means the next character (the value) starts at p.pos.Offset
		// But we need to skip the colon itself. The colon is at p.pos.Offset - 1 (0-based), so value starts at p.pos.Offset (0-based = p.pos.Offset - 1 + 1)
		// Actually, Offset is the position AFTER the token, so if colon is at index 7, Offset is 8, and value starts at index 8 (0-based)
		colonEndPos := p.pos.Offset // 1-based position after colon
		val := p.parseValueFromPos(colonEndPos)
		return &TermExpr{Key: key.String(), Op: ":", Value: val}, nil
	case "IN":
		p.next()
		return p.parseList(key.String(), "IN")
	case "NOT":
		p.next()
		if p.text != "IN" {
			return nil, fmt.Errorf("expected 'IN' after 'NOT'")
		}
		p.next()
		return p.parseList(key.String(), "NOT IN")
	}
	return nil, fmt.Errorf("expected ':' or IN after key")
}

// parseValueFromPos reads a value from the input string starting at the given position (1-based scanner position)
// It continues reading until it hits whitespace or a logical operator (AND, OR, NOT)
func (p *Parser) parseValueFromPos(startPos int) string {
	// Convert 1-based scanner position to 0-based string index
	// startPos is the position AFTER the colon token, so it's already pointing to the value start
	startIdx := startPos - 1
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx >= len(p.input) {
		return ""
	}

	// Skip the colon if we're still on it (shouldn't happen, but be safe)
	if startIdx < len(p.input) && p.input[startIdx] == ':' {
		startIdx++
	}

	// Skip whitespace at start
	for startIdx < len(p.input) && (p.input[startIdx] == ' ' || p.input[startIdx] == '\t') {
		startIdx++
	}

	// Check if it's a quoted string
	if startIdx < len(p.input) && p.input[startIdx] == '"' {
		// Find the closing quote
		endIdx := startIdx + 1
		for endIdx < len(p.input) && p.input[endIdx] != '"' {
			if p.input[endIdx] == '\\' && endIdx+1 < len(p.input) {
				endIdx += 2 // Skip escaped character
			} else {
				endIdx++
			}
		}
		if endIdx < len(p.input) {
			val := p.input[startIdx+1 : endIdx]
			// Advance scanner past the quoted string
			for p.s.Pos().Offset-1 < endIdx+1 {
				p.next()
			}
			return val
		}
	}

	// For unquoted values, read until whitespace or logical operator
	var value strings.Builder
	i := startIdx
	for i < len(p.input) {
		ch := p.input[i]

		// Stop at whitespace
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			// Check if what follows is a logical operator
			remaining := strings.TrimSpace(p.input[i:])
			if strings.HasPrefix(remaining, "AND") ||
				strings.HasPrefix(remaining, "OR") ||
				strings.HasPrefix(remaining, "NOT") {
				break
			}
			// Also stop at closing parenthesis
			if strings.HasPrefix(remaining, ")") {
				break
			}
			// For unquoted values, whitespace typically ends the value
			break
		}

		// Stop at closing parenthesis
		if ch == ')' {
			break
		}

		value.WriteByte(ch)
		i++
	}

	valStr := value.String()

	// Advance the scanner to sync with our manual parsing
	// We need to consume tokens until we're past the value we just read
	for {
		// Check if current token position is past our value
		currPos := p.s.Pos().Offset - 1
		if currPos >= i {
			// We're past the value, we're synced
			break
		}
		// Scan next token
		p.next()
		// If we hit EOF or a logical operator, stop
		if p.tok == scanner.EOF || p.text == "AND" || p.text == "OR" || p.text == "NOT" {
			break
		}
	}

	return valStr
}

func (p *Parser) parseList(key, op string) (Expr, error) {
	if p.text != "[" {
		return nil, fmt.Errorf("expected '[' for %s clause", op)
	}
	p.next()
	values := []string{}
	for p.text != "]" && p.tok != scanner.EOF {
		values = append(values, strings.Trim(p.text, `"`))
		p.next()
		if p.text == "," {
			p.next()
		}
	}
	if p.text != "]" {
		return nil, fmt.Errorf("expected ']' to close list")
	}
	p.next()
	return &TermExpr{Key: key, Op: op, Value: values}, nil
}
