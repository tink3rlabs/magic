package mql

import (
	"fmt"
	"strings"
)

type Expr interface {
	Eval(input map[string]interface{}) bool
}

type BinaryExpr struct {
	Op    string
	Left  Expr
	Right Expr
}

type NotExpr struct {
	Expr Expr
}

type TermExpr struct {
	Key   string
	Op    string // ":" or "IN" or "NOT IN"
	Value interface{}
}

type GroupExpr struct {
	Inner Expr
}

// Eval implementations

func (e *BinaryExpr) Eval(input map[string]interface{}) bool {
	switch e.Op {
	case "AND":
		return e.Left.Eval(input) && e.Right.Eval(input)
	case "OR":
		return e.Left.Eval(input) || e.Right.Eval(input)
	}
	return false
}

func (e *NotExpr) Eval(input map[string]interface{}) bool {
	return !e.Expr.Eval(input)
}

func (e *GroupExpr) Eval(input map[string]interface{}) bool {
	return e.Inner.Eval(input)
}

func (e *TermExpr) Eval(input map[string]interface{}) bool {
	val, ok := input[e.Key]

	switch e.Op {
	case ":":
		if !ok {
			return false
		}
		return wildcardMatch(fmt.Sprintf("%v", val), fmt.Sprintf("%v", e.Value))

	case "IN":
		if !ok {
			return false
		}
		return listContains(e.Value.([]string), fmt.Sprintf("%v", val))

	case "NOT IN":
		// If the label is missing, we treat it as not in the forbidden list
		if !ok {
			return true
		}
		return !listContains(e.Value.([]string), fmt.Sprintf("%v", val))
	}
	return false
}

func wildcardMatch(value, pattern string) bool {
	pattern = strings.ToLower(pattern)
	value = strings.ToLower(value)

	if strings.Contains(pattern, "*") {
		parts := strings.Split(pattern, "*")
		starts := parts[0]
		ends := parts[len(parts)-1]
		return strings.HasPrefix(value, starts) && strings.HasSuffix(value, ends)
	}
	return value == pattern
}

func listContains(list []string, val string) bool {
	val = strings.ToLower(val)
	for _, v := range list {
		if strings.ToLower(v) == val {
			return true
		}
	}
	return false
}
