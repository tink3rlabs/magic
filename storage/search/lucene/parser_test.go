package lucene

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func TestNewParserFromType(t *testing.T) {
	type args struct {
		model any
	}
	tests := []struct {
		name    string
		args    args
		want    *Parser
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewParserFromType(tt.args.model)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewParserFromType() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewParserFromType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewParser(t *testing.T) {
	type args struct {
		defaultFields []FieldInfo
	}
	tests := []struct {
		name string
		args args
		want *Parser
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewParser(tt.args.defaultFields); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewParser() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getStructFields(t *testing.T) {
	type args struct {
		model any
	}
	tests := []struct {
		name    string
		args    args
		want    []FieldInfo
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getStructFields(tt.args.model)
			if (err != nil) != tt.wantErr {
				t.Fatalf("getStructFields() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getStructFields() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_ParseToMap(t *testing.T) {
	type args struct {
		query string
	}
	tests := []struct {
		name    string
		p       *Parser
		args    args
		want    map[string]any
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.ParseToMap(tt.args.query)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.ParseToMap() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parser.ParseToMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_ParseToSQL(t *testing.T) {
	type args struct {
		query string
	}
	tests := []struct {
		name    string
		p       *Parser
		args    args
		want    string
		want1   []any
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := tt.p.ParseToSQL(tt.args.query)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.ParseToSQL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("Parser.ParseToSQL() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("Parser.ParseToSQL() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestParser_parse(t *testing.T) {
	type args struct {
		query string
	}
	tests := []struct {
		name    string
		p       *Parser
		args    args
		want    *Node
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.parse(tt.args.query)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.parse() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parser.parse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_splitByOperator(t *testing.T) {
	type args struct {
		input string
		op    string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := splitByOperator(tt.args.input, tt.args.op); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitByOperator() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_createImplicitNode(t *testing.T) {
	type args struct {
		term string
	}
	tests := []struct {
		name    string
		p       *Parser
		args    args
		want    *Node
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.createImplicitNode(tt.args.term)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.createImplicitNode() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parser.createImplicitNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_createWildcardNode(t *testing.T) {
	type args struct {
		field string
		value string
	}
	tests := []struct {
		name    string
		p       *Parser
		args    args
		want    *Node
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.createWildcardNode(tt.args.field, tt.args.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.createWildcardNode() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parser.createWildcardNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_formatFieldName(t *testing.T) {
	type args struct {
		fieldName string
	}
	tests := []struct {
		name string
		p    *Parser
		args args
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.formatFieldName(tt.args.fieldName); got != tt.want {
				t.Errorf("Parser.formatFieldName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_createTermNode(t *testing.T) {
	type args struct {
		field string
		value string
	}
	tests := []struct {
		name    string
		p       *Parser
		args    args
		want    *Node
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.createTermNode(tt.args.field, tt.args.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.createTermNode() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parser.createTermNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_createLogicalNode(t *testing.T) {
	type args struct {
		op    LogicalOperator
		parts []string
	}
	tests := []struct {
		name    string
		p       *Parser
		args    args
		want    *Node
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.createLogicalNode(tt.args.op, tt.args.parts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.createLogicalNode() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parser.createLogicalNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_nodeToMap(t *testing.T) {
	type args struct {
		node *Node
	}
	tests := []struct {
		name string
		p    *Parser
		args args
		want map[string]any
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.nodeToMap(tt.args.node); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parser.nodeToMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_nodeToSQL(t *testing.T) {
	type args struct {
		node *Node
	}
	tests := []struct {
		name    string
		p       *Parser
		args    args
		want    string
		want1   []any
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := tt.p.nodeToSQL(tt.args.node)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.nodeToSQL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("Parser.nodeToSQL() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("Parser.nodeToSQL() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestParser_ParseToDynamoDBPartiQL(t *testing.T) {
	type args struct {
		query string
	}
	tests := []struct {
		name    string
		p       *Parser
		args    args
		want    string
		want1   []types.AttributeValue
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := tt.p.ParseToDynamoDBPartiQL(tt.args.query)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.ParseToDynamoDBPartiQL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("Parser.ParseToDynamoDBPartiQL() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("Parser.ParseToDynamoDBPartiQL() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestParser_nodeToDynamoDBPartiQL(t *testing.T) {
	type args struct {
		node *Node
	}
	tests := []struct {
		name    string
		p       *Parser
		args    args
		want    string
		want1   []types.AttributeValue
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := tt.p.nodeToDynamoDBPartiQL(tt.args.node)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.nodeToDynamoDBPartiQL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("Parser.nodeToDynamoDBPartiQL() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("Parser.nodeToDynamoDBPartiQL() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func Test_wildcardToPattern(t *testing.T) {
	type args struct {
		value     string
		matchType MatchType
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wildcardToPattern(tt.args.value, tt.args.matchType); got != tt.want {
				t.Errorf("wildcardToPattern() = %v, want %v", got, tt.want)
			}
		})
	}
}
