package lucene

import (
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	lucene "github.com/grindlemire/go-lucene"
	"github.com/grindlemire/go-lucene/pkg/lucene/expr"
)

// Safety limits for query parsing
const (
	DefaultMaxQueryLength = 10000 // 10KB - prevents memory exhaustion
	DefaultMaxDepth       = 20    // Prevents stack overflow from deep nesting
	DefaultMaxTerms       = 100   // Prevents CPU exhaustion from complex queries
)

// ParserConfig allows customization of parser behavior and security limits.
type ParserConfig struct {
	MaxQueryLength int // 0 = use default (10000)
	MaxDepth       int // 0 = use default (20)
	MaxTerms       int // 0 = use default (100)
}

// FieldInfo describes a searchable field and its properties.
type FieldInfo struct {
	Name           string
	Type           reflect.Type // For validation only
	ImplicitSearch bool         // Whether this field is included in unfielded/implicit queries
}

// Parser provides Lucene query parsing with security limits.
// Drivers are created on-demand when calling ParseToSQL or ParseToDynamoDBPartiQL.
type Parser struct {
	Fields []FieldInfo // All searchable fields

	// Security limits (configurable with safe defaults)
	MaxQueryLength int // Maximum query string length (default: 10KB)
	MaxDepth       int // Maximum nesting depth (default: 20)
	MaxTerms       int // Maximum number of terms (default: 100)

	// Field lookup maps for O(1) validation
	fieldMap map[string]FieldInfo // All fields by name
}

// NewParser creates a parser by introspecting a struct's fields.
//
// Basic usage:
//
//	parser, err := lucene.NewParser(Task{})
//
// With custom configuration:
//
//	config := &lucene.ParserConfig{
//	    MaxQueryLength: 5000,
//	    MaxDepth:       10,
//	}
//	parser, err := lucene.NewParser(Task{}, config)
//
// Auto-detection rules:
// - String fields: ImplicitSearch=true (included in unfielded queries)
// - Non-string fields (int, time.Time, uuid, etc.): ImplicitSearch=false
// - JSONB fields: ImplicitSearch=false (require field.subfield syntax)
//
// Field name extraction:
// - Uses `json` struct tag for field names
// - Skips fields without `json` tag or with `json:"-"`
func NewParser(model any, config ...*ParserConfig) (*Parser, error) {
	fields, err := extractFields(model)
	if err != nil {
		return nil, err
	}

	// Build field map and validate for duplicates
	fieldMap, err := buildFieldMap(fields)
	if err != nil {
		return nil, err
	}

	// Apply config or use defaults
	maxQueryLength := DefaultMaxQueryLength
	maxDepth := DefaultMaxDepth
	maxTerms := DefaultMaxTerms

	if len(config) > 0 && config[0] != nil {
		cfg := config[0]
		if cfg.MaxQueryLength > 0 {
			maxQueryLength = cfg.MaxQueryLength
		}
		if cfg.MaxDepth > 0 {
			maxDepth = cfg.MaxDepth
		}
		if cfg.MaxTerms > 0 {
			maxTerms = cfg.MaxTerms
		}
	}

	return &Parser{
		Fields:         fields,
		MaxQueryLength: maxQueryLength,
		MaxDepth:       maxDepth,
		MaxTerms:       maxTerms,
		fieldMap:       fieldMap,
	}, nil
}

// extractFields uses reflection to extract field metadata from a struct.
func extractFields(model any) ([]FieldInfo, error) {
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %s", t.Kind())
	}

	var fields []FieldInfo
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Get field name from json tag
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		// Strip options from json tag (e.g., "name,omitempty" -> "name")
		if commaIdx := strings.Index(jsonTag, ","); commaIdx != -1 {
			jsonTag = jsonTag[:commaIdx]
		}

		// Implicit search: only string fields
		implicitSearch := field.Type.Kind() == reflect.String

		fields = append(fields, FieldInfo{
			Name:           jsonTag,
			Type:           field.Type,
			ImplicitSearch: implicitSearch,
		})
	}

	return fields, nil
}

// buildFieldMap builds a field map from a slice of fields and validates for duplicates.
// Returns an error if duplicate field names are found.
func buildFieldMap(fields []FieldInfo) (map[string]FieldInfo, error) {
	fieldMap := make(map[string]FieldInfo, len(fields))
	for _, f := range fields {
		if existing, exists := fieldMap[f.Name]; exists {
			return nil, fmt.Errorf("duplicate field name '%s': already defined with type %v, cannot redefine with type %v", f.Name, existing.Type, f.Type)
		}
		fieldMap[f.Name] = f
	}
	return fieldMap, nil
}

// canUseNestedAccess checks if a field type supports nested access (field.subfield syntax).
func canUseNestedAccess(t reflect.Type) bool {
	// Return false for nil types
	if t == nil {
		return false
	}

	// Unwrap pointers
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Check type name for JSONB-like types
	name := t.Name()
	if strings.Contains(name, "JSONB") || strings.Contains(name, "JSON") {
		return true
	}

	// Maps and structs support nested access
	if t.Kind() == reflect.Map || t.Kind() == reflect.Struct {
		return true
	}

	return false
}

// Precompiled regex for performance - matches Lucene operators and special syntax
var (
	// Matches field:value pattern (including JSONB like labels.category:value)
	fieldValuePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)?:`)
	// Extracts field name from field:value pattern
	fieldExtractPattern = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?):`)
	// Matches boolean operators (case-insensitive)
	booleanOperators = regexp.MustCompile(`(?i)^(AND|OR|NOT|\+|-)$`)
	// Matches range syntax
	rangePattern = regexp.MustCompile(`^\[.*\s+TO\s+.*\]$|^\{.*\s+TO\s+.*\}$`)
)

// InvalidFieldError represents an error when a query references a non-existent field
type InvalidFieldError struct {
	Field       string
	ValidFields []string
}

func (e *InvalidFieldError) Error() string {
	return fmt.Sprintf("invalid field '%s' in query; valid fields are: %s", e.Field, strings.Join(e.ValidFields, ", "))
}

// ParseToMap parses a Lucene query into a map representation.
// Note: This is a legacy method kept for backward compatibility.
func (p *Parser) ParseToMap(query string) (map[string]any, error) {
	if err := p.validateQuery(query); err != nil {
		return nil, err
	}

	e, err := p.parseWithImplicitSearch(query)
	if err != nil {
		return nil, err
	}

	// Convert expression to map
	return p.exprToMap(e), nil
}

// parseQueryCommon performs common parsing steps shared by ParseToSQL and ParseToDynamoDBPartiQL.
// Returns the parsed expression or an error.
func (p *Parser) parseQueryCommon(query string, queryType string) (*expr.Expression, error) {
	slog.Debug(fmt.Sprintf(`Parsing query to %s: %s`, queryType, query))

	if err := p.validateQuery(query); err != nil {
		return nil, err
	}

	// Expand implicit terms first (for validation of the full query)
	expandedQuery := p.expandImplicitTerms(query)

	// Validate all field references exist in the model
	if err := p.ValidateFields(expandedQuery); err != nil {
		return nil, err
	}

	// Parse using the library
	e, err := p.parseWithImplicitSearch(query)
	if err != nil {
		return nil, err
	}

	return e, nil
}

// ParseToSQL parses a Lucene query and converts it to SQL with parameters for the specified provider.
// Creates a SQL driver on-demand for rendering with provider-specific syntax.
// Provider should be one of: "postgresql", "mysql", "sqlite"
func (p *Parser) ParseToSQL(query string, provider string) (string, []any, error) {
	e, err := p.parseQueryCommon(query, "SQL")
	if err != nil {
		return "", nil, err
	}

	// Create SQL driver on-demand for the specified provider and render
	driver, err := NewSQLDriver(p.Fields, provider)
	if err != nil {
		return "", nil, err
	}
	sql, params, err := driver.RenderParam(e)
	if err != nil {
		return "", nil, err
	}

	return sql, params, nil
}

// ParseToDynamoDBPartiQL parses a Lucene query and converts it to DynamoDB PartiQL.
// Creates a DynamoDB driver on-demand for rendering.
func (p *Parser) ParseToDynamoDBPartiQL(query string) (string, []types.AttributeValue, error) {
	e, err := p.parseQueryCommon(query, "DynamoDB PartiQL")
	if err != nil {
		return "", nil, err
	}

	// Create DynamoDB driver on-demand and render
	driver, err := NewDynamoDBDriver(p.Fields)
	if err != nil {
		return "", nil, err
	}
	partiql, attrs, err := driver.RenderPartiQL(e)
	if err != nil {
		return "", nil, err
	}

	return partiql, attrs, nil
}

func (p *Parser) validateQuery(query string) error {
	var errs []error

	if len(query) > p.MaxQueryLength {
		errs = append(errs, fmt.Errorf("query too long: %d bytes exceeds maximum of %d bytes", len(query), p.MaxQueryLength))
	}

	depth := calculateNestingDepth(query)
	if depth > p.MaxDepth {
		errs = append(errs, fmt.Errorf("query too complex: nesting depth %d exceeds maximum of %d", depth, p.MaxDepth))
	}

	terms := countTerms(query)
	if terms > p.MaxTerms {
		errs = append(errs, fmt.Errorf("query too large: %d terms exceeds maximum of %d", terms, p.MaxTerms))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

func calculateNestingDepth(query string) int {
	maxDepth := 0
	currentDepth := 0
	inQuotes := false

	for i := 0; i < len(query); i++ {
		c := query[i]

		if c == '\\' && i+1 < len(query) {
			i++
			continue
		}

		if c == '"' {
			inQuotes = !inQuotes
			continue
		}

		if !inQuotes {
			switch c {
			case '(', '[', '{':
				currentDepth++
				if currentDepth > maxDepth {
					maxDepth = currentDepth
				}
			case ')', ']', '}':
				currentDepth--
			}
		}
	}

	return maxDepth
}

// countTerms counts search terms in a query.
// Terms include field:value pairs, implicit terms, and quoted phrases.
// Operators (AND, OR, NOT) and parentheses are excluded.
func countTerms(query string) int {
	if query == "" {
		return 0
	}

	terms := 0
	inQuotes := false
	inRange := false
	currentTerm := false

	for i := 0; i < len(query); i++ {
		c := query[i]

		if c == '\\' && i+1 < len(query) {
			i++
			currentTerm = true
			continue
		}

		if c == '"' {
			if !inQuotes {
				if currentTerm {
					terms++
				}
				currentTerm = true
			} else {
				if currentTerm {
					terms++
					currentTerm = false
				}
			}
			inQuotes = !inQuotes
			continue
		}

		if !inQuotes {
			if c == '[' || c == '{' {
				inRange = true
				if currentTerm {
					terms++
					currentTerm = false
				}
				continue
			}
			if c == ']' || c == '}' {
				inRange = false
				if currentTerm {
					terms++
					currentTerm = false
				}
				continue
			}
		}

		if c == ' ' && !inQuotes && !inRange {
			if currentTerm {
				terms++
				currentTerm = false
			}
			continue
		}

		if !inQuotes && !inRange && (c == '(' || c == ')') {
			if currentTerm {
				terms++
				currentTerm = false
			}
			continue
		}

		if !inQuotes && !inRange && currentTerm {
			remaining := query[i:]
			if strings.HasPrefix(remaining, "AND ") || strings.HasPrefix(remaining, "OR ") ||
				strings.HasPrefix(remaining, "NOT ") || strings.HasPrefix(remaining, "and ") ||
				strings.HasPrefix(remaining, "or ") || strings.HasPrefix(remaining, "not ") {
				terms++
				currentTerm = false
				if len(remaining) >= 3 && (remaining[0] == 'A' || remaining[0] == 'a') {
					i += 3
					continue
				}
				if len(remaining) >= 3 && (remaining[0] == 'N' || remaining[0] == 'n') {
					i += 3
					continue
				}
				i += 2
				continue
			}
		}

		currentTerm = true
	}

	if currentTerm {
		terms++
	}

	return terms
}

// ValidateFields returns InvalidFieldError if the query references non-existent fields.
func (p *Parser) ValidateFields(query string) error {
	matches := fieldExtractPattern.FindAllStringSubmatchIndex(query, -1)
	if len(matches) == 0 {
		return nil
	}

	validFields := p.getValidFieldNames()

	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		fieldStart := match[2]
		fieldEnd := match[3]

		if isInsideQuotes(query, fieldStart) {
			continue
		}

		fieldName := query[fieldStart:fieldEnd]

		if err := p.validateFieldName(fieldName); err != nil {
			return &InvalidFieldError{
				Field:       fieldName,
				ValidFields: validFields,
			}
		}
	}

	return nil
}

func isInsideQuotes(query string, pos int) bool {
	inQuotes := false
	for i := 0; i < pos && i < len(query); i++ {
		c := query[i]
		if c == '\\' && i+1 < len(query) {
			i++
			continue
		}
		if c == '"' {
			inQuotes = !inQuotes
		}
	}
	return inQuotes
}

// validateFieldName validates both simple fields (name) and nested fields (labels.category).
func (p *Parser) validateFieldName(fieldName string) error {
	if strings.Contains(fieldName, ".") {
		parts := strings.SplitN(fieldName, ".", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid field format: %s", fieldName)
		}

		baseField := parts[0]
		subField := parts[1]

		// Check for whitespace in field names (security: prevents obfuscation, OWASP A03)
		if strings.TrimSpace(baseField) != baseField {
			return fmt.Errorf("invalid field format '%s': whitespace not allowed in field names", fieldName)
		}
		if strings.TrimSpace(subField) != subField {
			return fmt.Errorf("invalid field format '%s': whitespace not allowed in field names (use 'field.subfield' not 'field. subfield')", fieldName)
		}

		// Check if base field exists
		field, exists := p.fieldMap[baseField]
		if !exists {
			return fmt.Errorf("field '%s' does not exist", baseField)
		}

		// Check if base field supports nested access
		if !canUseNestedAccess(field.Type) {
			return fmt.Errorf("field '%s' does not support nested access (field.subfield syntax); use explicit field names only", baseField)
		}

		return nil
	}

	if _, exists := p.fieldMap[fieldName]; !exists {
		return fmt.Errorf("field '%s' does not exist", fieldName)
	}

	return nil
}

func (p *Parser) getValidFieldNames() []string {
	var names []string
	for _, f := range p.Fields {
		// Add a hint for fields that support nested access
		if canUseNestedAccess(f.Type) {
			names = append(names, f.Name+".*")
		} else {
			names = append(names, f.Name)
		}
	}
	return names
}

func (p *Parser) getImplicitSearchFields() []FieldInfo {
	var fields []FieldInfo
	for _, field := range p.Fields {
		if field.ImplicitSearch {
			fields = append(fields, field)
		}
	}
	return fields
}

// isImplicitTerm returns true if token is a search term without an explicit field prefix.
func isImplicitTerm(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}

	// Check if it's a boolean operator
	if booleanOperators.MatchString(token) {
		return false
	}

	// Check if it starts with + or - (required/prohibited operators)
	if strings.HasPrefix(token, "+") || strings.HasPrefix(token, "-") {
		// Remove the prefix and check the rest
		rest := token[1:]
		if fieldValuePattern.MatchString(rest) {
			return false // It's a +field:value or -field:value
		}
		// Otherwise it's an implicit term with +/- modifier
		return true
	}

	// Check if it's a field:value pattern
	if fieldValuePattern.MatchString(token) {
		return false
	}

	// Check if it's a range query
	if rangePattern.MatchString(token) {
		return false
	}

	// Check if it's a parenthesis
	if token == "(" || token == ")" {
		return false
	}

	// Quoted strings are also implicit terms (they search across implicit search fields)
	if strings.HasPrefix(token, `"`) && strings.HasSuffix(token, `"`) {
		return true
	}

	return true
}

// expandImplicitTerms expands implicit search terms to explicit field:value patterns
// across all implicit search fields. For example:
// "paint" → "(name:*paint* OR description:*paint*)"
// "paint*" → "(name:paint* OR description:paint*)"
// '"Living Room"' → '(name:"Living Room" OR description:"Living Room")'
func (p *Parser) expandImplicitTerms(query string) string {
	implicitFields := p.getImplicitSearchFields()
	if len(implicitFields) == 0 {
		return query
	}

	// Tokenize the query while preserving structure
	tokens := tokenizeQuery(query)
	var result []string

	for _, token := range tokens {
		if isImplicitTerm(token) {
			// Check if it has a +/- prefix
			prefix := ""
			term := token
			if strings.HasPrefix(token, "+") || strings.HasPrefix(token, "-") {
				prefix = string(token[0])
				term = token[1:]
			}

			// Check if it's a quoted phrase (exact match) or already has wildcards
			searchTerm := term
			isQuotedPhrase := strings.HasPrefix(term, `"`) && strings.HasSuffix(term, `"`)
			hasWildcards := strings.Contains(term, "*") || strings.Contains(term, "?")

			// For implicit search without wildcards or quotes, use contains matching
			// This provides a better user experience for simple searches
			if !isQuotedPhrase && !hasWildcards {
				searchTerm = "*" + term + "*"
			}

			// Expand to all implicit search fields with OR
			var fieldTerms []string
			for _, field := range implicitFields {
				fieldTerms = append(fieldTerms, fmt.Sprintf("%s:%s", field.Name, searchTerm))
			}

			if len(fieldTerms) == 1 {
				result = append(result, prefix+fieldTerms[0])
			} else {
				expanded := "(" + strings.Join(fieldTerms, " OR ") + ")"
				if prefix != "" {
					expanded = prefix + expanded
				}
				result = append(result, expanded)
			}
		} else {
			result = append(result, token)
		}
	}

	return strings.Join(result, " ")
}

// tokenizeQuery splits query into tokens, preserving quoted strings and range brackets.
func tokenizeQuery(query string) []string {
	var tokens []string
	var current strings.Builder
	inQuotes := false
	inRange := false
	rangeDepth := 0

	for i := 0; i < len(query); i++ {
		c := query[i]

		// Handle quotes
		if c == '"' && (i == 0 || query[i-1] != '\\') {
			inQuotes = !inQuotes
			current.WriteByte(c)
			continue
		}

		// Handle range brackets
		if !inQuotes {
			if c == '[' || c == '{' {
				inRange = true
				rangeDepth++
				current.WriteByte(c)
				continue
			}
			if c == ']' || c == '}' {
				current.WriteByte(c)
				rangeDepth--
				if rangeDepth == 0 {
					inRange = false
				}
				continue
			}
		}

		// Handle spaces (token separators) when not in quotes or range
		if c == ' ' && !inQuotes && !inRange {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}

		// Handle parentheses as separate tokens
		if !inQuotes && !inRange && (c == '(' || c == ')') {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			tokens = append(tokens, string(c))
			continue
		}

		current.WriteByte(c)
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// parseWithImplicitSearch expands unfielded terms across all implicit search fields with OR.
func (p *Parser) parseWithImplicitSearch(query string) (*expr.Expression, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	// Expand implicit terms to explicit field:value patterns
	expandedQuery := p.expandImplicitTerms(query)

	slog.Debug("Query expansion", "original", query, "expanded", expandedQuery)

	// Get first implicit field as fallback for the parser
	fallbackField := ""
	implicitFields := p.getImplicitSearchFields()
	if len(implicitFields) > 0 {
		fallbackField = implicitFields[0].Name
	}
	if fallbackField == "" && len(p.Fields) > 0 {
		fallbackField = p.Fields[0].Name
	}

	return lucene.Parse(expandedQuery, lucene.WithDefaultField(fallbackField))
}

// exprToMap converts expression to map format (legacy, kept for backward compatibility).
func (p *Parser) exprToMap(e *expr.Expression) map[string]any {
	if e == nil {
		return nil
	}

	result := make(map[string]any)

	switch e.Op {
	case expr.Equals:
		if col, ok := e.Left.(expr.Column); ok {
			result[string(col)] = p.valueToAny(e.Right)
		}
	case expr.Like:
		if col, ok := e.Left.(expr.Column); ok {
			pattern := p.valueToAny(e.Right)
			result[string(col)] = map[string]any{"$like": pattern}
		}
	case expr.And, expr.Or, expr.Not:
		var children []map[string]any
		if leftExpr, ok := e.Left.(*expr.Expression); ok {
			children = append(children, p.exprToMap(leftExpr))
		}
		if rightExpr, ok := e.Right.(*expr.Expression); ok {
			children = append(children, p.exprToMap(rightExpr))
		}
		result[e.Op.String()] = children
	default:
		// For other operators, do a simple conversion
		if col, ok := e.Left.(expr.Column); ok {
			result[string(col)] = p.valueToAny(e.Right)
		}
	}

	return result
}

func (p *Parser) valueToAny(v any) any {
	switch val := v.(type) {
	case *expr.Expression:
		return p.exprToMap(val)
	case string:
		return val
	case int, float64:
		return val
	default:
		return fmt.Sprintf("%v", v)
	}
}
