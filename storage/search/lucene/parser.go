package lucene

import (
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

// FieldInfo describes a searchable field and its properties.
type FieldInfo struct {
	Name           string
	IsJSONB        bool
	ImplicitSearch bool // Whether this field is included in unfielded/implicit queries
}

// Parser provides Lucene query parsing with security limits.
type Parser struct {
	Fields []FieldInfo // All searchable fields

	// Security limits (configurable with safe defaults)
	MaxQueryLength int // Maximum query string length (default: 10KB)
	MaxDepth       int // Maximum nesting depth (default: 20)
	MaxTerms       int // Maximum number of terms (default: 100)

	// Field lookup maps for O(1) validation
	fieldMap    map[string]FieldInfo // All fields by name
	jsonbFields map[string]bool      // JSONB field names for sub-field validation

	// Custom drivers for different backends
	postgresDriver *PostgresJSONBDriver
	dynamoDriver   *DynamoDBPartiQLDriver
}

// NewParserFromType creates a parser by introspecting a struct's fields.
// This is the recommended approach for initializing parsers as it:
// - Works with any backend (PostgreSQL, MySQL, DynamoDB, etc.)
// - Zero database overhead
// - Compile-time safety
// - Auto-detects JSONB fields from gorm tags
// - Auto-sets string fields for implicit search (ImplicitSearch=true)
//
// Example:
//
//	type Task struct {
//	    ID          string    `json:"id"`
//	    Name        string    `json:"name"`                         // Auto: ImplicitSearch=true
//	    Description string    `json:"description"`                  // Auto: ImplicitSearch=true
//	    Status      string    `json:"status" lucene:"explicit"`     // Explicit: ImplicitSearch=false
//	    CreatedAt   time.Time `json:"created_at"`                   // Auto: ImplicitSearch=false (not string)
//	    Labels      JSONB     `json:"labels" gorm:"type:jsonb"`     // Auto: IsJSONB=true, ImplicitSearch=false
//	}
//
//	parser, err := lucene.NewParserFromType(Task{})
//
// Struct tag controls:
// - lucene:"implicit" - Force ImplicitSearch=true (include in unfielded queries)
// - lucene:"explicit" - Force ImplicitSearch=false (require field:value syntax)
// - gorm:"type:jsonb" - Auto-detected as JSONB field
//
// Auto-detection rules (when no lucene tag):
// - String fields: ImplicitSearch=true (included in unfielded queries)
// - Non-string fields (int, time.Time, uuid, etc.): ImplicitSearch=false
// - JSONB fields: ImplicitSearch=false (require field.subfield syntax)
func NewParserFromType(model any) (*Parser, error) {
	fields, err := getStructFields(model)
	if err != nil {
		return nil, err
	}
	return NewParser(fields), nil
}

func NewParser(fields []FieldInfo) *Parser {
	fieldMap := make(map[string]FieldInfo, len(fields))
	jsonbFields := make(map[string]bool)
	for _, f := range fields {
		fieldMap[f.Name] = f
		if f.IsJSONB {
			jsonbFields[f.Name] = true
		}
	}

	return &Parser{
		Fields:         fields,
		MaxQueryLength: DefaultMaxQueryLength,
		MaxDepth:       DefaultMaxDepth,
		MaxTerms:       DefaultMaxTerms,
		fieldMap:       fieldMap,
		jsonbFields:    jsonbFields,
		postgresDriver: NewPostgresJSONBDriver(fields),
		dynamoDriver:   NewDynamoDBPartiQLDriver(fields),
	}
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

// getStructFields uses reflection to extract field metadata from a struct.
// String fields get ImplicitSearch=true, others get ImplicitSearch=false.
func getStructFields(model any) ([]FieldInfo, error) {
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
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		if commaIdx := strings.Index(jsonTag, ","); commaIdx != -1 {
			jsonTag = jsonTag[:commaIdx]
		}

		gormTag := field.Tag.Get("gorm")
		isJSONB := strings.Contains(gormTag, "type:jsonb")

		luceneTag := field.Tag.Get("lucene")
		implicitSearch := false
		if luceneTag == "implicit" {
			implicitSearch = true
		} else if luceneTag != "explicit" {
			implicitSearch = field.Type.Kind() == reflect.String && !isJSONB
		}

		fields = append(fields, FieldInfo{
			Name:           jsonTag,
			IsJSONB:        isJSONB,
			ImplicitSearch: implicitSearch,
		})
	}

	return fields, nil
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

// ParseToSQL parses a Lucene query and converts it to PostgreSQL SQL with parameters.
func (p *Parser) ParseToSQL(query string) (string, []any, error) {
	slog.Debug(fmt.Sprintf(`Parsing query to SQL: %s`, query))

	if err := p.validateQuery(query); err != nil {
		return "", nil, err
	}

	// Expand implicit terms first (for validation of the full query)
	expandedQuery := p.expandImplicitTerms(query)

	// Validate all field references exist in the model
	if err := p.ValidateFields(expandedQuery); err != nil {
		return "", nil, err
	}

	// Parse using the library
	e, err := p.parseWithImplicitSearch(query)
	if err != nil {
		return "", nil, err
	}

	// Render using custom PostgreSQL driver
	sql, params, err := p.postgresDriver.RenderParam(e)
	if err != nil {
		return "", nil, err
	}

	return sql, params, nil
}

// ParseToDynamoDBPartiQL parses a Lucene query and converts it to DynamoDB PartiQL.
func (p *Parser) ParseToDynamoDBPartiQL(query string) (string, []types.AttributeValue, error) {
	slog.Debug(fmt.Sprintf(`Parsing query to DynamoDB PartiQL: %s`, query))

	if err := p.validateQuery(query); err != nil {
		return "", nil, err
	}

	// Expand implicit terms first (for validation of the full query)
	expandedQuery := p.expandImplicitTerms(query)

	// Validate all field references exist in the model
	if err := p.ValidateFields(expandedQuery); err != nil {
		return "", nil, err
	}

	// Parse using the library
	e, err := p.parseWithImplicitSearch(query)
	if err != nil {
		return "", nil, err
	}

	// Render using custom DynamoDB driver
	partiql, attrs, err := p.dynamoDriver.RenderPartiQL(e)
	if err != nil {
		return "", nil, err
	}

	return partiql, attrs, nil
}

func (p *Parser) validateQuery(query string) error {
	if len(query) > p.MaxQueryLength {
		return fmt.Errorf("query too long: %d bytes exceeds maximum of %d bytes", len(query), p.MaxQueryLength)
	}

	depth := calculateNestingDepth(query)
	if depth > p.MaxDepth {
		return fmt.Errorf("query too complex: nesting depth %d exceeds maximum of %d", depth, p.MaxDepth)
	}

	terms := countTerms(query)
	if terms > p.MaxTerms {
		return fmt.Errorf("query too large: %d terms exceeds maximum of %d", terms, p.MaxTerms)
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
				} else if len(remaining) >= 3 && (remaining[0] == 'N' || remaining[0] == 'n') {
					i += 3
				} else {
					i += 2
				}
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

// validateFieldName validates both simple fields (name) and JSONB sub-fields (labels.category).
func (p *Parser) validateFieldName(fieldName string) error {
	if strings.Contains(fieldName, ".") {
		parts := strings.SplitN(fieldName, ".", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid field format: %s", fieldName)
		}

		baseField := parts[0]

		if !p.jsonbFields[baseField] {
			if _, exists := p.fieldMap[baseField]; !exists {
				return fmt.Errorf("field '%s' does not exist", baseField)
			}
			return fmt.Errorf("field '%s' is not a JSONB field; cannot use sub-field notation", baseField)
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
		if f.IsJSONB {
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
	} else if len(p.Fields) > 0 {
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
