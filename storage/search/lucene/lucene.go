package lucene

// TODO: Make it accept storageAdapter and add logic to handlem ultiple sql providers
// TODO: Refactor the file so it works for memory, dynamodb and memory
import (
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"
)

type FieldInfo struct {
	Name    string
	IsJSONB bool
}

func NewParserFromType(model any) (*Parser, error) {
	fields, err := getStructFields(model)
	if err != nil {
		return nil, err
	}
	return NewParser(fields), nil
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

type Parser struct {
	DefaultFields []FieldInfo
}

func NewParser(defaultFields []FieldInfo) *Parser {
	return &Parser{DefaultFields: defaultFields}
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

func (p *Parser) ParseToMap(query string) (map[string]any, error) {
	node, err := p.parse(query)
	if err != nil {
		return nil, err
	}
	return p.nodeToMap(node), nil
}

func (p *Parser) ParseToSQL(query string) (string, []any, error) {
	slog.Debug(fmt.Sprintf(`Parsing query to sql: %s`, query))
	node, err := p.parse(query)
	if err != nil {
		return "", nil, err
	}
	return p.nodeToSQL(node, 1)
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
		return p.createTermNode(field, value)
	}

	return p.createImplicitNode(query)
}

func splitByOperator(input string, op string) []string {
	re := regexp.MustCompile(fmt.Sprintf(`(?i)\s+%s\s+`, op))
	parts := re.Split(input, -1)
	if len(parts) > 1 {
		return parts
	}
	return nil
}

func (p *Parser) createImplicitNode(term string) (*Node, error) {
	slog.Debug(fmt.Sprintf(`Handling implicit: %s`, term))
	node := &Node{
		Type:     NodeLogical,
		Operator: OR,
	}

	for _, field := range p.DefaultFields {
		child, err := p.createTermNode(field.Name, term)
		if err != nil {
			return nil, err
		}

		// Force ILIKE behavior for implicit searches
		if child.Type == NodeTerm {
			child.Type = NodeWildcard
			child.MatchType = matchContains
		}

		// Handle JSONB field path formatting
		if field.IsJSONB {
			if parts := strings.SplitN(child.Field, ".", 2); len(parts) == 2 {
				child.Field = fmt.Sprintf("%s->>'%s'", parts[0], parts[1])
			}
		}

		node.Children = append(node.Children, child)
	}

	return node, nil
}

func (p *Parser) createTermNode(field, value string) (*Node, error) {
	node := &Node{
		Type:  NodeTerm,
		Field: field,
		Value: value,
	}

	// Handle wildcards
	if strings.Contains(value, "*") {
		node.Type = NodeWildcard
		switch {
		case strings.HasPrefix(value, "*") && strings.HasSuffix(value, "*"):
			node.MatchType = matchContains
			node.Value = strings.Trim(value, "*")
		case strings.HasPrefix(value, "*"):
			node.MatchType = matchEndsWith
			node.Value = strings.TrimPrefix(value, "*")
		case strings.HasSuffix(value, "*"):
			node.MatchType = matchStartsWith
			node.Value = strings.TrimSuffix(value, "*")
		default:
			node.MatchType = matchContains
		}
	}

	return node, nil
}

func (p *Parser) isJSONBField(field string) bool {
	for _, f := range p.DefaultFields {
		if f.Name == field && f.IsJSONB {
			return true
		}
	}
	return false
}

func (p *Parser) createLogicalNode(op LogicalOperator, parts []string) (*Node, error) {
	node := &Node{
		Type:     NodeLogical,
		Operator: op,
	}

	for _, part := range parts {
		child, err := p.parse(part)
		if err != nil {
			return nil, err
		}
		if child != nil {
			node.Children = append(node.Children, child)
		}
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

func (p *Parser) nodeToSQL(node *Node, paramIndex int) (string, []any, error) {
	if node == nil {
		return "", nil, nil
	}

	switch node.Type {
	case NodeTerm:
		return fmt.Sprintf("%s::text = $%d", node.Field, paramIndex), []any{node.Value}, nil
	case NodeWildcard:
		pattern := wildcardToPattern(node.Value, node.MatchType)
		return fmt.Sprintf("%s::text ILIKE $%d", node.Field, paramIndex), []any{pattern}, nil
	case NodeLogical:
		var parts []string
		var params []any
		currentParam := paramIndex

		for _, child := range node.Children {
			sqlPart, childParams, err := p.nodeToSQL(child, currentParam)
			if err != nil {
				return "", nil, err
			}
			if sqlPart != "" {
				parts = append(parts, sqlPart)
				params = append(params, childParams...)
				currentParam += len(childParams)
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
