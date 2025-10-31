package mql

import (
	"reflect"
	"testing"
)

func TestNewParser(t *testing.T) {
	type args struct {
		input string
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
			if got := NewParser(tt.args.input); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewParser() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_next(t *testing.T) {
	tests := []struct {
		name string
		p    *Parser
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.p.next()
		})
	}
}

func TestParser_Parse(t *testing.T) {
	tests := []struct {
		name    string
		p       *Parser
		want    Expr
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.Parse()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.Parse() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parser.Parse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_parseOr(t *testing.T) {
	tests := []struct {
		name    string
		p       *Parser
		want    Expr
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.parseOr()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.parseOr() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parser.parseOr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_parseAnd(t *testing.T) {
	tests := []struct {
		name    string
		p       *Parser
		want    Expr
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.parseAnd()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.parseAnd() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parser.parseAnd() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_parseUnary(t *testing.T) {
	tests := []struct {
		name    string
		p       *Parser
		want    Expr
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.parseUnary()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.parseUnary() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parser.parseUnary() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_parsePrimary(t *testing.T) {
	tests := []struct {
		name    string
		p       *Parser
		want    Expr
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.parsePrimary()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.parsePrimary() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parser.parsePrimary() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_parseTerm(t *testing.T) {
	tests := []struct {
		name    string
		p       *Parser
		want    Expr
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.parseTerm()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.parseTerm() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parser.parseTerm() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParser_parseList(t *testing.T) {
	type args struct {
		key string
		op  string
	}
	tests := []struct {
		name    string
		p       *Parser
		args    args
		want    Expr
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.parseList(tt.args.key, tt.args.op)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parser.parseList() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parser.parseList() = %v, want %v", got, tt.want)
			}
		})
	}
}
