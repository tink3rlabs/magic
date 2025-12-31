package lucene

import (
	"fmt"
	"strings"

	"github.com/grindlemire/go-lucene/pkg/driver"
	"github.com/grindlemire/go-lucene/pkg/lucene/expr"
)

// SQLDriver is a SQL driver that supports multiple SQL dialects (PostgreSQL, MySQL, SQLite).
// It handles database-specific syntax for LIKE operators, JSON field access, and parameter placeholders.
type SQLDriver struct {
	driver.Base
	fields   map[string]FieldInfo // Map of field names to their metadata
	provider string               // SQL provider: "postgresql", "mysql", or "sqlite"
}

// NewSQLDriver creates a new SQL driver for the specified provider.
// Provider should be one of: "postgresql", "mysql", "sqlite"
func NewSQLDriver(fields []FieldInfo, provider string) *SQLDriver {
	fieldMap := make(map[string]FieldInfo)
	for _, f := range fields {
		fieldMap[f.Name] = f
	}

	// RenderFNs map - we handle most operators in renderParamInternal
	// Only keeping base implementations for operators we don't intercept
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
		expr.Like:      driver.Shared[expr.Like],
		expr.Greater:   driver.Shared[expr.Greater],
		expr.GreaterEq: driver.Shared[expr.GreaterEq],
		expr.Less:      driver.Shared[expr.Less],
		expr.LessEq:    driver.Shared[expr.LessEq],
		expr.In:        driver.Shared[expr.In],
		expr.List:      driver.Shared[expr.List],
	}

	return &SQLDriver{
		Base: driver.Base{
			RenderFNs: fns,
		},
		fields:   fieldMap,
		provider: provider,
	}
}

// RenderParam renders the expression with provider-specific parameter placeholders.
func (s *SQLDriver) RenderParam(e *expr.Expression) (string, []any, error) {
	// Process JSON field notation before rendering
	s.processJSONFields(e)

	// Use our custom rendering logic
	str, params, err := s.renderParamInternal(e)
	if err != nil {
		return "", nil, err
	}

	// Convert ? placeholders to provider-specific format
	// PostgreSQL uses $1, $2, $3; MySQL and SQLite use ?
	switch s.provider {
	case "postgresql":
		str = convertToPostgresPlaceholders(str)
	case "mysql", "sqlite":
		// Already uses ? placeholders, no conversion needed
	}

	return str, params, nil
}

// renderParamInternal dispatches to specialized renderers based on operator type.
func (s *SQLDriver) renderParamInternal(e *expr.Expression) (string, []any, error) {
	if e == nil {
		return "", nil, nil
	}

	switch e.Op {
	case expr.Like, expr.Wild:
		return s.renderLikeOrWild(e)
	case expr.Fuzzy:
		return s.renderFuzzy(e)
	case expr.Boost:
		return "", nil, fmt.Errorf("boost operator (^) is not supported in SQL filtering; it only affects ranking/scoring")
	case expr.Range:
		return s.renderRange(e)
	case expr.Equals, expr.Greater, expr.Less, expr.GreaterEq, expr.LessEq:
		return s.renderComparison(e)
	case expr.And, expr.Or, expr.Must, expr.MustNot:
		return s.renderBinary(e)
	default:
		// Use base implementation for all other operators
		return s.Base.RenderParam(e)
	}
}

// renderLikeOrWild converts LIKE and Wild operators to provider-specific case-insensitive matching.
func (s *SQLDriver) renderLikeOrWild(e *expr.Expression) (string, []any, error) {
	leftStr, leftParams, err := s.serializeColumn(e.Left)
	if err != nil {
		return "", nil, err
	}

	rightStr, rightParams, err := s.serializeValue(e.Right)
	if err != nil {
		return "", nil, err
	}

	params := append(leftParams, rightParams...)

	switch s.provider {
	case "postgresql":
		// PostgreSQL: ILIKE for case-insensitive matching
		if isJSONSyntax(leftStr) {
			return fmt.Sprintf("%s ILIKE %s", leftStr, rightStr), params, nil
		}
		return fmt.Sprintf("%s::text ILIKE %s", leftStr, rightStr), params, nil

	case "mysql":
		// MySQL: Use LOWER() for case-insensitive matching
		return fmt.Sprintf("LOWER(%s) LIKE LOWER(%s)", leftStr, rightStr), params, nil

	case "sqlite":
		// SQLite: LIKE is already case-insensitive for ASCII by default
		return fmt.Sprintf("%s LIKE %s", leftStr, rightStr), params, nil

	default:
		return "", nil, fmt.Errorf("unsupported SQL provider: %s", s.provider)
	}
}

// renderFuzzy handles fuzzy search with provider-specific implementations.
// For queries like "name:roam~2", the structure is:
// - Op: Fuzzy
// - Left: Equals expression (name:roam) with Left=Column("name"), Right=Literal("roam")
// - Right: nil (distance stored in unexported fuzzyDistance field)
func (s *SQLDriver) renderFuzzy(e *expr.Expression) (string, []any, error) {
	leftExpr, ok := e.Left.(*expr.Expression)
	if !ok || leftExpr.Op != expr.Equals {
		return "", nil, fmt.Errorf("fuzzy operator requires field:value syntax (e.g., name:roam~2)")
	}

	colStr, colParams, err := s.serializeColumn(leftExpr.Left)
	if err != nil {
		return "", nil, err
	}

	termStr, termParams, err := s.serializeValue(leftExpr.Right)
	if err != nil {
		return "", nil, err
	}

	params := append(colParams, termParams...)

	switch s.provider {
	case "postgresql":
		// PostgreSQL: Use similarity() function from pg_trgm extension
		// Threshold 0.3 (lower = more matches, higher = stricter)
		threshold := 0.3
		if isJSONSyntax(colStr) {
			return fmt.Sprintf("similarity(%s, %s) > %f", colStr, termStr, threshold), params, nil
		}
		return fmt.Sprintf("similarity(%s::text, %s) > %f", colStr, termStr, threshold), params, nil

	case "mysql":
		// MySQL: Use SOUNDEX for phonetic matching (limited fuzzy support)
		return fmt.Sprintf("SOUNDEX(%s) = SOUNDEX(%s)", colStr, termStr), params, nil

	case "sqlite":
		// SQLite: No built-in fuzzy search support
		return "", nil, fmt.Errorf("fuzzy search (field:term~N) is not supported with SQLite; use wildcards instead (e.g., field:term*)")

	default:
		return "", nil, fmt.Errorf("unsupported SQL provider: %s", s.provider)
	}
}

// renderComparison handles comparison operators with IS NULL support for null values.
func (s *SQLDriver) renderComparison(e *expr.Expression) (string, []any, error) {
	leftStr, leftParams, err := s.serializeColumn(e.Left)
	if err != nil {
		return "", nil, err
	}

	if isNullValue(e.Right) {
		if e.Op == expr.Equals {
			return fmt.Sprintf("%s IS NULL", leftStr), leftParams, nil
		}
		return "", nil, fmt.Errorf("cannot use comparison operators (>, <, >=, <=) with null value")
	}

	rightStr, rightParams, err := s.serializeValue(e.Right)
	if err != nil {
		return "", nil, err
	}

	params := append(leftParams, rightParams...)

	var opSymbol string
	switch e.Op {
	case expr.Equals:
		opSymbol = "="
	case expr.Greater:
		opSymbol = ">"
	case expr.Less:
		opSymbol = "<"
	case expr.GreaterEq:
		opSymbol = ">="
	case expr.LessEq:
		opSymbol = "<="
	}

	return fmt.Sprintf("%s %s %s", leftStr, opSymbol, rightStr), params, nil
}

// renderBinary handles binary and unary logical operators recursively.
// Note: Must and MustNot are unary (only Left operand), while And and Or are binary.
func (s *SQLDriver) renderBinary(e *expr.Expression) (string, []any, error) {
	switch e.Op {
	case expr.Must, expr.MustNot:
		if e.Left == nil {
			return "", nil, fmt.Errorf("%s operator requires a left operand", e.Op)
		}

		if leftExpr, ok := e.Left.(*expr.Expression); ok {
			leftStr, leftParams, err := s.renderParamInternal(leftExpr)
			if err != nil {
				return "", nil, err
			}

			if e.Op == expr.Must {
				return leftStr, leftParams, nil
			}
			return fmt.Sprintf("NOT (%s)", leftStr), leftParams, nil
		}

		leftStr, leftParams, err := s.serializeColumn(e.Left)
		if err != nil {
			leftStr, leftParams, err = s.serializeValue(e.Left)
			if err != nil {
				return s.Base.RenderParam(e)
			}
		}

		if e.Op == expr.Must {
			return leftStr, leftParams, nil
		}
		return fmt.Sprintf("NOT (%s)", leftStr), leftParams, nil

	case expr.And, expr.Or:
		if e.Left == nil || e.Right == nil {
			return "", nil, fmt.Errorf("%s operator requires both left and right operands", e.Op)
		}

		leftExpr, leftIsExpr := e.Left.(*expr.Expression)
		rightExpr, rightIsExpr := e.Right.(*expr.Expression)

		if !leftIsExpr || !rightIsExpr {
			return s.Base.RenderParam(e)
		}

		leftStr, leftParams, err := s.renderParamInternal(leftExpr)
		if err != nil {
			return "", nil, err
		}

		rightStr, rightParams, err := s.renderParamInternal(rightExpr)
		if err != nil {
			return "", nil, err
		}

		params := append(leftParams, rightParams...)

		if e.Op == expr.And {
			return fmt.Sprintf("(%s) AND (%s)", leftStr, rightStr), params, nil
		}
		return fmt.Sprintf("(%s) OR (%s)", leftStr, rightStr), params, nil

	default:
		return "", nil, fmt.Errorf("unsupported operator: %v", e.Op)
	}
}

func (s *SQLDriver) serializeColumn(in any) (string, []any, error) {
	switch v := in.(type) {
	case expr.Column:
		colStr := string(v)
		if isJSONSyntax(colStr) {
			return colStr, nil, nil
		}
		return fmt.Sprintf(`"%s"`, colStr), nil, nil
	case string:
		if isJSONSyntax(v) {
			return v, nil, nil
		}
		return fmt.Sprintf(`"%s"`, v), nil, nil
	case *expr.Expression:
		if v.Op == expr.Literal && v.Left != nil {
			if col, ok := v.Left.(expr.Column); ok {
				colStr := string(col)
				if isJSONSyntax(colStr) {
					return colStr, nil, nil
				}
				return fmt.Sprintf(`"%s"`, colStr), nil, nil
			}
		}
		return s.renderParamInternal(v)
	default:
		return "", nil, fmt.Errorf("unexpected column type: %T", v)
	}
}

// serializeValue converts Lucene wildcards (* and ?) to SQL wildcards (% and _).
func (s *SQLDriver) serializeValue(in any) (string, []any, error) {
	switch v := in.(type) {
	case string:
		return "?", []any{convertWildcards(v)}, nil
	case *expr.Expression:
		if v.Op == expr.Literal && v.Left != nil {
			literalVal := fmt.Sprintf("%v", v.Left)
			return "?", []any{convertWildcards(literalVal)}, nil
		}
		if v.Op == expr.Wild && v.Left != nil {
			literalVal := fmt.Sprintf("%v", v.Left)
			return "?", []any{convertWildcards(literalVal)}, nil
		}
		return s.renderParamInternal(v)
	case nil:
		return "", nil, fmt.Errorf("nil value in expression")
	default:
		return "?", []any{v}, nil
	}
}

// processJSONFields recursively processes the expression tree to convert
// field.subfield notation to provider-specific JSON syntax.
func (s *SQLDriver) processJSONFields(e *expr.Expression) {
	if e == nil {
		return
	}

	// Process left side if it's a column
	if col, ok := e.Left.(expr.Column); ok {
		e.Left = s.formatFieldName(string(col))
	}

	// Recursively process expressions
	if leftExpr, ok := e.Left.(*expr.Expression); ok {
		s.processJSONFields(leftExpr)
	}
	if rightExpr, ok := e.Right.(*expr.Expression); ok {
		s.processJSONFields(rightExpr)
	}

	// Process expression slices
	if exprs, ok := e.Left.([]*expr.Expression); ok {
		for _, ex := range exprs {
			s.processJSONFields(ex)
		}
	}
	if exprs, ok := e.Right.([]*expr.Expression); ok {
		for _, ex := range exprs {
			s.processJSONFields(ex)
		}
	}
}

// formatFieldName converts field.subfield to provider-specific JSON syntax.
func (s *SQLDriver) formatFieldName(fieldName string) expr.Column {
	parts := strings.SplitN(fieldName, ".", 2)
	if len(parts) == 2 {
		baseField := parts[0]
		subField := parts[1]

		if field, exists := s.fields[baseField]; exists && canUseNestedAccess(field.Type) {
			switch s.provider {
			case "postgresql":
				// PostgreSQL: JSONB operator ->>
				return expr.Column(fmt.Sprintf("%s->>'%s'", baseField, subField))

			case "mysql":
				// MySQL 5.7+: JSON_UNQUOTE(JSON_EXTRACT(column, '$.field'))
				return expr.Column(fmt.Sprintf("JSON_UNQUOTE(JSON_EXTRACT(%s, '$.%s'))", baseField, subField))

			case "sqlite":
				// SQLite: JSON_EXTRACT(column, '$.field')
				return expr.Column(fmt.Sprintf("JSON_EXTRACT(%s, '$.%s')", baseField, subField))
			}
		}
	}
	return expr.Column(fieldName)
}

// Helper functions for SQL driver

// convertWildcards converts Lucene wildcards to SQL wildcards.
// * (any characters) → % (SQL wildcard)
// ? (single character) → _ (SQL wildcard)
//
// Note: go-lucene's base driver also converts wildcards, but only for expr.Like operators.
// We need this function because we also convert wildcards for expr.Wild expressions
// and when serializing values for fuzzy search and other operators.
func convertWildcards(s string) string {
	// Use a builder for efficient string manipulation
	var result strings.Builder
	result.Grow(len(s))

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '*':
			result.WriteByte('%')
		case '?':
			result.WriteByte('_')
		default:
			result.WriteByte(c)
		}
	}
	return result.String()
}

// isJSONSyntax checks if a column string contains provider-specific JSON syntax.
func isJSONSyntax(col string) bool {
	// Check for PostgreSQL JSONB operator
	if strings.Contains(col, "->>") {
		return true
	}
	// Check for MySQL/SQLite JSON_EXTRACT
	if strings.Contains(col, "JSON_EXTRACT") || strings.Contains(col, "JSON_UNQUOTE") {
		return true
	}
	return false
}

// isNullValue checks if a value represents null in Lucene query syntax.
// Supports: null, NULL, Null (case-insensitive)
// Note: This is a SQL-specific extension (vanilla Lucene doesn't support NULL values).
// We intentionally do NOT support "empty" or "nil" as they could be legitimate search values.
func isNullValue(v any) bool {
	strVal := extractStringValue(v)
	if strVal == "" {
		return false
	}
	lower := strings.ToLower(strVal)
	return lower == "null"
}

func extractStringValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case *expr.Expression:
		if val.Op == expr.Literal && val.Left != nil {
			if strVal, ok := val.Left.(string); ok {
				return strVal
			}
		}
	}
	return ""
}

func extractLiteralValue(v any) string {
	if v == nil {
		return ""
	}

	// If it's an expression, try to extract the Left value (for LITERAL expressions)
	if ex, ok := v.(*expr.Expression); ok {
		if ex.Op == expr.Literal && ex.Left != nil {
			// LITERAL expressions store the actual value in Left
			return fmt.Sprintf("%v", ex.Left)
		}
		// For other expression types, return the string representation
		return fmt.Sprintf("%v", v)
	}

	// For non-expression types, return as string
	return fmt.Sprintf("%v", v)
}

// renderRange handles range queries including open-ended ranges with wildcards (*).
func (s *SQLDriver) renderRange(e *expr.Expression) (string, []any, error) {
	colStr, _, err := s.serializeColumn(e.Left)
	if err != nil {
		return "", nil, err
	}

	rangeBoundary, ok := e.Right.(*expr.RangeBoundary)
	if !ok {
		return "", nil, fmt.Errorf("invalid range expression structure: expected *expr.RangeBoundary, got %T", e.Right)
	}

	var minVal, maxVal string
	var params []any

	if rangeBoundary.Min != nil {
		minVal = extractLiteralValue(rangeBoundary.Min)
	}

	if rangeBoundary.Max != nil {
		maxVal = extractLiteralValue(rangeBoundary.Max)
	}

	if minVal == "*" && maxVal == "*" {
		return "", nil, fmt.Errorf("both range bounds cannot be wildcards")
	}

	if minVal == "*" {
		params = append(params, maxVal)
		if rangeBoundary.Inclusive {
			return fmt.Sprintf("%s <= ?", colStr), params, nil
		}
		return fmt.Sprintf("%s < ?", colStr), params, nil
	}

	if maxVal == "*" {
		params = append(params, minVal)
		if rangeBoundary.Inclusive {
			return fmt.Sprintf("%s >= ?", colStr), params, nil
		}
		return fmt.Sprintf("%s > ?", colStr), params, nil
	}

	params = append(params, minVal, maxVal)
	if rangeBoundary.Inclusive {
		return fmt.Sprintf("%s BETWEEN ? AND ?", colStr), params, nil
	}
	return fmt.Sprintf("(%s > ? AND %s < ?)", colStr, colStr), params, nil
}

// convertToPostgresPlaceholders converts ? placeholders to PostgreSQL's $N format.
func convertToPostgresPlaceholders(query string) string {
	paramIndex := 1
	result := strings.Builder{}
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			result.WriteString(fmt.Sprintf("$%d", paramIndex))
			paramIndex++
		} else {
			result.WriteByte(query[i])
		}
	}
	return result.String()
}
