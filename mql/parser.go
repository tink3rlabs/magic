package mql

import (
	"fmt"
	"strings"
	"text/scanner"
)

type Parser struct {
	s    scanner.Scanner
	tok  rune
	text string
}

func NewParser(input string) *Parser {
	var s scanner.Scanner
	s.Init(strings.NewReader(input))
	s.Mode = scanner.ScanIdents | scanner.ScanStrings | scanner.ScanInts
	p := &Parser{s: s}
	p.next()
	return p
}

func (p *Parser) next() {
	p.tok = p.s.Scan()
	p.text = p.s.TokenText()
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
	key := p.text
	p.next()

	switch p.text {
	case ":":
		p.next()
		val := p.text
		p.next()
		return &TermExpr{Key: key, Op: ":", Value: val}, nil
	case "IN":
		p.next()
		return p.parseList(key, "IN")
	case "NOT":
		p.next()
		if p.text != "IN" {
			return nil, fmt.Errorf("expected 'IN' after 'NOT'")
		}
		p.next()
		return p.parseList(key, "NOT IN")
	}
	return nil, fmt.Errorf("expected ':' or IN after key")
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
