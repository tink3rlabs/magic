package mql

import (
	"reflect"
	"testing"
)

// TestParseListPreservesUnquotedValues guards against a regression where
// parseList appended one member per scanner token instead of one per
// comma-delimited value (issue #233). The scanner mode does not treat "-" as
// part of an identifier, so an unquoted UUID, date or semver string was shredded
// into its fragments: the value written into the list never matched, and every
// fragment matched instead.
func TestParseListPreservesUnquotedValues(t *testing.T) {
	tests := []struct {
		name  string
		input string
		key   string
		op    string
		want  []string
	}{
		{
			name:  "unquoted uuid stays one member",
			input: `k IN [3f2504e0-4f89-11d3-9a0c-0305e82c3301]`,
			key:   "k", op: "IN",
			want: []string{"3f2504e0-4f89-11d3-9a0c-0305e82c3301"},
		},
		{
			name:  "two unquoted hyphenated values",
			input: `k IN [2026-08-25, 2026-08-26]`,
			key:   "k", op: "IN",
			want: []string{"2026-08-25", "2026-08-26"},
		},
		{
			name:  "unquoted semver",
			input: `version IN [1.2.3-rc.1]`,
			key:   "version", op: "IN",
			want: []string{"1.2.3-rc.1"},
		},
		{
			name:  "unquoted email",
			input: `email IN [first-last@example.com]`,
			key:   "email", op: "IN",
			want: []string{"first-last@example.com"},
		},
		{
			name:  "quoted values keep working",
			input: `k IN ["a-b", "c-d"]`,
			key:   "k", op: "IN",
			want: []string{"a-b", "c-d"},
		},
		{
			name:  "mixed quoted and unquoted",
			input: `k IN ["a-b", c-d]`,
			key:   "k", op: "IN",
			want: []string{"a-b", "c-d"},
		},
		{
			name:  "plain identifiers unaffected",
			input: `k IN [alpha, beta, gamma]`,
			key:   "k", op: "IN",
			want: []string{"alpha", "beta", "gamma"},
		},
		{
			name:  "single member without hyphen",
			input: `k IN [alpha]`,
			key:   "k", op: "IN",
			want: []string{"alpha"},
		},
		{
			name:  "NOT IN shreds the same way",
			input: `k NOT IN [3f2504e0-4f89-11d3-9a0c-0305e82c3301]`,
			key:   "k", op: "NOT IN",
			want: []string{"3f2504e0-4f89-11d3-9a0c-0305e82c3301"},
		},
		{
			name:  "empty list",
			input: `k IN []`,
			key:   "k", op: "IN",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := NewParser(tt.input).Parse()
			if err != nil {
				t.Fatalf("Parse(%q) returned error: %v", tt.input, err)
			}
			term, ok := expr.(*TermExpr)
			if !ok {
				t.Fatalf("Parse(%q) = %T, want *TermExpr", tt.input, expr)
			}
			if term.Key != tt.key {
				t.Errorf("Parse(%q) key = %q, want %q", tt.input, term.Key, tt.key)
			}
			if term.Op != tt.op {
				t.Errorf("Parse(%q) op = %q, want %q", tt.input, term.Op, tt.op)
			}
			got, ok := term.Value.([]string)
			if !ok {
				t.Fatalf("Parse(%q) value = %T, want []string", tt.input, term.Value)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse(%q) value = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

// TestEvalListDoesNotMatchFragments pins the user-visible consequence of #233:
// a fragment of a list member must not satisfy IN, and the member itself must.
func TestEvalListDoesNotMatchFragments(t *testing.T) {
	const uuid = "3f2504e0-4f89-11d3-9a0c-0305e82c3301"

	expr, err := NewParser(`k IN [` + uuid + `]`).Parse()
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if !expr.Eval(map[string]interface{}{"k": uuid}) {
		t.Errorf("Eval(k=%q) = false, want true (the value written into the list must match)", uuid)
	}
	for _, fragment := range []string{"3f2504e0", "4f89", "9", "-", "11d3"} {
		if expr.Eval(map[string]interface{}{"k": fragment}) {
			t.Errorf("Eval(k=%q) = true, want false (a fragment must not match)", fragment)
		}
	}
}

// TestParseListFollowedByMoreQuery exercises the scanner re-sync that runs after
// a list is read from the raw input. If re-sync overshoots the closing "]", the
// clause after the list is silently dropped and the filter matches more than it
// should, so each case asserts the trailing clause still participates.
func TestParseListFollowedByMoreQuery(t *testing.T) {
	tests := []struct {
		name  string
		input string
		data  map[string]interface{}
		want  bool
	}{
		{"AND after list, both true", `k IN [a-b] AND x:1`, map[string]interface{}{"k": "a-b", "x": "1"}, true},
		{"AND after list, trailing clause false", `k IN [a-b] AND x:1`, map[string]interface{}{"k": "a-b", "x": "2"}, false},
		{"OR after list", `k IN [a-b] OR x:1`, map[string]interface{}{"k": "zz", "x": "1"}, true},
		{"quoted member then AND", `k IN ["a-b", c] AND x:1`, map[string]interface{}{"k": "c", "x": "1"}, true},
		{"grouped list then AND", `(k IN [a-b]) AND x:1`, map[string]interface{}{"k": "a-b", "x": "1"}, true},
		{"NOT IN then AND", `k NOT IN [a-b] AND x:1`, map[string]interface{}{"k": "zz", "x": "1"}, true},
		{"list as right operand", `x:1 AND k IN [a-b]`, map[string]interface{}{"k": "a-b", "x": "1"}, true},
		{"hyphenated date then AND", `k IN [2026-08-25] AND x:1`, map[string]interface{}{"k": "2026-08-25", "x": "1"}, true},
		{"empty list then AND", `k IN [] AND x:1`, map[string]interface{}{"k": "a", "x": "1"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := NewParser(tt.input).Parse()
			if err != nil {
				t.Fatalf("Parse(%q) returned error: %v", tt.input, err)
			}
			if _, isTerm := expr.(*TermExpr); isTerm {
				t.Fatalf("Parse(%q) = *TermExpr, want a binary expression: the clause after the list was dropped", tt.input)
			}
			if got := expr.Eval(tt.data); got != tt.want {
				t.Errorf("Parse(%q).Eval(%v) = %v, want %v", tt.input, tt.data, got, tt.want)
			}
		})
	}
}

// TestParseListRejectsQuoteInsideUnquotedValue pins the parse error for a value
// that opens a quote mid-member. Accepting it would desync the scanner from the
// input and silently truncate the query.
func TestParseListRejectsQuoteInsideUnquotedValue(t *testing.T) {
	for _, input := range []string{`k IN [a"b]`, `k IN [a"b] AND x:1`, `k IN [ok, a"b]`} {
		if _, err := NewParser(input).Parse(); err == nil {
			t.Errorf("Parse(%q) returned no error, want a parse error", input)
		}
	}
}
