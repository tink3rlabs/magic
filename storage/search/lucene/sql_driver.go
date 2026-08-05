package lucene

import (
	"fmt"
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
	provider string               // SQL provider name, as given by the caller
	dialect  sqlDialect           // Per-database rendering, resolved from provider
}

// NewSQLDriver creates a new SQL driver for the specified provider.
//
// Provider must be one of: "postgresql", "mysql", "sqlite". That list is the
// set of registered dialects (see dialect.go); the error returned for an
// unknown provider names the supported ones, so it stays accurate if the set
// changes.
//
// Returns an error if duplicate field names are found or the provider is unknown.
func NewSQLDriver(fields []FieldInfo, provider string) (*SQLDriver, error) {
	dialect, err := lookupDialect(provider)
	if err != nil {
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
		dialect:  dialect,
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
	case expr.And, expr.Or, expr.Must, expr.MustNot, expr.Not:
		return s.renderBinary(e)
	default:
		// Use base implementation for all other operators
		return s.Base.RenderParam(e)
	}
}

// renderLikeOrWild converts LIKE and Wild operators to provider-specific case-insensitive matching.
func (s *SQLDriver) renderLikeOrWild(e *expr.Expression) (string, []any, error) {
	ref, err := s.resolveField(e.Left)
	if err != nil {
		return "", nil, err
	}
	leftStr := ref.sql
	leftParams := ref.params

	rightStr, rightParams, err := s.serializeValue(e.Right)
	if err != nil {
		return "", nil, err
	}

	params := append(leftParams, rightParams...)

	if ref.isArray() {
		return s.renderArrayWildcard(ref, isBareWildcard(e.Right), params)
	}

	sqlStr, err := s.renderScalarLike(leftStr, rightStr)
	if err != nil {
		return "", nil, err
	}
	return sqlStr, params, nil
}

// renderScalarLike renders provider-specific case-insensitive pattern matching
// for a single-valued column.
func (s *SQLDriver) renderScalarLike(leftStr, rightStr string) (string, error) {
	return s.dialect.ScalarLike(leftStr, rightStr), nil
}

// renderArrayWildcard renders a wildcard match against a multi-valued column.
//
// Two distinct forms:
//
//	field:*     "has a value"  -> IS NOT NULL (whole column)
//	field:*go*  pattern match   -> per element
//
// Per-element matching is required for correctness: casting the whole array to
// text and substring-matching lets a pattern span the separator between two
// elements, so {alpha,beta} wrongly matches '%ha,be%'.
//
// The bare-star form stays whole-column so it keeps matching rows holding an
// empty array, which the per-element form would silently drop. This is a
// deliberate divergence from Elasticsearch's exists query.
func (s *SQLDriver) renderArrayWildcard(ref fieldRef, bare bool, params []any) (string, []any, error) {
	if bare {
		// The pattern param is deliberately dropped: this form tests the whole
		// column, not a pattern. ref.params is still returned so a rendered
		// sub-expression column keeps its own placeholders bound.
		return fmt.Sprintf("%s IS NOT NULL", ref.sql), ref.params, nil
	}

	// A wildcard is substring matching, which only means something for string
	// elements. On a numeric or boolean array every provider either errors
	// (Postgres: "operator does not exist: integer ~~* unknown") or silently
	// matches nothing (MySQL JSON_SEARCH only searches string scalars), so
	// reject it here and return a filter error rather than a database one.
	if !isStringArray(ref.info.Type) {
		return "", nil, fmt.Errorf(
			"wildcard matching is not supported on non-string array field '%s'; use containment (%s:value)",
			ref.name, ref.name,
		)
	}

	return s.dialect.ArrayWildcard(ref.sql), params, nil
}

// isBareWildcard reports whether a value is exactly "*" — the documented
// "field has a value" form, as opposed to a pattern like "*go*".
func isBareWildcard(v any) bool {
	return extractLiteralValue(v) == "*"
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

	sqlStr, err := s.dialect.Fuzzy(colStr, termStr)
	if err != nil {
		return "", nil, err
	}
	return sqlStr, params, nil
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
		elem, err := normalizeArrayElemValue(ref.name, ref.info.Type, extractLiteralValue(e.Right))
		if err != nil {
			return "", nil, err
		}
		param, err := s.dialect.EncodeElement(elem)
		if err != nil {
			return "", nil, err
		}
		return s.dialect.ArrayContains(ref.sql), append(leftParams, param), nil
	}

	if ref.isArray() {
		return "", nil, fmt.Errorf(
			"operator %s is not supported on array field '%s'; array fields support containment (%s:value), wildcards (%s:*value*) and null checks",
			e.Op, ref.name, ref.name, ref.name,
		)
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

// renderGroupedFieldLeaf renders a single node (leaf or sub-tree) in a grouped
// field expression, always using ref as the column regardless of what the
// node's own field is.
//
// Operator selection goes through the same array check as renderComparison, so
// tags:(a OR b) renders containment rather than scalar equality, and a wildcard
// leaf such as tags:(golang* OR rust) matches per element rather than binding
// the raw pattern as a literal to be contained.
func (s *SQLDriver) renderGroupedFieldLeaf(ref fieldRef, v any) (string, []any, error) {
	if e, ok := v.(*expr.Expression); ok {
		switch e.Op {
		case expr.Or, expr.And:
			return s.renderGroupedFieldExpr(ref, e)
		case expr.Equals, expr.Like:
			// Use the value from this leaf but with the outer field
			return s.renderGroupedFieldLeaf(ref, e.Right)
		case expr.Wild:
			return s.renderGroupedWildcardLeaf(ref, e)
		}
	}
	if isNullValue(v) {
		return fmt.Sprintf("%s IS NULL", ref.sql), nil, nil
	}

	if ref.isArray() {
		elem, err := normalizeArrayElemValue(ref.name, ref.info.Type, extractLiteralValue(v))
		if err != nil {
			return "", nil, err
		}
		param, err := s.dialect.EncodeElement(elem)
		if err != nil {
			return "", nil, err
		}
		return s.dialect.ArrayContains(ref.sql), []any{param}, nil
	}

	valStr, valParams, err := s.serializeValue(v)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("%s = %s", ref.sql, valStr), valParams, nil
}

// renderGroupedWildcardLeaf renders a wildcard leaf inside a grouped field
// expression — the `golang*` in tags:(golang* OR rust) — against the outer
// field, using the same pattern-matching path as the ungrouped form.
//
// Without this the leaf would fall through to equality/containment and bind the
// raw pattern (`golang*`) as a literal value, which silently matches nothing.
func (s *SQLDriver) renderGroupedWildcardLeaf(ref fieldRef, e *expr.Expression) (string, []any, error) {
	raw, ok := extractLiteralString(e)
	if !ok {
		return "", nil, fmt.Errorf("invalid wildcard value in grouped expression for field '%s'", ref.name)
	}

	valStr, valParams, err := s.serializeValue(e)
	if err != nil {
		return "", nil, err
	}

	if ref.isArray() {
		return s.renderArrayWildcard(ref, isBareWildcard(raw), valParams)
	}

	sqlStr, err := s.renderScalarLike(ref.sql, valStr)
	if err != nil {
		return "", nil, err
	}
	return sqlStr, valParams, nil
}

// renderBinary handles binary and unary logical operators via the shared walker.
// Note: Must, MustNot and Not are unary (only Left operand), while And and Or
// are binary.
func (s *SQLDriver) renderBinary(e *expr.Expression) (string, []any, error) {
	// A negation over field:null collapses to IS NOT NULL rather than
	// NOT (field IS NULL). Both are correct SQL, but the base driver emits the
	// former, and going through the walker instead would silently change the
	// rendered output for every existing `NOT field:null` query.
	if e.Op == expr.Not || e.Op == expr.MustNot {
		if inner, ok := nullEqualsOperand(e.Left); ok {
			ref, err := s.resolveField(inner.Left)
			if err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("%s IS NOT NULL", ref.sql), ref.params, nil
		}

		// Array containment yields NULL for a NULL column, so a plain NOT would
		// drop those rows instead of complementing them. IS NOT TRUE maps both
		// NULL and false to true, which is the complement we want.
		//
		// The leaf cannot do this itself with COALESCE: that wrapper is opaque
		// to the Postgres planner and costs the GIN index (13.8ms sequential
		// scan versus 0.036ms bitmap index scan on 300k rows). Applying it only
		// here keeps the positive path — the common one — indexable, and a
		// negation could not use the index either way.
		//
		// Scalar comparisons keep plain NOT, so their NULL handling is
		// unchanged; only a subtree that actually contains array containment
		// takes this path.
		if child, ok := e.Left.(*expr.Expression); ok && s.subtreeHasArrayContainment(child) {
			inner, params, err := s.renderParamInternal(child)
			if err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("(%s) IS NOT TRUE", inner), params, nil
		}
	}

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

// quoteColumn quotes a column name using the provider's identifier
// syntax, unless the string is already provider-specific JSON access syntax.
//
// MySQL's default sql_mode does not include ANSI_QUOTES, so a double-quoted
// "col" there is a string LITERAL, not an identifier — the comparison would
// silently run against the constant text instead of the column. MySQL uses
// backticks; Postgres and SQLite use double quotes.
func (s *SQLDriver) quoteColumn(colStr string) string {
	if isJSONSyntax(colStr) {
		return colStr
	}
	return s.dialect.QuoteIdent(colStr)
}

func (s *SQLDriver) serializeColumn(in any) (string, []any, error) {
	if name, ok := columnName(in); ok {
		return s.quoteColumn(name), nil, nil
	}
	if sub, ok := in.(*expr.Expression); ok {
		return s.renderParamInternal(sub)
	}
	return "", nil, fmt.Errorf("unexpected column type: %T", in)
}

// fieldRef is a column reference resolved back to its model metadata.
//
// Resolution must happen BEFORE quoting: by the time a column has been through
// quoteColumn it is `"tags"` or `metadata->>'k'`, and the original model
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
	raw, ok := columnName(in)
	if !ok {
		// Not a column reference: a rendered sub-expression carries its own SQL
		// and params, and anything else is a caller error.
		sub, isExpr := in.(*expr.Expression)
		if !isExpr {
			return fieldRef{}, fmt.Errorf("unexpected column type: %T", in)
		}
		sql, params, err := s.renderParamInternal(sub)
		if err != nil {
			return fieldRef{}, err
		}
		return fieldRef{sql: sql, params: params}, nil
	}

	ref := fieldRef{name: raw, sql: s.quoteColumn(raw)}
	// A dotted name is JSONB nested access; the base field is not the column.
	if !strings.Contains(raw, ".") {
		if info, exists := s.fields[raw]; exists {
			ref.info = info
			ref.known = true
		}
	}
	return ref, nil
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
			// Quote the base here: the returned expression contains ->> or
			// JSON_EXTRACT, so quoteColumn sees isJSONSyntax and passes it
			// through untouched — this is the only chance to quote it. Left
			// bare, a mixed-case column folds on Postgres ("column \"mixed\"
			// does not exist") and a reserved word is a syntax error on MySQL.
			return expr.Column(s.dialect.JSONExtract(s.dialect.QuoteIdent(baseField), subField))
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

// nullEqualsOperand reports whether v is an Equals(field, null) expression —
// the operand shape that turns a negation into IS NOT NULL. Mirrors
// go-lucene's base driver (driver.Base.RenderParam), which collapses
// Not/MustNot over that shape rather than emitting NOT (... IS NULL).
func nullEqualsOperand(v any) (*expr.Expression, bool) {
	e, ok := v.(*expr.Expression)
	if !ok || e.Op != expr.Equals {
		return nil, false
	}
	if !isNullValue(e.Right) {
		return nil, false
	}
	return e, true
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
	ref, err := s.resolveField(e.Left)
	if err != nil {
		return "", nil, err
	}
	if ref.isArray() {
		return "", nil, fmt.Errorf(
			"range queries are not supported on array field '%s'; array fields support containment (%s:value), wildcards (%s:*value*) and null checks",
			ref.name, ref.name, ref.name,
		)
	}
	colStr := ref.sql

	rangeBoundary, ok := e.Right.(*expr.RangeBoundary)
	if !ok {
		return "", nil, fmt.Errorf("invalid range expression structure: expected *expr.RangeBoundary, got %T", e.Right)
	}

	var minVal, maxVal string
	params := append([]any{}, ref.params...)

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

// subtreeHasArrayContainment reports whether any Equals node under e resolves
// to a multi-valued field, i.e. whether rendering e produces at least one
// containment expression whose NULL handling needs the IS NOT TRUE treatment.
//
// It walks the expression tree rather than inspecting rendered SQL, so it
// cannot be fooled by a column or value that happens to contain an operator.
func (s *SQLDriver) subtreeHasArrayContainment(e *expr.Expression) bool {
	if e == nil {
		return false
	}

	if e.Op == expr.Equals && !isNullValue(e.Right) {
		if ref, err := s.resolveField(e.Left); err == nil && ref.isArray() {
			return true
		}
	}

	if left, ok := e.Left.(*expr.Expression); ok && s.subtreeHasArrayContainment(left) {
		return true
	}
	if right, ok := e.Right.(*expr.Expression); ok && s.subtreeHasArrayContainment(right) {
		return true
	}
	return false
}
