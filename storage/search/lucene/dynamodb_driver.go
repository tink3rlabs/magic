package lucene

import (
	"fmt"
	"regexp"
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
		expr.Like:      dynamoDBLike, // Custom LIKE for DynamoDB functions
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

// RenderPartiQL renders the expression to DynamoDB PartiQL with AttributeValue parameters.
func (d *DynamoDBPartiQLDriver) RenderPartiQL(e *expr.Expression) (string, []types.AttributeValue, error) {
	// Use base rendering with ? placeholders
	str, params, err := d.RenderParam(e)
	if err != nil {
		return "", nil, err
	}

	// Convert params to DynamoDB AttributeValues
	attrValues := make([]types.AttributeValue, len(params))
	for i, param := range params {
		attrValues[i] = &types.AttributeValueMemberS{Value: fmt.Sprintf("%v", param)}
	}

	return str, attrValues, nil
}

// escapePartiQLString escapes a string value for safe use in PartiQL string literals.
// Escapes single quotes by doubling them (PartiQL standard).
func escapePartiQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
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

// unquotePartiQLString safely removes surrounding quotes from a PartiQL string literal.
// Handles already-escaped quotes correctly.
func unquotePartiQLString(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	return s
}

// dynamoDBLike implements LIKE using DynamoDB's begins_with and contains functions.
func dynamoDBLike(left, right string) (string, error) {
	// Validate and escape field name (left)
	safeLeft, err := escapePartiQLIdentifier(left)
	if err != nil {
		return "", fmt.Errorf("invalid field name: %w", err)
	}

	// Extract the raw value from the right side (remove quotes if present)
	rawValue := unquotePartiQLString(right)

	// Analyze pattern for wildcards
	hasPrefix := strings.HasPrefix(rawValue, "%")
	hasSuffix := strings.HasSuffix(rawValue, "%")

	if hasPrefix && hasSuffix {
		// %value% -> contains(field, value)
		value := strings.Trim(rawValue, "%")
		escapedValue := escapePartiQLString(value)
		return fmt.Sprintf("contains(%s, '%s')", safeLeft, escapedValue), nil
	}
	if !hasPrefix && hasSuffix {
		// value% -> begins_with(field, value)
		value := strings.TrimSuffix(rawValue, "%")
		escapedValue := escapePartiQLString(value)
		return fmt.Sprintf("begins_with(%s, '%s')", safeLeft, escapedValue), nil
	}
	if hasPrefix && !hasSuffix {
		// %value -> contains(field, value) (DynamoDB doesn't have ends_with)
		value := strings.TrimPrefix(rawValue, "%")
		escapedValue := escapePartiQLString(value)
		return fmt.Sprintf("contains(%s, '%s')", safeLeft, escapedValue), nil
	}

	// Exact match - escape the value and wrap in quotes
	escapedValue := escapePartiQLString(rawValue)
	return fmt.Sprintf("%s = '%s'", safeLeft, escapedValue), nil
}
