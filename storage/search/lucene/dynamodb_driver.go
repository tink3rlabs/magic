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
			param, err := elemAttributeValue(elem)
			if err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("contains(%s, ?)", safe), []any{param}, nil
		}
		if !isArray && name != "" {
			if group, ok := e.Right.(*expr.Expression); ok && isGroup(group) {
				return d.renderGroupedScalarExpr(name, group)
			}
			return d.renderScalarComparison(name, e.Op, e.Right)
		}
	case expr.Wild, expr.Like:
		if isArray {
			return "", nil, errArrayWildcardUnsupported(name)
		}
		if name != "" {
			return d.renderScalarWildcard(name, e.Right)
		}
	case expr.Greater, expr.GreaterEq, expr.Less, expr.LessEq:
		if isArray {
			return "", nil, fmt.Errorf(
				"operator %s is not supported on array field '%s'; array fields support containment (%s:value) and null checks",
				e.Op, name, name,
			)
		}
		if name != "" {
			return d.renderScalarComparison(name, e.Op, e.Right)
		}
	case expr.Range:
		if isArray {
			return "", nil, fmt.Errorf(
				"range queries are not supported on array field '%s'; array fields support containment (%s:value) and null checks",
				name, name,
			)
		}
		if name != "" {
			return d.renderScalarRange(name, e.Right)
		}
	}

	return d.Base.RenderParam(e)
}

// isGroup reports whether v is a grouped value list such as (a OR b).
func isGroup(v any) bool {
	e, ok := v.(*expr.Expression)
	return ok && (e.Op == expr.Or || e.Op == expr.And)
}

// renderGroupedScalarExpr expands a grouped value list under a single-valued
// field, e.g. tenant:(a OR b) becomes ("tenant" = ? OR "tenant" = ?).
//
// go-lucene parses the group as EQUALS(field, OR(EQUALS(default, a), ...)),
// with each leaf on the parser's default field, so the field comes from the
// caller and each leaf contributes only its operator and value.
func (d *DynamoDBPartiQLDriver) renderGroupedScalarExpr(name string, e *expr.Expression) (string, []any, error) {
	leftStr, leftParams, err := d.renderGroupedScalarLeaf(name, e.Left)
	if err != nil {
		return "", nil, err
	}
	if e.Right == nil {
		return leftStr, leftParams, nil
	}
	rightStr, rightParams, err := d.renderGroupedScalarLeaf(name, e.Right)
	if err != nil {
		return "", nil, err
	}

	op := " OR "
	if e.Op == expr.And {
		op = " AND "
	}
	return fmt.Sprintf("(%s%s%s)", leftStr, op, rightStr), append(leftParams, rightParams...), nil
}

// renderGroupedScalarLeaf renders one member of a grouped value list against
// field name. A member can be a bare value rather than an expression, which
// means equality.
func (d *DynamoDBPartiQLDriver) renderGroupedScalarLeaf(name string, v any) (string, []any, error) {
	e, ok := v.(*expr.Expression)
	if !ok || e.Op == expr.Literal || e.Op == expr.Null {
		return d.renderScalarComparison(name, expr.Equals, v)
	}
	switch e.Op {
	case expr.Or, expr.And:
		return d.renderGroupedScalarExpr(name, e)
	case expr.Equals, expr.Greater, expr.GreaterEq, expr.Less, expr.LessEq:
		if isGroup(e.Right) {
			return d.renderGroupedScalarLeaf(name, e.Right)
		}
		return d.renderScalarComparison(name, e.Op, e.Right)
	case expr.Like, expr.Wild:
		if e.Op == expr.Wild {
			return d.renderScalarWildcard(name, e)
		}
		return d.renderScalarWildcard(name, e.Right)
	case expr.Range:
		return d.renderScalarRange(name, e.Right)
	case expr.Must:
		return d.renderGroupedScalarLeaf(name, e.Left)
	case expr.Not, expr.MustNot:
		inner, params, err := d.renderGroupedScalarLeaf(name, e.Left)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("NOT (%s)", inner), params, nil
	}
	return "", nil, fmt.Errorf("unsupported operator %s in grouped values for field '%s'", e.Op, name)
}

var comparisonSymbols = map[expr.Operator]string{
	expr.Equals:    "=",
	expr.Greater:   ">",
	expr.GreaterEq: ">=",
	expr.Less:      "<",
	expr.LessEq:    "<=",
}

// renderScalarComparison renders =, >, >=, < or <= on a single-valued field,
// binding the value as the attribute type of the field.
func (d *DynamoDBPartiQLDriver) renderScalarComparison(name string, op expr.Operator, right any) (string, []any, error) {
	ref, err := quotePartiQLPath(name)
	if err != nil {
		return "", nil, err
	}
	if isNullValue(right) {
		if op == expr.Equals {
			return fmt.Sprintf("%s IS NULL", ref), nil, nil
		}
		return "", nil, fmt.Errorf("comparison operator %s cannot be used with null; use field:null for IS NULL", op)
	}
	param, err := d.scalarParam(name, extractLiteralValue(right))
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("%s %s ?", ref, comparisonSymbols[op]), []any{param}, nil
}

// renderScalarRange renders [a TO b], {a TO b} and their open-ended forms,
// binding both bounds as the attribute type of the field.
func (d *DynamoDBPartiQLDriver) renderScalarRange(name string, right any) (string, []any, error) {
	ref, err := quotePartiQLPath(name)
	if err != nil {
		return "", nil, err
	}
	boundary, ok := right.(*expr.RangeBoundary)
	if !ok {
		return "", nil, fmt.Errorf("invalid range expression structure: expected *expr.RangeBoundary, got %T", right)
	}
	minRaw, minOpen, err := rangeBound(boundary.Min)
	if err != nil {
		return "", nil, err
	}
	maxRaw, maxOpen, err := rangeBound(boundary.Max)
	if err != nil {
		return "", nil, err
	}
	if minOpen && maxOpen {
		return "", nil, fmt.Errorf("both range bounds cannot be wildcards")
	}

	var minParam, maxParam types.AttributeValue
	if !minOpen {
		if minParam, err = d.scalarParam(name, minRaw); err != nil {
			return "", nil, err
		}
	}
	if !maxOpen {
		if maxParam, err = d.scalarParam(name, maxRaw); err != nil {
			return "", nil, err
		}
	}

	lower, upper := ">", "<"
	if boundary.Inclusive {
		lower, upper = ">=", "<="
	}
	switch {
	case minOpen:
		return fmt.Sprintf("%s %s ?", ref, upper), []any{maxParam}, nil
	case maxOpen:
		return fmt.Sprintf("%s %s ?", ref, lower), []any{minParam}, nil
	case boundary.Inclusive:
		return fmt.Sprintf("%s BETWEEN ? AND ?", ref), []any{minParam, maxParam}, nil
	default:
		return fmt.Sprintf("%s > ? AND %s < ?", ref, ref), []any{minParam, maxParam}, nil
	}
}

// rangeBound returns a range bound's raw value, or open=true for *.
func rangeBound(bound any) (raw string, open bool, err error) {
	if isNullValue(bound) {
		return "", false, fmt.Errorf("null is not allowed as a range bound; use field:null for IS NULL")
	}
	if e, ok := bound.(*expr.Expression); ok && e.Op == expr.Wild {
		return "", true, nil
	}
	raw = extractLiteralValue(bound)
	return raw, raw == "*", nil
}

// scalarParam converts raw into the attribute type the field is stored as:
// N for numeric fields, BOOL for booleans and S for everything else,
// including time.Time, which the SDK marshals as an RFC 3339 string. A field
// that isn't in the model, such as a nested path, is bound as S.
func (d *DynamoDBPartiQLDriver) scalarParam(name, raw string) (types.AttributeValue, error) {
	info, known := d.fields[name]
	if !known {
		return &types.AttributeValueMemberS{Value: raw}, nil
	}
	elem, err := normalizeScalarValue(name, info.Type, raw)
	if err != nil {
		return nil, err
	}
	return elemAttributeValue(elem)
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
	param, err := elemAttributeValue(elem)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("contains(%s, ?)", safe), []any{param}, nil
}

// elemAttributeValue converts a validated value into the typed
// AttributeValue that matches how it is stored, so a numeric or boolean field
// or array is compared against a number or a boolean rather than a string
// that can never match.
//
// It switches on the value itself rather than re-deriving the type from the
// field's reflect.Kind. Re-deriving was a second source of truth that could
// disagree with the validation that produced the value.
func elemAttributeValue(v elemValue) (types.AttributeValue, error) {
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

// renderScalarWildcard renders a wildcard match on a single-valued field.
// PartiQL has no LIKE, so only the patterns that map onto its functions are
// supported:
//
//	field:*        -> field IS NOT MISSING
//	field:value*   -> begins_with(field, ?)
//	field:*value*  -> contains(field, ?)
//
// Anything else (an ends-with match, a wildcard inside the value, or the
// single-character wildcard ?) is an error rather than a query that matches
// the wrong items.
func (d *DynamoDBPartiQLDriver) renderScalarWildcard(name string, right any) (string, []any, error) {
	if info, known := d.fields[name]; known && derefType(info.Type).Kind() != reflect.String {
		return "", nil, fmt.Errorf("wildcard matching is not supported on non-string field '%s'", name)
	}
	ref, err := quotePartiQLPath(name)
	if err != nil {
		return "", nil, err
	}

	pattern := wildcardPattern(right)
	if strings.Contains(pattern, "?") {
		return "", nil, fmt.Errorf("the single-character wildcard ? is not supported on field '%s' in DynamoDB", name)
	}
	leading := strings.HasPrefix(pattern, "*")
	trailing := strings.HasSuffix(pattern, "*")
	value := strings.TrimSuffix(strings.TrimPrefix(pattern, "*"), "*")
	if strings.Contains(value, "*") {
		return "", nil, fmt.Errorf("a wildcard inside a value is not supported on field '%s' in DynamoDB; use value* or *value*", name)
	}

	switch {
	case value == "" && (leading || trailing):
		return fmt.Sprintf("%s IS NOT MISSING", ref), nil, nil
	case leading && trailing:
		return fmt.Sprintf("contains(%s, ?)", ref), []any{value}, nil
	case trailing:
		return fmt.Sprintf("begins_with(%s, ?)", ref), []any{value}, nil
	case leading:
		return "", nil, fmt.Errorf("ends-with matching (*value) is not supported on field '%s' in DynamoDB; use value* or *value*", name)
	default:
		return fmt.Sprintf("%s = ?", ref), []any{value}, nil
	}
}

// wildcardPattern returns the raw pattern of a Wild or Literal value.
func wildcardPattern(v any) string {
	if e, ok := v.(*expr.Expression); ok && (e.Op == expr.Wild || e.Op == expr.Literal) {
		return fmt.Sprintf("%v", e.Left)
	}
	return extractLiteralValue(v)
}

// derefType strips pointer indirection, so *string is treated as string.
func derefType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return reflect.TypeOf("")
	}
	return t
}

// quotePartiQLPath validates a field name and quotes each dot-separated
// segment, so a nested path renders as "base"."sub" rather than as one
// attribute named "base.sub".
func quotePartiQLPath(name string) (string, error) {
	segments := strings.Split(name, ".")
	for i, segment := range segments {
		if !partiQLIdentifierPattern.MatchString(segment) {
			return "", fmt.Errorf("invalid field name '%s'", name)
		}
		segments[i] = `"` + segment + `"`
	}
	return strings.Join(segments, "."), nil
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
