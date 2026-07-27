package mql

import (
	"testing"
	"time"
)

// TestParseQuotedValueAsFinalToken guards against a regression where a quoted
// value appearing as the final token of the input caused Parser.Parse to spin
// forever in parseValueFromPos: at EOF the scanner stops advancing its offset,
// so the scanner-advance loop's condition never becomes false (issue #221).
//
// Each case is parsed in a goroutine so the test fails via timeout instead of
// hanging the whole suite if the loop ever regresses.
func TestParseQuotedValueAsFinalToken(t *testing.T) {
	tests := []struct {
		name  string
		input string
		key   string
		value string
	}{
		{"single-char quoted final value", `k:"v"`, "k", "v"},
		{"word quoted final value", `name:"alice"`, "name", "alice"},
		{"quoted final value with space", `k:"hello world"`, "k", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			type parseResult struct {
				expr Expr
				err  error
			}
			done := make(chan parseResult, 1)
			go func() {
				expr, err := NewParser(tt.input).Parse()
				done <- parseResult{expr, err}
			}()

			select {
			case r := <-done:
				if r.err != nil {
					t.Fatalf("Parse(%q) returned error: %v", tt.input, r.err)
				}
				term, ok := r.expr.(*TermExpr)
				if !ok {
					t.Fatalf("Parse(%q) = %T, want *TermExpr", tt.input, r.expr)
				}
				if term.Key != tt.key {
					t.Errorf("Parse(%q) key = %q, want %q", tt.input, term.Key, tt.key)
				}
				if term.Value != tt.value {
					t.Errorf("Parse(%q) value = %v, want %q", tt.input, term.Value, tt.value)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("Parse(%q) did not terminate within 2s (infinite loop)", tt.input)
			}
		})
	}
}
