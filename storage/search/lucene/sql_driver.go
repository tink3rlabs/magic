package lucene

import (
	"fmt"
	"reflect"
	"regexp"
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

// validateProvider validates that the provider is one of the supported SQL providers.
func validateProvider(provider string) error {
	switch provider {
	case "postgresql", "mysql", "sqlite":
		return nil
	default:
		return fmt.Errorf("unsupported SQL provider: %s (supported: postgresql, mysql, sqlite)", provider)
	}
}

// NewSQLDriver creates a new SQL driver for the specified provider.
// Provider should be one of: "postgresql", "mysql", "sqlite"
// Returns an error if duplicate field names are found or provider is invalid.
func NewSQLDriver(fields []FieldInfo, provider string) (*SQLDriver, error) {
	if err := validateProvider(provider); err != nil {
		return nil, err
	}

	fieldMap, err := buildFieldMap(fields)
	if err != nil {
		return nil, err
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
	}, nil
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

	// Keep ? placeholders for all providers.
	// GORM's PostgreSQL driver handles ? → $N conversion automatically,
	// so pre-converting here would conflict with additional WHERE clauses
	// (e.g. cursor pagination) that GORM adds with its own ? placeholders.

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
	ref, err := s.resolveField(e.Left)
	if err != nil {
		return "", nil, err
	}
	leftStr := ref.sql
	leftParams := ref.params

	if isNullValue(e.Right) {
		if e.Op == expr.Equals {
			return fmt.Sprintf("%s IS NULL", leftStr), leftParams, nil
		}
		return "", nil, fmt.Errorf("cannot use comparison operators (>, <, >=, <=) with null value")
	}

	// When go-lucene parses grouped OR/AND expressions like field:(a OR b OR null) with a
	// default field set, it produces EQUALS(outer_field, OR(EQUALS(default_field, a), EQUALS(default_field, null))).
	// The inner leaves use the default field, not the outer field. We must re-render each leaf
	// using ref (the correct outer field) to avoid producing wrong SQL like
	// ("id" = ?) OR ("id" IS NULL) when the query was tenant_id:(abc123 OR null).
	if rightExpr, ok := e.Right.(*expr.Expression); ok && e.Op == expr.Equals {
		switch rightExpr.Op {
		case expr.Or, expr.And:
			groupStr, groupParams, err := s.renderGroupedFieldExpr(ref, rightExpr)
			if err != nil {
				return "", nil, err
			}
			return groupStr, append(leftParams, groupParams...), nil
		}
	}

	if ref.isArray() && e.Op == expr.Equals {
		sqlStr, err := s.renderArrayContains(ref)
		if err != nil {
			return "", nil, err
		}
		return sqlStr, append(leftParams, extractLiteralValue(e.Right)), nil
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

// renderGroupedFieldExpr renders an OR/AND expression tree where each leaf comparison
// should use the given field reference instead of whatever field the leaf has internally.
// This handles go-lucene's behavior of wrapping grouped field expressions as
// EQUALS(outer_field, OR(EQUALS(default_field, v1), EQUALS(default_field, v2))).
func (s *SQLDriver) renderGroupedFieldExpr(ref fieldRef, e *expr.Expression) (string, []any, error) {
	leftStr, leftParams, err := s.renderGroupedFieldLeaf(ref, e.Left)
	if err != nil {
		return "", nil, err
	}

	if e.Right == nil {
		return leftStr, leftParams, nil
	}

	rightStr, rightParams, err := s.renderGroupedFieldLeaf(ref, e.Right)
	if err != nil {
		return "", nil, err
	}

	op := " OR "
	if e.Op == expr.And {
		op = " AND "
	}
	return fmt.Sprintf("(%s%s%s)", leftStr, op, rightStr), append(leftParams, rightParams...), nil
}

// renderGroupedFieldLeaf renders a single node (leaf or sub-tree) in a grouped field expression,
// always using ref's column regardless of what the node's own field is.
func (s *SQLDriver) renderGroupedFieldLeaf(ref fieldRef, v any) (string, []any, error) {
	if e, ok := v.(*expr.Expression); ok {
		if e.Op == expr.Or || e.Op == expr.And {
			return s.renderGroupedFieldExpr(ref, e)
		}
		if e.Op == expr.Equals {
			// Use the value from this leaf but with the outer field
			return s.renderGroupedFieldLeaf(ref, e.Right)
		}
	}
	if isNullValue(v) {
		return fmt.Sprintf("%s IS NULL", ref.sql), nil, nil
	}
	valStr, valParams, err := s.serializeValue(v)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("%s = %s", ref.sql, valStr), valParams, nil
}

// renderBinary handles binary and unary logical operators via the shared walker.
// Note: Must and MustNot are unary (only Left operand), while And and Or are binary.
func (s *SQLDriver) renderBinary(e *expr.Expression) (string, []any, error) {
	// Preserves the pre-refactor recovery chain for a non-expression Must/MustNot
	// operand: try it as a column, then as a value, then let the base driver try.
	// And/Or never had this chain — a non-expression operand there went straight
	// to the base driver — so that behaviour is preserved here too.
	//
	// renderLogicalOps treats whatever this closure returns as the final
	// answer (it applies no NOT-wrapping of its own), so — like the
	// pre-refactor code — MustNot must be wrapped here when the column/value
	// chain succeeds. Only the base-driver escape hatch is returned bare,
	// since Base.RenderParam renders the whole node (including any NOT).
	fallback := func(e *expr.Expression) (string, []any, error) {
		if e.Op == expr.Must || e.Op == expr.MustNot {
			str, params, err := s.serializeColumn(e.Left)
			if err != nil {
				str, params, err = s.serializeValue(e.Left)
			}
			if err == nil {
				if e.Op == expr.MustNot {
					return fmt.Sprintf("NOT (%s)", str), params, nil
				}
				return str, params, nil
			}
		}
		return s.Base.RenderParam(e)
	}

	sql, params, ok, err := renderLogicalOps(e, s.renderParamInternal, fallback)
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, fmt.Errorf("unsupported operator: %v", e.Op)
	}
	return sql, params, nil
}

// quoteColumnNameFor quotes a column name using the provider's identifier
// syntax, unless the string is already provider-specific JSON access syntax.
//
// MySQL's default sql_mode does not include ANSI_QUOTES, so a double-quoted
// "col" there is a string LITERAL, not an identifier — the comparison would
// silently run against the constant text instead of the column. MySQL uses
// backticks; Postgres and SQLite use double quotes.
func quoteColumnNameFor(provider, colStr string) string {
	if isJSONSyntax(colStr) {
		return colStr
	}
	if provider == "mysql" {
		return fmt.Sprintf("`%s`", strings.ReplaceAll(colStr, "`", "``"))
	}
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(colStr, `"`, `""`))
}

func (s *SQLDriver) serializeColumn(in any) (string, []any, error) {
	switch v := in.(type) {
	case expr.Column:
		return quoteColumnNameFor(s.provider, string(v)), nil, nil
	case string:
		return quoteColumnNameFor(s.provider, v), nil, nil
	case *expr.Expression:
		if v.Op == expr.Literal && v.Left != nil {
			if col, ok := v.Left.(expr.Column); ok {
				return quoteColumnNameFor(s.provider, string(col)), nil, nil
			}
		}
		return s.renderParamInternal(v)
	default:
		return "", nil, fmt.Errorf("unexpected column type: %T", v)
	}
}

// fieldRef is a column reference resolved back to its model metadata.
//
// Resolution must happen BEFORE quoting: by the time a column has been through
// quoteColumnNameFor it is `"tags"` or `metadata->>'k'`, and the original model
// field name — the key into s.fields — is no longer recoverable.
type fieldRef struct {
	name   string    // original model field name ("" when unresolvable)
	sql    string    // provider-quoted column SQL
	info   FieldInfo // zero value when known is false
	known  bool
	params []any // placeholders bound by sql itself (only for rendered sub-expressions)
}

// isArray reports whether this reference is a multi-valued column.
func (f fieldRef) isArray() bool {
	return f.known && isArrayField(f.info.Type)
}

// resolveField turns an expression's left-hand side into a fieldRef, looking up
// the model metadata before the name is quoted away.
func (s *SQLDriver) resolveField(in any) (fieldRef, error) {
	var raw string

	switch v := in.(type) {
	case expr.Column:
		raw = string(v)
	case string:
		raw = v
	case *expr.Expression:
		if v.Op == expr.Literal && v.Left != nil {
			if col, ok := v.Left.(expr.Column); ok {
				raw = string(col)
			}
		}
		if raw == "" {
			sql, params, err := s.renderParamInternal(v)
			if err != nil {
				return fieldRef{}, err
			}
			return fieldRef{sql: sql, params: params}, nil
		}
	default:
		return fieldRef{}, fmt.Errorf("unexpected column type: %T", in)
	}

	ref := fieldRef{name: raw, sql: quoteColumnNameFor(s.provider, raw)}
	// A dotted name is JSONB nested access; the base field is not the column.
	if !strings.Contains(raw, ".") {
		if info, exists := s.fields[raw]; exists {
			ref.info = info
			ref.known = true
		}
	}
	return ref, nil
}

// arrayElemCast returns the Postgres cast suffix needed so a text-bound
// parameter is compared against a non-text array.
//
// Parameters are always serialized as Go strings (serializeValue stringifies
// via fmt.Sprintf), so `int[] @> ARRAY[$1]` fails with
// "operator does not exist: integer[] @> text[]" without an explicit cast.
//
// The cast must name the column's ACTUAL element type. `@>` requires exactly
// matching array types and neither narrowing nor widening rescues a mismatch:
// `bigint[] @> ARRAY['9999999999']::int[]` errors with "value out of range for
// type integer", `int[] @> ARRAY['5']::bigint[]` errors with "no operator
// matches", and `smallint[] @> ARRAY['1']::int[]` errors the same way. Likewise
// numeric[] does not satisfy a float8[] column.
//
// So each Go element kind maps to the natural Postgres array type for that kind
// — the width that round-trips it exactly — not to a convenient superset. Each
// signed width picks the smallest Postgres integer that holds it; unsigned kinds
// need one more bit and so step up a width.
//
// reflect.Uint8 never reaches here — isArrayField excludes []byte as a scalar
// blob — but it is listed so the width branches are exhaustive.
func arrayElemCast(t reflect.Type) string {
	if t == nil {
		return ""
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Slice && t.Kind() != reflect.Array {
		return ""
	}
	switch t.Elem().Kind() {
	case reflect.String:
		return ""
	case reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint64, reflect.Uint32:
		// Go int is 64-bit; uint32 needs 33 bits, so it too only fits bigint.
		return "::bigint[]"
	case reflect.Int8, reflect.Int16, reflect.Uint8:
		return "::smallint[]"
	case reflect.Int32, reflect.Uint16:
		return "::int[]"
	case reflect.Float64:
		return "::double precision[]"
	case reflect.Float32:
		return "::real[]"
	case reflect.Bool:
		return "::boolean[]"
	default:
		return ""
	}
}

// renderArrayContains renders "does this array column contain the bound value",
// with one ? placeholder.
//
// Wrapped in COALESCE(..., false) so NOT field:value matches rows whose column
// is NULL. Without it, NOT(NULL) is NULL and those rows vanish silently.
func (s *SQLDriver) renderArrayContains(f fieldRef) (string, error) {
	switch s.provider {
	case "postgresql":
		cast := arrayElemCast(f.info.Type)
		return fmt.Sprintf("COALESCE(%s @> ARRAY[?]%s, false)", f.sql, cast), nil
	case "mysql":
		return fmt.Sprintf("COALESCE(JSON_CONTAINS(%s, JSON_QUOTE(?)), false)", f.sql), nil
	case "sqlite":
		return fmt.Sprintf("EXISTS (SELECT 1 FROM json_each(%s) WHERE value = ?)", f.sql), nil
	default:
		return "", fmt.Errorf("unsupported SQL provider: %s", s.provider)
	}
}

// extractLiteralString extracts a string value from an expression for wildcard conversion.
func extractLiteralString(v *expr.Expression) (string, bool) {
	if v.Left == nil {
		return "", false
	}
	if v.Op == expr.Literal || v.Op == expr.Wild {
		return fmt.Sprintf("%v", v.Left), true
	}
	return "", false
}

// serializeValue converts Lucene wildcards (* and ?) to SQL wildcards (% and _).
func (s *SQLDriver) serializeValue(in any) (string, []any, error) {
	switch v := in.(type) {
	case string:
		return "?", []any{convertWildcards(v)}, nil
	case *expr.Expression:
		if literalVal, ok := extractLiteralString(v); ok {
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

var (
	// jsonSubFieldPattern matches valid JSON subfield names (alphanumeric, underscore, and dot for nested paths)
	jsonSubFieldPattern = regexp.MustCompile(`^[a-zA-Z0-9_.]+$`)
)

// validateSubFieldName validates that a subfield name contains only safe characters.
// Subfield names should be alphanumeric with underscores and dots for nested paths.
// This prevents injection attacks via JSON path manipulation.
func validateSubFieldName(subField string) error {
	if subField == "" {
		return fmt.Errorf("subfield name cannot be empty")
	}

	if !jsonSubFieldPattern.MatchString(subField) {
		return fmt.Errorf("invalid subfield name '%s': contains unsafe characters (only alphanumeric, underscore, and dot allowed)", subField)
	}
	return nil
}

// escapeJSONPathSegment escapes a JSON path segment for safe use in JSON path expressions.
// For PostgreSQL: escapes single quotes in the key name (used in ->>'key' syntax)
// For MySQL/SQLite: escapes special characters in JSON path (though validation should prevent most)
func escapeJSONPathSegment(segment string) string {
	// Replace single quote with escaped single quote (for PostgreSQL ->>'key' syntax)
	result := strings.ReplaceAll(segment, "'", "''")
	return result
}

// formatFieldName converts field.subfield to provider-specific JSON syntax.
// Validates and escapes subfield names to prevent injection attacks.
func (s *SQLDriver) formatFieldName(fieldName string) expr.Column {
	parts := strings.SplitN(fieldName, ".", 2)
	if len(parts) == 2 {
		baseField := parts[0]
		subField := parts[1]

		// Validate subfield name for security (prevents injection)
		if err := validateSubFieldName(subField); err != nil {
			// If validation fails, return original field name (will be caught by field validation)
			return expr.Column(fieldName)
		}

		if field, exists := s.fields[baseField]; exists && canUseNestedAccess(field.Type) {
			// Escape subfield name for safe interpolation
			// PostgreSQL uses ->>'key' syntax where key is in quotes, so we need to escape quotes
			escapedSubField := escapeJSONPathSegment(subField)

			switch s.provider {
			case "postgresql":
				// PostgreSQL: JSONB operator ->>
				// Key is in single quotes, so we escape single quotes
				return expr.Column(fmt.Sprintf("%s->>'%s'", baseField, escapedSubField))

			case "mysql":
				// MySQL 5.7+: JSON_UNQUOTE(JSON_EXTRACT(column, '$.field'))
				// Path is '$.field' - field name is not separately quoted, but validation ensures it's safe
				return expr.Column(fmt.Sprintf("JSON_UNQUOTE(JSON_EXTRACT(%s, '$.%s'))", baseField, subField))

			case "sqlite":
				// SQLite: JSON_EXTRACT(column, '$.field')
				// Path is '$.field' - field name is not separately quoted, but validation ensures it's safe
				return expr.Column(fmt.Sprintf("JSON_EXTRACT(%s, '$.%s')", baseField, subField))

			default:
				// Should never happen due to validateProvider, but defensive programming
				return expr.Column(fieldName)
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

// isNullValue reports whether v is the bare `null` keyword, which go-lucene
// parses into a typed Null expression node (Op == expr.Null). A Go nil also
// counts as null. The quoted form `field:"null"` parses to a string literal
// instead and is intentionally NOT treated as null — it's a search for the
// text "null".
func isNullValue(v any) bool {
	if v == nil {
		return true
	}
	e, ok := v.(*expr.Expression)
	return ok && e.Op == expr.Null
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
			fmt.Fprintf(&result, "$%d", paramIndex)
			paramIndex++
		} else {
			result.WriteByte(query[i])
		}
	}
	return result.String()
}
