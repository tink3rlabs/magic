package lucene

import (
	"fmt"

	"github.com/grindlemire/go-lucene/pkg/lucene/expr"
)

// nodeRenderer renders a single expression node. Drivers pass their own
// dispatcher so the walker can recurse without knowing any SQL or PartiQL.
type nodeRenderer func(*expr.Expression) (string, []any, error)

// renderLogicalOps renders the logical operators (Must, MustNot, Not, And, Or),
// recursing into renderNode for each child.
//
// It exists because go-lucene's Base.RenderParam recurses through its own
// serializeParams and never calls back into a driver override — so any driver
// that needs to intercept a leaf node (e.g. Equals on an array field) must own
// the whole traversal, not just the top node. Both SQLDriver and
// DynamoDBPartiQLDriver share this traversal.
//
// ok reports whether e.Op was a logical operator. When ok is false the caller
// handles the node itself.
//
//	  And/Or (binary)        Must/MustNot/Not (unary)
//	     /      \                     |
//	renderNode  renderNode        renderNode
//
// Not and MustNot are the two spellings of negation (`NOT tags:go` and
// `-tags:go`); both must walk the same path or one of them silently falls back
// to go-lucene's base driver and never reaches a driver's leaf overrides.
//
// fallback is used when a child is not an *expr.Expression and the driver has
// no better option; it may be nil, in which case such a node is an error.
//
// fallback is called with e itself, NOT the offending child, and its result is
// returned verbatim — the walker applies no negation wrapping to it, so a
// fallback handling a MustNot node owns emitting the NOT.
func renderLogicalOps(
	e *expr.Expression,
	renderNode nodeRenderer,
	fallback nodeRenderer,
) (sql string, params []any, ok bool, err error) {
	switch e.Op {
	case expr.Must, expr.MustNot, expr.Not:
		if e.Left == nil {
			return "", nil, true, fmt.Errorf("%s operator requires a left operand", e.Op)
		}

		var leftStr string
		var leftParams []any

		if leftExpr, isExpr := e.Left.(*expr.Expression); isExpr {
			leftStr, leftParams, err = renderNode(leftExpr)
			if err != nil {
				return "", nil, true, err
			}
		} else {
			if fallback == nil {
				return "", nil, true, fmt.Errorf("unexpected operand type %T for %s", e.Left, e.Op)
			}
			leftStr, leftParams, err = fallback(e)
			if err != nil {
				return "", nil, true, err
			}
			return leftStr, leftParams, true, nil
		}

		if e.Op == expr.Must {
			return leftStr, leftParams, true, nil
		}
		return fmt.Sprintf("NOT (%s)", leftStr), leftParams, true, nil

	case expr.And, expr.Or:
		if e.Left == nil || e.Right == nil {
			return "", nil, true, fmt.Errorf("%s operator requires both left and right operands", e.Op)
		}

		leftExpr, leftIsExpr := e.Left.(*expr.Expression)
		rightExpr, rightIsExpr := e.Right.(*expr.Expression)

		if !leftIsExpr || !rightIsExpr {
			if fallback == nil {
				return "", nil, true, fmt.Errorf("unexpected operand types for %s", e.Op)
			}
			sql, params, err = fallback(e)
			return sql, params, true, err
		}

		leftStr, leftParams, err := renderNode(leftExpr)
		if err != nil {
			return "", nil, true, err
		}

		rightStr, rightParams, err := renderNode(rightExpr)
		if err != nil {
			return "", nil, true, err
		}

		allParams := append(leftParams, rightParams...)

		if e.Op == expr.And {
			return fmt.Sprintf("(%s) AND (%s)", leftStr, rightStr), allParams, true, nil
		}
		return fmt.Sprintf("(%s) OR (%s)", leftStr, rightStr), allParams, true, nil
	}

	return "", nil, false, nil
}
