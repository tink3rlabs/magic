package lucene

import (
	"fmt"
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

func NewDynamoDBDriver(fields []FieldInfo) *DynamoDBPartiQLDriver {
	fieldMap := make(map[string]FieldInfo)
	for _, f := range fields {
		fieldMap[f.Name] = f
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
	}
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

// dynamoDBLike implements LIKE using DynamoDB's begins_with and contains functions.
func dynamoDBLike(left, right string) (string, error) {
	// Remove quotes from right side to analyze pattern
	pattern := strings.Trim(right, "'")

	// Replace wildcards for analysis
	hasPrefix := strings.HasPrefix(pattern, "%")
	hasSuffix := strings.HasSuffix(pattern, "%")

	if hasPrefix && hasSuffix {
		// %value% -> contains(field, value)
		value := strings.Trim(pattern, "%")
		return fmt.Sprintf("contains(%s, '%s')", left, value), nil
	} else if !hasPrefix && hasSuffix {
		// value% -> begins_with(field, value)
		value := strings.TrimSuffix(pattern, "%")
		return fmt.Sprintf("begins_with(%s, '%s')", left, value), nil
	} else if hasPrefix && !hasSuffix {
		// %value -> contains(field, value) (DynamoDB doesn't have ends_with)
		value := strings.TrimPrefix(pattern, "%")
		return fmt.Sprintf("contains(%s, '%s')", left, value), nil
	}

	// Exact match
	return fmt.Sprintf("%s = %s", left, right), nil
}
