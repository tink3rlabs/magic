package lucene

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/grindlemire/go-lucene/pkg/lucene/expr"
)

// ElasticsearchDriver converts Lucene queries to Elasticsearch Query DSL.
//
// The output is structured data: user text only ever becomes the value of a
// term, wildcard, range or fuzzy clause, never part of a query string.
type ElasticsearchDriver struct {
	fields map[string]FieldInfo
}

func NewElasticsearchDriver(fields []FieldInfo) (*ElasticsearchDriver, error) {
	fieldMap, err := buildFieldMap(fields)
	if err != nil {
		return nil, err
	}
	return &ElasticsearchDriver{fields: fieldMap}, nil
}

// Render converts e to a Query DSL object. A nil expression (an empty query)
// matches every document, as an empty SQL WHERE clause does.
func (d *ElasticsearchDriver) Render(e *expr.Expression) (map[string]any, error) {
	if e == nil {
		return map[string]any{"match_all": map[string]any{}}, nil
	}
	return d.render(e)
}

func (d *ElasticsearchDriver) render(e *expr.Expression) (map[string]any, error) {
	switch e.Op {
	case expr.And, expr.Or:
		left, err := d.renderChild(e, e.Left)
		if err != nil {
			return nil, err
		}
		right, err := d.renderChild(e, e.Right)
		if err != nil {
			return nil, err
		}
		if e.Op == expr.And {
			return esBool("filter", left, right), nil
		}
		return esShould(left, right), nil
	case expr.Not, expr.MustNot:
		// NOT field:null is "field has a value", not a double negation.
		if eq, ok := nullEqualsOperand(e.Left); ok {
			name, _, err := d.field(eq.Left)
			if err != nil {
				return nil, err
			}
			return esExists(name), nil
		}
		child, err := d.renderChild(e, e.Left)
		if err != nil {
			return nil, err
		}
		return esBool("must_not", child), nil
	case expr.Must:
		return d.renderChild(e, e.Left)
	case expr.Equals:
		return d.renderEquals(e)
	case expr.Like, expr.Wild:
		return d.renderWildcard(e)
	case expr.Range:
		return d.renderRange(e)
	case expr.Greater, expr.GreaterEq, expr.Less, expr.LessEq:
		return d.renderComparison(e)
	}
	return nil, fmt.Errorf("operator %s is not supported for Elasticsearch", e.Op)
}

func (d *ElasticsearchDriver) renderChild(parent *expr.Expression, child any) (map[string]any, error) {
	e, ok := child.(*expr.Expression)
	if !ok || e == nil {
		return nil, fmt.Errorf("%s operator requires an expression operand, got %T", parent.Op, child)
	}
	return d.render(e)
}

func (d *ElasticsearchDriver) renderEquals(e *expr.Expression) (map[string]any, error) {
	name, t, err := d.field(e.Left)
	if err != nil {
		return nil, err
	}
	if isNullValue(e.Right) {
		return esBool("must_not", esExists(name)), nil
	}
	if group, ok := e.Right.(*expr.Expression); ok && isGroup(group) {
		return d.render(onField(name, group))
	}
	v, err := value(name, t, extractLiteralValue(e.Right))
	if err != nil {
		return nil, err
	}
	return esTerm(name, v), nil
}

// renderWildcard renders field:pattern. A bare * means "has a value", which
// is also the only wildcard a non-string field accepts.
func (d *ElasticsearchDriver) renderWildcard(e *expr.Expression) (map[string]any, error) {
	name, t, err := d.field(e.Left)
	if err != nil {
		return nil, err
	}
	if r, ok := e.Right.(*expr.Expression); ok && r.Op == expr.Regexp {
		return nil, fmt.Errorf("regexp queries (field:/pattern/) are not supported; use wildcards instead (for example %s:ro?m*), and quote a value that contains / (for example %s:\"a/b\")", name, name)
	}
	pattern := extractLiteralValue(e.Right)
	if pattern == "*" {
		return esExists(name), nil
	}
	if isArrayField(t) {
		if !isStringArray(t) {
			return nil, fmt.Errorf("wildcard matching is not supported on non-string array field '%s'; use containment (%s:value)", name, name)
		}
	} else if !acceptsWildcard(t) {
		return nil, fmt.Errorf("wildcard matching is not supported on non-string field '%s'; use %s:value or a range", name, name)
	}
	return map[string]any{"wildcard": map[string]any{
		name: map[string]any{"value": pattern, "case_insensitive": true},
	}}, nil
}

var esComparisons = map[expr.Operator]string{
	expr.Greater:   "gt",
	expr.GreaterEq: "gte",
	expr.Less:      "lt",
	expr.LessEq:    "lte",
}

// renderComparison renders field:>v, field:>=v, field:<v and field:<=v.
func (d *ElasticsearchDriver) renderComparison(e *expr.Expression) (map[string]any, error) {
	name, t, err := d.field(e.Left)
	if err != nil {
		return nil, err
	}
	if isArrayField(t) {
		return nil, fmt.Errorf("operator %s is not supported on array field '%s'; array fields support containment (%s:value) and null checks", e.Op, name, name)
	}
	v, err := value(name, t, extractLiteralValue(e.Right))
	if err != nil {
		return nil, err
	}
	return esRange(name, map[string]any{esComparisons[e.Op]: v}), nil
}

// renderRange renders field:[a TO b] (inclusive) and field:{a TO b}
// (exclusive). A * bound is open and leaves that side out.
func (d *ElasticsearchDriver) renderRange(e *expr.Expression) (map[string]any, error) {
	name, t, err := d.field(e.Left)
	if err != nil {
		return nil, err
	}
	if isArrayField(t) {
		return nil, fmt.Errorf("range queries are not supported on array field '%s'; array fields support containment (%s:value) and null checks", name, name)
	}
	bounds, ok := e.Right.(*expr.RangeBoundary)
	if !ok {
		return nil, fmt.Errorf("invalid range expression structure: expected *expr.RangeBoundary, got %T", e.Right)
	}
	lower, upper := "gt", "lt"
	if bounds.Inclusive {
		lower, upper = "gte", "lte"
	}
	r := map[string]any{}
	for key, bound := range map[string]any{lower: bounds.Min, upper: bounds.Max} {
		raw := "*"
		if bound != nil {
			raw = extractLiteralValue(bound)
		}
		if raw == "*" {
			continue
		}
		v, err := value(name, t, raw)
		if err != nil {
			return nil, err
		}
		r[key] = v
	}
	if len(r) == 0 {
		return nil, fmt.Errorf("both range bounds cannot be wildcards")
	}
	return esRange(name, r), nil
}

// acceptsWildcard reports whether a field of type t is indexed as text that a
// wildcard can match. Elasticsearch refuses a wildcard on a numeric, boolean
// or date field with a server error. A uuid or other string-like value is a
// keyword and matches.
func acceptsWildcard(t reflect.Type) bool {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return true
	}
	if t == reflect.TypeOf(time.Time{}) {
		return false
	}
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return false
	}
	return true
}

// onField returns a copy of the grouped value list v with each value bound to
// field name. go-lucene parses field:(a OR b) as EQUALS(field, OR(a, b)), and
// a value inside the group may carry the parser's default field instead of
// the outer one. Each rebound leaf then renders like field:a.
func onField(name string, v any) *expr.Expression {
	e, ok := v.(*expr.Expression)
	if !ok || e.Op == expr.Literal || e.Op == expr.Null {
		return &expr.Expression{Op: expr.Equals, Left: expr.Column(name), Right: v}
	}
	c := *e
	switch e.Op {
	case expr.And, expr.Or:
		c.Left = onField(name, e.Left)
		c.Right = onField(name, e.Right)
	case expr.Not, expr.MustNot, expr.Must, expr.Fuzzy, expr.Boost:
		c.Left = onField(name, e.Left)
	default:
		if isGroup(e.Right) {
			return onField(name, e.Right)
		}
		c.Left = expr.Column(name)
	}
	return &c
}

// field resolves the field a leaf refers to and its Go type. A JSON
// sub-field (meta.k) has no Go type: its values stay strings. Validation has
// already rejected unknown fields, so a miss here means an unexpected tree
// shape.
func (d *ElasticsearchDriver) field(v any) (string, reflect.Type, error) {
	name, ok := columnName(v)
	if !ok {
		return "", nil, fmt.Errorf("unexpected field reference %T in query", v)
	}
	if info, ok := d.fields[name]; ok {
		return name, info.Type, nil
	}
	if base, _, ok := strings.Cut(name, "."); ok {
		if info, ok := d.fields[base]; ok && canUseNestedAccess(info.Type) {
			return name, nil, nil
		}
	}
	return "", nil, fmt.Errorf("unknown field '%s' in query", name)
}

// value types raw against a field: an array field checks it against the
// element type, a single-valued field against its own type.
func value(name string, t reflect.Type, raw string) (any, error) {
	normalize := normalizeScalarValue
	if isArrayField(t) {
		normalize = normalizeArrayElemValue
	}
	v, err := normalize(name, t, raw)
	return v.Val, err
}

func esTerm(field string, value any) map[string]any {
	return map[string]any{"term": map[string]any{field: map[string]any{"value": value}}}
}

func esRange(field string, bounds map[string]any) map[string]any {
	return map[string]any{"range": map[string]any{field: bounds}}
}

func esExists(field string) map[string]any {
	return map[string]any{"exists": map[string]any{"field": field}}
}

func esBool(occur string, clauses ...map[string]any) map[string]any {
	return map[string]any{"bool": map[string]any{occur: clauses}}
}

func esShould(clauses ...map[string]any) map[string]any {
	return map[string]any{"bool": map[string]any{"should": clauses, "minimum_should_match": 1}}
}
