package lucene

import (
	"fmt"

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
			name, err := d.fieldName(eq.Left)
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
	name, err := d.fieldName(e.Left)
	if err != nil {
		return nil, err
	}
	if isNullValue(e.Right) {
		return esBool("must_not", esExists(name)), nil
	}
	if group, ok := e.Right.(*expr.Expression); ok && isGroup(group) {
		return d.render(onField(name, group))
	}
	v, err := normalizeScalarValue(name, d.fields[name].Type, extractLiteralValue(e.Right))
	if err != nil {
		return nil, err
	}
	return esTerm(name, v.Val), nil
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

// fieldName resolves the field a leaf refers to. Validation has already
// rejected unknown fields, so a miss here means an unexpected tree shape.
func (d *ElasticsearchDriver) fieldName(v any) (string, error) {
	name, ok := columnName(v)
	if !ok {
		return "", fmt.Errorf("unexpected field reference %T in query", v)
	}
	if _, ok := d.fields[name]; !ok {
		return "", fmt.Errorf("unknown field '%s' in query", name)
	}
	return name, nil
}

func esTerm(field string, value any) map[string]any {
	return map[string]any{"term": map[string]any{field: map[string]any{"value": value}}}
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
