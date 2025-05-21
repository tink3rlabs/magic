package lucene

import (
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type FieldInfo struct {
	Name    string
	IsJSONB bool
}

type Parser struct {
	DefaultFields []FieldInfo
}

type NodeType int

const (
	NodeTerm NodeType = iota
	NodeWildcard
	NodeLogical
)

type LogicalOperator string

const (
	AND LogicalOperator = "AND"
	OR  LogicalOperator = "OR"
	NOT LogicalOperator = "NOT"
)

type MatchType int

const (
	matchExact MatchType = iota
	matchStartsWith
	matchEndsWith
	matchContains
)

type Node struct {
	Type      NodeType
	Field     string
	Value     string
	Operator  LogicalOperator
	Children  []*Node
	Negate    bool
	MatchType MatchType
}

func NewParserFromType(model any) (*Parser, error) {
	fields, err := getStructFields(model)
	if err != nil {
		return nil, err
	}
	return NewParser(fields), nil
}

func NewParser(defaultFields []FieldInfo) *Parser {
	return &Parser{DefaultFields: defaultFields}
}

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

		fields = append(fields, FieldInfo{
			Name:    jsonTag,
			IsJSONB: isJSONB,
		})
	}

	return fields, nil
}

func (p *Parser) ParseToMap(query string) (map[string]any, error) {
	node, err := p.parse(query)
	if err != nil {
		return nil, err
	}
	return p.nodeToMap(node), nil
}

func (p *Parser) ParseToSQL(query string) (string, []any, error) {
	slog.Debug(fmt.Sprintf(`Parsing query to sql: %s`, query))
	re := regexp.MustCompile(`(\w+):"([^"]+)"`)
	query = re.ReplaceAllString(query, `$1:$2`)
	node, err := p.parse(query)
	if err != nil {
		return "", nil, err
	}
	return p.nodeToSQL(node)
}

func (p *Parser) parse(query string) (*Node, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	if strings.HasPrefix(query, "(") && strings.HasSuffix(query, ")") {
		return p.parse(query[1 : len(query)-1])
	}

	if andParts := splitByOperator(query, "AND"); len(andParts) > 1 {
		return p.createLogicalNode(AND, andParts)
	}
	if orParts := splitByOperator(query, "OR"); len(orParts) > 1 {
		return p.createLogicalNode(OR, orParts)
	}
	if notParts := splitByOperator(query, "NOT"); len(notParts) > 1 {
		return p.createLogicalNode(NOT, notParts)
	}

	if parts := strings.SplitN(query, ":", 2); len(parts) == 2 {
		field := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		// Skip empty fields or values
		if field == "" || value == "" {
			return nil, nil
		}
		return p.createTermNode(field, value)
	}

	// Skip empty implicit terms
	if query = strings.TrimSpace(query); query == "" {
		return nil, nil
	}

	return p.createImplicitNode(query)
}

func splitByOperator(input string, op string) []string {
	// Handle case where the operator is at the beginning of the string
	trimmedInput := strings.TrimSpace(input)
	lowerInput := strings.ToLower(trimmedInput)
	lowerOp := strings.ToLower(op)

	if strings.HasPrefix(lowerInput, lowerOp) {
		// Check if it's a standalone word (followed by space or end of string)
		opLength := len(op)
		if len(trimmedInput) == opLength || (len(trimmedInput) > opLength && trimmedInput[opLength] == ' ') {
			afterOp := strings.TrimSpace(trimmedInput[opLength:])
			if afterOp != "" {
				return []string{"", afterOp}
			}
		}
	}

	// Original logic for operators in the middle
	re := regexp.MustCompile(fmt.Sprintf(`(?i)\s+%s\s+`, op))
	parts := re.Split(input, -1)
	if len(parts) > 1 {
		return parts
	}

	return nil
}

func (p *Parser) createImplicitNode(term string) (*Node, error) {
	slog.Debug(fmt.Sprintf(`Handling implicit: %s`, term))
	term = strings.Trim(term, `"`)

	containsWildcard := strings.Contains(term, "*") || strings.Contains(term, "?")

	node := &Node{
		Type:     NodeLogical,
		Operator: OR,
	}

	for _, field := range p.DefaultFields {
		var child *Node
		var err error

		if containsWildcard {
			child, err = p.createWildcardNode(field.Name, term)
		} else {
			child, err = p.createTermNode(field.Name, term)

			if child.Type == NodeTerm {
				child.Type = NodeWildcard
				child.MatchType = matchContains
			}
		}
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, child)
	}

	return node, nil
}

func (p *Parser) createWildcardNode(field, value string) (*Node, error) {
	// Skip empty fields or values
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)

	if field == "" || value == "" {
		return nil, nil
	}

	formattedField := p.formatFieldName(field)

	node := &Node{
		Type:  NodeWildcard,
		Field: formattedField,
		Value: value,
	}

	// Process the wildcard pattern
	if strings.HasPrefix(value, "*") && strings.HasSuffix(value, "*") {
		// For *term* pattern
		node.MatchType = matchContains
		node.Value = strings.Trim(value, "*")
	} else if strings.HasPrefix(value, "*") {
		// For *term pattern
		node.MatchType = matchEndsWith
		node.Value = strings.TrimPrefix(value, "*")
	} else if strings.HasSuffix(value, "*") {
		// For term* pattern
		node.MatchType = matchStartsWith
		node.Value = strings.TrimSuffix(value, "*")
	} else if strings.Contains(value, "*") {
		// For patterns like te*rm
		node.MatchType = matchContains
		// Replace wildcards with % for SQL LIKE
		node.Value = strings.Replace(value, "*", "%", -1)
	} else {
		// Default to contains match for other patterns
		node.MatchType = matchContains
	}

	// Skip if the value becomes empty after processing
	if node.Value == "" {
		return nil, nil
	}

	return node, nil
}

func (p *Parser) formatFieldName(fieldName string) string {
	if parts := strings.SplitN(fieldName, ".", 2); len(parts) == 2 {
		baseField := parts[0]
		subField := parts[1]

		for _, field := range p.DefaultFields {
			if field.IsJSONB && field.Name == baseField {
				return fmt.Sprintf("%s->>'%s'", baseField, subField)
			}
		}
	}
	return fieldName
}

func (p *Parser) createTermNode(field, value string) (*Node, error) {
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)

	if field == "" || value == "" {
		return nil, nil
	}
	formattedField := p.formatFieldName(field)

	trimmedValue := strings.TrimSpace(strings.Trim(value, `"`))

	// Skip if the value becomes empty after trimming
	if trimmedValue == "" {
		return nil, nil
	}

	node := &Node{
		Type:  NodeTerm,
		Field: formattedField,
		Value: strings.Trim(value, `"`),
	}

	if strings.Contains(value, "*") || strings.Contains(value, "?") {
		node.Type = NodeWildcard

		// Determine the match type based on wildcard position
		if strings.HasPrefix(value, "*") && strings.HasSuffix(value, "*") {
			node.MatchType = matchContains
			node.Value = strings.Trim(value, "*")
		} else if strings.HasPrefix(value, "*") {
			node.MatchType = matchEndsWith
			node.Value = strings.TrimPrefix(value, "*")
		} else if strings.HasSuffix(value, "*") {
			node.MatchType = matchStartsWith
			node.Value = strings.TrimSuffix(value, "*")
		} else {
			// For patterns like te*rm or te?rm
			node.MatchType = matchContains
			// For SQL LIKE, convert * to % and ? to _
			node.Value = strings.Replace(strings.Replace(value, "*", "%", -1), "?", "_", -1)
		}

		// Skip if the value becomes empty after processing wildcards
		if node.Value == "" {
			return nil, nil
		}
	}

	return node, nil
}

func (p *Parser) createLogicalNode(op LogicalOperator, parts []string) (*Node, error) {
	node := &Node{
		Type:     NodeLogical,
		Operator: op,
	}

	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		child, err := p.parse(part)
		if err != nil {
			return nil, err
		}
		if child != nil {
			node.Children = append(node.Children, child)
		}
	}

	// If no valid children were found, return nil
	if len(node.Children) == 0 {
		return nil, nil
	}

	return node, nil
}

func (p *Parser) nodeToMap(node *Node) map[string]any {
	if node == nil {
		return nil
	}

	switch node.Type {
	case NodeTerm:
		return map[string]any{node.Field: node.Value}
	case NodeWildcard:
		return map[string]any{node.Field: map[string]string{
			"$like": wildcardToPattern(node.Value, node.MatchType),
		}}
	case NodeLogical:
		result := make(map[string]any)
		children := make([]map[string]any, 0, len(node.Children))
		for _, child := range node.Children {
			children = append(children, p.nodeToMap(child))
		}
		result[string(node.Operator)] = children
		return result
	}
	return nil
}

func (p *Parser) nodeToSQL(node *Node) (string, []any, error) {
	if node == nil {
		return "", nil, nil
	}

	switch node.Type {
	case NodeTerm:
		if strings.Contains(node.Field, "->>") {
			return fmt.Sprintf("%s = ?", node.Field), []any{node.Value}, nil
		}
		return fmt.Sprintf("%s = ?", node.Field), []any{node.Value}, nil
	case NodeWildcard:
		pattern := wildcardToPattern(node.Value, node.MatchType)
		if strings.Contains(node.Field, "->>") {
			return fmt.Sprintf("%s ILIKE ?", node.Field), []any{pattern}, nil
		} else {
			return fmt.Sprintf("%s::text ILIKE ?", node.Field), []any{pattern}, nil
		}
	case NodeLogical:
		var parts []string
		var params []any

		for _, child := range node.Children {
			sqlPart, childParams, err := p.nodeToSQL(child)
			if err != nil {
				return "", nil, err
			}
			if sqlPart != "" {
				parts = append(parts, sqlPart)
				params = append(params, childParams...)
			}
		}

		if len(parts) == 0 {
			return "", nil, nil
		}

		if len(parts) == 1 {
			return parts[0], params, nil
		}

		operator := string(node.Operator)
		if node.Negate {
			operator = "NOT " + operator
		}

		return fmt.Sprintf("(%s)", strings.Join(parts, fmt.Sprintf(" %s ", operator))), params, nil
	}

	return "", nil, fmt.Errorf("unsupported node type")
}

func (p *Parser) ParseToDynamoDBPartiQL(query string) (string, []types.AttributeValue, error) {
	slog.Debug(fmt.Sprintf(`Parsing query to DynamoDB PartiQL: %s`, query))
	node, err := p.parse(query)
	if err != nil {
		return "", nil, err
	}
	return p.nodeToDynamoDBPartiQL(node)
}

func (p *Parser) nodeToDynamoDBPartiQL(node *Node) (string, []types.AttributeValue, error) {
	if node == nil {
		return "", nil, nil
	}

	switch node.Type {
	case NodeTerm:
		// For term node, create an exact match condition
		return fmt.Sprintf("%s = ?", node.Field), []types.AttributeValue{
			&types.AttributeValueMemberS{Value: node.Value},
		}, nil
	case NodeWildcard:
		// For wildcard node, use begins_with or contains based on the match type
		switch node.MatchType {
		case matchStartsWith:
			return fmt.Sprintf("begins_with(%s, ?)", node.Field), []types.AttributeValue{
				&types.AttributeValueMemberS{Value: node.Value},
			}, nil
		case matchEndsWith, matchContains:
			return fmt.Sprintf("contains(%s, ?)", node.Field), []types.AttributeValue{
				&types.AttributeValueMemberS{Value: node.Value},
			}, nil
		default:
			return fmt.Sprintf("%s = ?", node.Field), []types.AttributeValue{
				&types.AttributeValueMemberS{Value: node.Value},
			}, nil
		}
	case NodeLogical:
		// For logical node, combine conditions with appropriate operator
		var parts []string
		var params []types.AttributeValue

		for _, child := range node.Children {
			part, childParams, err := p.nodeToDynamoDBPartiQL(child)
			if err != nil {
				return "", nil, err
			}
			if part != "" {
				parts = append(parts, part)
				params = append(params, childParams...)
			}
		}

		if len(parts) == 0 {
			return "", nil, nil
		}

		operator := string(node.Operator)
		if node.Negate {
			operator = "NOT " + operator
		}

		return fmt.Sprintf("(%s)", strings.Join(parts, fmt.Sprintf(" %s ", operator))), params, nil
	}

	return "", nil, fmt.Errorf("unsupported node type")
}

func wildcardToPattern(value string, matchType MatchType) string {
	switch matchType {
	case matchStartsWith:
		return value + "%"
	case matchEndsWith:
		return "%" + value
	case matchContains:
		return "%" + value + "%"
	default:
		return value
	}
}
