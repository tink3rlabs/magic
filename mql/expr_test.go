package mql

import "testing"

func TestBinaryExpr_Eval(t *testing.T) {
	type args struct {
		input map[string]interface{}
	}
	tests := []struct {
		name string
		e    *BinaryExpr
		args args
		want bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.Eval(tt.args.input); got != tt.want {
				t.Errorf("BinaryExpr.Eval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNotExpr_Eval(t *testing.T) {
	type args struct {
		input map[string]interface{}
	}
	tests := []struct {
		name string
		e    *NotExpr
		args args
		want bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.Eval(tt.args.input); got != tt.want {
				t.Errorf("NotExpr.Eval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGroupExpr_Eval(t *testing.T) {
	type args struct {
		input map[string]interface{}
	}
	tests := []struct {
		name string
		e    *GroupExpr
		args args
		want bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.Eval(tt.args.input); got != tt.want {
				t.Errorf("GroupExpr.Eval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTermExpr_Eval(t *testing.T) {
	type args struct {
		input map[string]interface{}
	}
	tests := []struct {
		name string
		e    *TermExpr
		args args
		want bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.Eval(tt.args.input); got != tt.want {
				t.Errorf("TermExpr.Eval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_wildcardMatch(t *testing.T) {
	type args struct {
		value   string
		pattern string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wildcardMatch(tt.args.value, tt.args.pattern); got != tt.want {
				t.Errorf("wildcardMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_listContains(t *testing.T) {
	type args struct {
		list []string
		val  string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := listContains(tt.args.list, tt.args.val); got != tt.want {
				t.Errorf("listContains() = %v, want %v", got, tt.want)
			}
		})
	}
}
