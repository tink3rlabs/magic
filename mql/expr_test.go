package mql

import "testing"

// TestWildcardMatch covers issue #199: wildcardMatch only checked the first and
// last segment of a pattern, so every literal between two wildcards was silently
// dropped, and the prefix and suffix checks were allowed to overlap the same
// characters of the value.
func TestWildcardMatch(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		pattern string
		want    bool
	}{
		// No wildcard: exact, case-insensitive.
		{"exact match", "alpha", "alpha", true},
		{"exact match is case insensitive", "ALPHA", "alpha", true},
		{"exact mismatch", "alpha", "beta", false},

		// Bare "*" matches anything, including the empty string.
		{"bare star matches any value", "anything", "*", true},
		{"bare star matches empty value", "", "*", true},

		// Prefix and suffix.
		{"prefix wildcard", "admin_read", "*read", true},
		{"prefix wildcard mismatch", "admin_write", "*read", false},
		{"suffix wildcard", "admin_read", "admin*", true},
		{"suffix wildcard mismatch", "user_read", "admin*", false},

		// Middle segments must be respected (#199, first defect).
		{"middle segment present", "admin_read_own", "admin*read*own", true},
		{"middle segment absent", "admin_write_all_own", "admin*read*own", false},
		{"single interior wildcard, middle absent", "aXc", "a*b*c", false},
		{"single interior wildcard, middle present", "aBc", "a*b*c", true},

		// Middle segments must appear in order, not merely be present.
		{"middle segments out of order", "a_c_b_d", "a*b*c*d", false},
		{"middle segments in order", "a_b_c_d", "a*b*c*d", true},

		// Prefix and suffix must not overlap the same characters (#199 follow-up).
		{"prefix and suffix overlap on single char", "a", "a*a", false},
		{"prefix and suffix overlap on substring", "abc", "ab*bc", false},
		{"prefix and suffix adjacent but distinct", "aa", "a*a", true},
		{"prefix and suffix distinct", "abbc", "ab*bc", true},

		// A middle segment must not overlap the suffix either.
		{"middle segment overlaps suffix", "axbc", "a*b*bc", false},

		// Wildcards match the empty string.
		{"star matches empty span", "ac", "a*c", true},
		{"consecutive wildcards", "abc", "a**c", true},

		// Case insensitivity applies to wildcard patterns too.
		{"wildcard match is case insensitive", "ADMIN_READ_OWN", "admin*read*own", true},

		// Empty value.
		{"empty value against literal pattern", "", "a", false},
		{"empty value against prefix pattern", "", "a*", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wildcardMatch(tt.value, tt.pattern); got != tt.want {
				t.Errorf("wildcardMatch(%q, %q) = %v, want %v", tt.value, tt.pattern, got, tt.want)
			}
		})
	}
}

// TestEvalListOperatorsWithNonStringValue covers the second defect in #199: the
// IN and NOT IN branches asserted e.Value.([]string) unguarded and panicked when
// a TermExpr was built programmatically with any other type.
func TestEvalListOperatorsWithNonStringValue(t *testing.T) {
	tests := []struct {
		name  string
		op    string
		value interface{}
		want  bool
	}{
		{"IN with non-list value", "IN", "not-a-list", false},
		{"NOT IN with non-list value", "NOT IN", "not-a-list", false},
		{"IN with nil value", "IN", nil, false},
		{"NOT IN with nil value", "NOT IN", nil, false},
		{"IN with int value", "IN", 42, false},
		{"NOT IN with int value", "NOT IN", 42, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Eval panicked with %v, want a clean false", r)
				}
			}()
			e := &TermExpr{Key: "k", Op: tt.op, Value: tt.value}
			if got := e.Eval(map[string]interface{}{"k": "anything"}); got != tt.want {
				t.Errorf("Eval() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestEvalNotInOnAbsentKey pins the deliberate absent-key rule so the fix to the
// unguarded assertion does not change it: a missing key is treated as not being
// in the list, keeping IN and NOT IN exact duals.
func TestEvalNotInOnAbsentKey(t *testing.T) {
	in := &TermExpr{Key: "k", Op: "IN", Value: []string{"a"}}
	notIn := &TermExpr{Key: "k", Op: "NOT IN", Value: []string{"a"}}
	empty := map[string]interface{}{}

	if in.Eval(empty) {
		t.Errorf("IN on absent key = true, want false")
	}
	if !notIn.Eval(empty) {
		t.Errorf("NOT IN on absent key = false, want true")
	}
}
