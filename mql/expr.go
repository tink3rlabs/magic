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

	// The IN and NOT IN branches below repeat the list assertion rather than
	// sharing one. Merging them would have to validate the list before applying
	// the absent-key rule, which would turn NOT IN on a missing key from true
	// into false. The duplication is what keeps that rule first.
	case "IN":
		if !ok {
			return false
		}
		list, isList := e.Value.([]string)
		if !isList {
			return false
		}
		return listContains(list, fmt.Sprintf("%v", val))

	case "NOT IN":
		// If the label is missing, we treat it as not in the forbidden list
		if !ok {
			return true
		}
		// A malformed expression must not grant anything, so an unusable list
		// evaluates to false here rather than mirroring the IN case.
		list, isList := e.Value.([]string)
		if !isList {
			return false
		}
		return !listContains(list, fmt.Sprintf("%v", val))
	}
	return false
}

// wildcardMatch reports whether value matches pattern, case-insensitively,
// where "*" stands for any run of characters including an empty one. Every
// literal segment of the pattern must be present, in order, and no two segments
// may be satisfied by the same characters of value.
func wildcardMatch(value, pattern string) bool {
	pattern = strings.ToLower(pattern)
	value = strings.ToLower(value)

	if !strings.Contains(pattern, "*") {
		return value == pattern
	}

	segments := strings.Split(pattern, "*")

	// The segment before the first "*" is anchored to the start of value, and
	// the one after the last "*" to its end. Consuming each match as we go is
	// what stops two segments from overlapping the same characters: "a*a" must
	// not match "a", and "ab*bc" must not match "abc".
	prefix, suffix := segments[0], segments[len(segments)-1]
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	rest := value[len(prefix):]

	// Interior segments float, but must appear in the order they were written.
	// Taking the leftmost occurrence of each is safe: with "*" as the only
	// metacharacter, matching a segment as early as possible always leaves the
	// most room for the segments after it.
	for _, segment := range segments[1 : len(segments)-1] {
		idx := strings.Index(rest, segment)
		if idx < 0 {
			return false
		}
		rest = rest[idx+len(segment):]
	}

	return strings.HasSuffix(rest, suffix)
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
