package lucene

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/grindlemire/go-lucene/pkg/driver"
	"github.com/grindlemire/go-lucene/pkg/lucene/expr"
)

// DynamoDBPartiQLDriver converts Lucene queries to DynamoDB PartiQL.
type DynamoDBPartiQLDriver struct {
	driver.Base
	fields map[string]FieldInfo
}

func NewDynamoDBDriver(fields []FieldInfo) (*DynamoDBPartiQLDriver, error) {
	fieldMap, err := buildFieldMap(fields)
	if err != nil {
		return nil, err
	}

	fns := map[expr.Operator]driver.RenderFN{
		expr.Literal:   driver.Shared[expr.Literal],
		expr.And:       driver.Shared[expr.And],
		expr.Or:        driver.Shared[expr.Or],
		expr.Not:       driver.Shared[expr.Not],
		expr.Equals:    driver.Shared[expr.Equals],
		expr.Range:     driver.Shared[expr.Range],
		expr.Must:      driver.Shared[expr.Must],
		expr.MustNot:   driver.Shared[expr.MustNot],
		expr.Wild:      driver.Shared[expr.Wild],
		expr.Regexp:    driver.Shared[expr.Regexp],
		expr.Like:      dynamoDBLike, // Custom LIKE for DynamoDB functions
		expr.Greater:   driver.Shared[expr.Greater],
		expr.GreaterEq: driver.Shared[expr.GreaterEq],
		expr.Less:      driver.Shared[expr.Less],
		expr.LessEq:    driver.Shared[expr.LessEq],
		expr.In:        driver.Shared[expr.In],
		expr.List:      driver.Shared[expr.List],
	}

	return &DynamoDBPartiQLDriver{
		Base: driver.Base{
			RenderFNs: fns,
		},
		fields: fieldMap,
	}, nil
}

// RenderParam renders the expression, intercepting array fields before
// delegating to the base driver.
//
// The traversal is ours rather than the base driver's because
// go-lucene's Base.RenderParam recurses through its own serializeParams and
// never calls back into an override — so a top-level override alone would miss
// an Equals nested inside AND/OR.
func (d *DynamoDBPartiQLDriver) RenderParam(e *expr.Expression) (string, []any, error) {
	return d.renderNode(e)
}

func (d *DynamoDBPartiQLDriver) renderNode(e *expr.Expression) (string, []any, error) {
	if e == nil {
		return "", nil, nil
	}

	// A negation over field:null collapses to IS NOT NULL. The base driver
	// already does this, and it must be intercepted before the walker below
	// claims Not/MustNot, which would render NOT (field IS NULL) instead.
	if e.Op == expr.Not || e.Op == expr.MustNot {
		if _, ok := nullEqualsOperand(e.Left); ok {
			return d.Base.RenderParam(e)
		}
	}

	if sql, params, ok, err := renderLogicalOps(e, d.renderNode, d.Base.RenderParam); ok {
		return sql, params, err
	}

	name, fieldType, isArray := d.resolveArrayField(e.Left)

	switch e.Op {
	case expr.Equals:
		if isArray && !isNullValue(e.Right) {
			// go-lucene wraps a grouped value list as EQUALS(field, OR(...)),
			// so the right side may be a whole sub-tree. Expand it against this
			// field rather than stringifying it into a single bound value.
			if group, ok := e.Right.(*expr.Expression); ok && (group.Op == expr.Or || group.Op == expr.And) {
				return d.renderGroupedArrayExpr(name, fieldType, group)
			}
			safe, err := escapePartiQLIdentifier(name)
			if err != nil {
				return "", nil, fmt.Errorf("invalid field name: %w", err)
			}
			elem, err := normalizeArrayElemValue(name, fieldType, extractLiteralValue(e.Right))
			if err != nil {
				return "", nil, err
			}
			param, err := arrayContainsParam(elem)
			if err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("contains(%s, ?)", safe), []any{param}, nil
		}
	case expr.Wild, expr.Like:
		if isArray {
			return "", nil, errArrayWildcardUnsupported(name)
		}
	case expr.Greater, expr.GreaterEq, expr.Less, expr.LessEq:
		if isArray {
			return "", nil, fmt.Errorf(
				"operator %s is not supported on array field '%s'; array fields support containment (%s:value) and null checks",
				e.Op, name, name,
			)
		}
	case expr.Range:
		if isArray {
			return "", nil, fmt.Errorf(
				"range queries are not supported on array field '%s'; array fields support containment (%s:value) and null checks",
				name, name,
			)
		}
	}

	return d.Base.RenderParam(e)
}

// errArrayWildcardUnsupported is the shared rejection for a wildcard against a
// DynamoDB array: PartiQL contains() tests element membership, not substrings,
// so no rendering would mean what the user asked for.
func errArrayWildcardUnsupported(name string) error {
	return fmt.Errorf(
		"wildcard matching is not supported on array field '%s' in DynamoDB; PartiQL contains() tests element membership, not substrings — use %s:value for containment",
		name, name,
	)
}

// renderGroupedArrayExpr expands an OR/AND group under an array field into a
// containment test per leaf, e.g. tags:(golang OR rust) becomes
// (contains(tags, ?) OR contains(tags, ?)).
//
// The leaves carry go-lucene's default field rather than the outer one, so the
// field name comes from the caller and each leaf contributes only its value.
func (d *DynamoDBPartiQLDriver) renderGroupedArrayExpr(name string, fieldType reflect.Type, e *expr.Expression) (string, []any, error) {
	leftStr, leftParams, err := d.renderGroupedArrayLeaf(name, fieldType, e.Left)
	if err != nil {
		return "", nil, err
	}

	if e.Right == nil {
		return leftStr, leftParams, nil
	}

	rightStr, rightParams, err := d.renderGroupedArrayLeaf(name, fieldType, e.Right)
	if err != nil {
		return "", nil, err
	}

	op := " OR "
	if e.Op == expr.And {
		op = " AND "
	}
	return fmt.Sprintf("(%s%s%s)", leftStr, op, rightStr), append(leftParams, rightParams...), nil
}

func (d *DynamoDBPartiQLDriver) renderGroupedArrayLeaf(name string, fieldType reflect.Type, v any) (string, []any, error) {
	safe, err := escapePartiQLIdentifier(name)
	if err != nil {
		return "", nil, fmt.Errorf("invalid field name: %w", err)
	}

	if e, ok := v.(*expr.Expression); ok {
		switch e.Op {
		case expr.Or, expr.And:
			return d.renderGroupedArrayExpr(name, fieldType, e)
		case expr.Equals:
			// A leaf's own field is the parser's default field, not the outer
			// one; only its value matters here.
			return d.renderGroupedArrayLeaf(name, fieldType, e.Right)
		case expr.Wild, expr.Like:
			return "", nil, errArrayWildcardUnsupported(name)
		}
	}

	if isNullValue(v) {
		return fmt.Sprintf("%s IS NULL", safe), nil, nil
	}

	elem, err := normalizeArrayElemValue(name, fieldType, extractLiteralValue(v))
	if err != nil {
		return "", nil, err
	}
	param, err := arrayContainsParam(elem)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("contains(%s, ?)", safe), []any{param}, nil
}

// arrayContainsParam converts a validated element into the typed
// AttributeValue that matches how it is stored, so a numeric or boolean array
// is compared against a number or a boolean rather than a string that can
// never match.
//
// It switches on the value itself rather than re-deriving the type from the
// field's reflect.Kind. Re-deriving was a second source of truth that could
// disagree with the validation that produced the value.
func arrayContainsParam(v elemValue) (types.AttributeValue, error) {
	switch t := v.Val.(type) {
	case bool:
		return &types.AttributeValueMemberBOOL{Value: t}, nil
	case int64:
		return &types.AttributeValueMemberN{Value: strconv.FormatInt(t, 10)}, nil
	case float64:
		bits := 64
		if v.Kind == reflect.Float32 {
			bits = 32
		}
		return &types.AttributeValueMemberN{Value: strconv.FormatFloat(t, 'g', -1, bits)}, nil
	case string:
		return &types.AttributeValueMemberS{Value: t}, nil
	default:
		return nil, fmt.Errorf("unsupported array element type %T", v.Val)
	}
}

// resolveArrayField returns the model field name for a column reference, the
// field's Go type, and whether it is a multi-valued (array) attribute.
//
// The type is nil unless the field is a known array, so a caller that ignores
// the bool cannot read a stale zero value.
func (d *DynamoDBPartiQLDriver) resolveArrayField(in any) (string, reflect.Type, bool) {
	raw, ok := columnName(in)
	if !ok || raw == "" {
		return "", nil, false
	}
	info, exists := d.fields[raw]
	if !exists || !isArrayField(info.Type) {
		return raw, nil, false
	}
	return raw, info.Type, true
}

// RenderPartiQL renders the expression to DynamoDB PartiQL with AttributeValue parameters.
func (d *DynamoDBPartiQLDriver) RenderPartiQL(e *expr.Expression) (string, []types.AttributeValue, error) {
	// Use base rendering with ? placeholders
	str, params, err := d.RenderParam(e)
	if err != nil {
		return "", nil, err
	}

	// Convert params to DynamoDB AttributeValues. Array containment binds an
	// already-typed AttributeValue (number, boolean) so it survives this step;
	// everything else is still a string attribute.
	attrValues := make([]types.AttributeValue, len(params))
	for i, param := range params {
		if av, ok := param.(types.AttributeValue); ok {
			attrValues[i] = av
			continue
		}
		attrValues[i] = &types.AttributeValueMemberS{Value: fmt.Sprintf("%v", param)}
	}

	return str, attrValues, nil
}

// escapePartiQLString escapes a string value for safe use in PartiQL string literals.
// Escapes single quotes by doubling them (PartiQL standard).
func escapePartiQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

var (
	// partiQLIdentifierPattern matches valid PartiQL identifiers (alphanumeric and underscore only)
	partiQLIdentifierPattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
)

// escapePartiQLIdentifier escapes a field name for safe use in PartiQL.
// Validates that the identifier contains only safe characters (alphanumeric, underscore).
// Returns error if identifier contains potentially dangerous characters.
func escapePartiQLIdentifier(identifier string) (string, error) {
	if !partiQLIdentifierPattern.MatchString(identifier) {
		return "", fmt.Errorf("invalid identifier: contains unsafe characters (only alphanumeric and underscore allowed)")
	}
	return identifier, nil
}

// unquotePartiQLString safely removes surrounding quotes from a PartiQL string literal.
// Handles already-escaped quotes correctly.
func unquotePartiQLString(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	return s
}

// dynamoDBLike implements LIKE using DynamoDB's begins_with and contains functions.
func dynamoDBLike(left, right string) (string, error) {
	// Validate and escape field name (left)
	safeLeft, err := escapePartiQLIdentifier(left)
	if err != nil {
		return "", fmt.Errorf("invalid field name: %w", err)
	}

	// Extract the raw value from the right side (remove quotes if present)
	rawValue := unquotePartiQLString(right)

	// Analyze pattern for wildcards
	hasPrefix := strings.HasPrefix(rawValue, "%")
	hasSuffix := strings.HasSuffix(rawValue, "%")

	if hasPrefix && hasSuffix {
		// %value% -> contains(field, value)
		value := strings.Trim(rawValue, "%")
		escapedValue := escapePartiQLString(value)
		return fmt.Sprintf("contains(%s, '%s')", safeLeft, escapedValue), nil
	}
	if !hasPrefix && hasSuffix {
		// value% -> begins_with(field, value)
		value := strings.TrimSuffix(rawValue, "%")
		escapedValue := escapePartiQLString(value)
		return fmt.Sprintf("begins_with(%s, '%s')", safeLeft, escapedValue), nil
	}
	if hasPrefix && !hasSuffix {
		// %value -> contains(field, value) (DynamoDB doesn't have ends_with)
		value := strings.TrimPrefix(rawValue, "%")
		escapedValue := escapePartiQLString(value)
		return fmt.Sprintf("contains(%s, '%s')", safeLeft, escapedValue), nil
	}

	// Exact match - escape the value and wrap in quotes
	escapedValue := escapePartiQLString(rawValue)
	return fmt.Sprintf("%s = '%s'", safeLeft, escapedValue), nil
}
