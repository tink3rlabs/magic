package lucene

import (
	"reflect"
	"testing"
	"time"

	"github.com/grindlemire/go-lucene/pkg/lucene/expr"
)

// The codec needs a real Go value, not a string: MySQL must distinguish JSON
// true from JSON 1, and SQLite binds the value natively.
func TestNormalizeArrayElemValueTypes(t *testing.T) {
	tests := []struct {
		name     string
		typ      reflect.Type
		raw      string
		wantVal  any
		wantKind reflect.Kind
	}{
		{"int64", reflect.TypeOf([]int{}), "5", int64(5), reflect.Int},
		{"negative", reflect.TypeOf([]int64{}), "-5", int64(-5), reflect.Int64},
		{"uint stays int64", reflect.TypeOf([]uint64{}), "5", int64(5), reflect.Uint64},
		{"uint at MaxInt64", reflect.TypeOf([]uint64{}), "9223372036854775807", int64(9223372036854775807), reflect.Uint64},
		{"float", reflect.TypeOf([]float64{}), "1.5", 1.5, reflect.Float64},
		{"float32", reflect.TypeOf([]float32{}), "2.5", 2.5, reflect.Float32},
		{"bool true", reflect.TypeOf([]bool{}), "1", true, reflect.Bool},
		{"bool false", reflect.TypeOf([]bool{}), "FALSE", false, reflect.Bool},
		{"string", reflect.TypeOf([]string{}), "golang", "golang", reflect.String},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeArrayElemValue("f", tt.typ, tt.raw)
			if err != nil {
				t.Fatalf("normalizeArrayElemValue(%q): %v", tt.raw, err)
			}
			if got.Val != tt.wantVal {
				t.Errorf("Val = %#v (%T), want %#v (%T)", got.Val, got.Val, tt.wantVal, tt.wantVal)
			}
			if got.Kind != tt.wantKind {
				t.Errorf("Kind = %v, want %v", got.Kind, tt.wantKind)
			}
		})
	}
}

// Val is restricted to four types so every dialect can switch exhaustively.
func TestElemValueValIsClosedSet(t *testing.T) {
	types := []reflect.Type{
		reflect.TypeOf([]int{}), reflect.TypeOf([]int8{}), reflect.TypeOf([]int16{}),
		reflect.TypeOf([]int32{}), reflect.TypeOf([]int64{}), reflect.TypeOf([]uint{}),
		reflect.TypeOf([]uint16{}), reflect.TypeOf([]uint32{}), reflect.TypeOf([]uint64{}),
		reflect.TypeOf([]float32{}), reflect.TypeOf([]float64{}), reflect.TypeOf([]bool{}),
		reflect.TypeOf([]string{}),
	}
	raws := map[reflect.Kind]string{reflect.Bool: "true", reflect.String: "x"}
	for _, tp := range types {
		raw, ok := raws[arrayElemKind(tp)]
		if !ok {
			raw = "1"
		}
		got, err := normalizeArrayElemValue("f", tp, raw)
		if err != nil {
			t.Fatalf("%s: %v", tp, err)
		}
		switch got.Val.(type) {
		case int64, float64, bool, string:
		default:
			t.Errorf("%s produced Val of type %T; must be int64, float64, bool or string", tp, got.Val)
		}
	}
}

// String() is the canonical JSON scalar literal, kept so the DynamoDB driver
// and today's SQL rendering keep working while the codec lands.
func TestElemValueString(t *testing.T) {
	tests := []struct {
		typ  reflect.Type
		raw  string
		want string
	}{
		{reflect.TypeOf([]int{}), "5", "5"},
		{reflect.TypeOf([]float64{}), "3.0", "3"},
		{reflect.TypeOf([]float64{}), "1.5", "1.5"},
		{reflect.TypeOf([]bool{}), "1", "true"},
		{reflect.TypeOf([]string{}), "golang", "golang"},
	}
	for _, tt := range tests {
		got, err := normalizeArrayElemValue("f", tt.typ, tt.raw)
		if err != nil {
			t.Fatalf("%v", err)
		}
		if got.String() != tt.want {
			t.Errorf("String() = %q, want %q", got.String(), tt.want)
		}
	}
}

// The width is what the table still governs, and a wrong width means a value
// that cannot round-trip the column's element type reaches the database.
func TestArrayElemBits(t *testing.T) {
	tests := []struct {
		typ  reflect.Type
		want int
	}{
		{reflect.TypeOf([]int{}), 64},
		{reflect.TypeOf([]int64{}), 64},
		{reflect.TypeOf([]uint{}), 63},
		{reflect.TypeOf([]uint64{}), 63},
		{reflect.TypeOf([]uint32{}), 32},
		{reflect.TypeOf([]int8{}), 8},
		{reflect.TypeOf([]int16{}), 16},
		{reflect.TypeOf([]int32{}), 32},
		{reflect.TypeOf([]uint16{}), 16},
		{reflect.TypeOf([]float64{}), 64},
		{reflect.TypeOf([]float32{}), 32},
		{reflect.TypeOf([]string{}), 0},
		{reflect.TypeOf([]bool{}), 0},
		{reflect.TypeOf([]byte{}), 0},
		{nil, 0},
	}
	for _, tt := range tests {
		if got := arrayElemBits[arrayElemKind(tt.typ)]; got != tt.want {
			t.Errorf("arrayElemBits[%v] = %d, want %d", tt.typ, got, tt.want)
		}
	}
}

func TestIsStringArray(t *testing.T) {
	if !isStringArray(reflect.TypeOf([]string{})) {
		t.Error("[]string should be a string array")
	}
	for _, tp := range []reflect.Type{
		reflect.TypeOf([]int{}), reflect.TypeOf([]bool{}),
		reflect.TypeOf([]float64{}), reflect.TypeOf([]byte{}), nil,
	} {
		if isStringArray(tp) {
			t.Errorf("isStringArray(%v) = true, want false", tp)
		}
	}
}

// An element kind with no row in arrayElemBits — []time.Time, []struct{} —
// must still produce a usable element rather than being mistaken for a
// numeric one. It falls through normalizeArrayElemValue's default branch and
// stays a string, which every dialect then binds as text.
//
// This replaces a test for isNumericArray, which no longer exists: nothing
// derives "is this numeric" any more, because each dialect encodes the value
// by its Go type. A wrong derivation is now structurally impossible rather
// than merely tested for.
func TestUnlistedElemKindStaysText(t *testing.T) {
	for _, tp := range []reflect.Type{
		reflect.TypeOf([]time.Time{}),
		reflect.TypeOf([]struct{}{}),
	} {
		got, err := normalizeArrayElemValue("f", tp, "2024-01-01T00:00:00Z")
		if err != nil {
			t.Fatalf("normalizeArrayElemValue(%s): %v", tp, err)
		}
		if _, ok := got.Val.(string); !ok {
			t.Errorf("%s produced Val of type %T, want string", tp, got.Val)
		}
	}
}

func TestNormalizeArrayElemValue(t *testing.T) {
	tests := []struct {
		name    string
		typ     reflect.Type
		raw     string
		want    string
		wantErr bool
	}{
		{"int passes through", reflect.TypeOf([]int{}), "5", "5", false},
		{"negative int", reflect.TypeOf([]int{}), "-5", "-5", false},
		{"float canonicalized", reflect.TypeOf([]float64{}), "3.0", "3", false},
		{"float keeps precision", reflect.TypeOf([]float64{}), "1.5", "1.5", false},
		{"bool canonicalized from 1", reflect.TypeOf([]bool{}), "1", "true", false},
		{"bool canonicalized from TRUE", reflect.TypeOf([]bool{}), "TRUE", "true", false},
		{"string untouched", reflect.TypeOf([]string{}), "anything", "anything", false},
		{"non-array untouched", reflect.TypeOf(""), "anything", "anything", false},

		{"int rejects text", reflect.TypeOf([]int{}), "abc", "", true},
		{"int rejects float", reflect.TypeOf([]int{}), "1.5", "", true},
		{"uint rejects negative", reflect.TypeOf([]uint{}), "-1", "", true},
		{"float rejects text", reflect.TypeOf([]float64{}), "abc", "", true},
		{"bool rejects maybe", reflect.TypeOf([]bool{}), "maybe", "", true},

		// The width is range-checked, so a value that cannot round-trip the
		// column's element type is rejected here rather than by the database.
		{"int8 rejects overflow", reflect.TypeOf([]int8{}), "200", "", true},
		{"int8 accepts in range", reflect.TypeOf([]int8{}), "127", "127", false},
		{"uint16 rejects overflow", reflect.TypeOf([]uint16{}), "70000", "", true},

		// Postgres bigint is signed, so MaxInt64 is the last uint that a
		// bigint[] column can hold — one past it, Postgres fails the cast with
		// "value ... is out of range for type bigint".
		{"uint64 accepts MaxInt64", reflect.TypeOf([]uint64{}), "9223372036854775807", "9223372036854775807", false},
		{"uint64 rejects MaxInt64+1", reflect.TypeOf([]uint64{}), "9223372036854775808", "", true},
		{"uint64 rejects MaxUint64", reflect.TypeOf([]uint64{}), "18446744073709551615", "", true},
		{"uint accepts MaxInt64", reflect.TypeOf([]uint{}), "9223372036854775807", "9223372036854775807", false},
		{"uint rejects MaxInt64+1", reflect.TypeOf([]uint{}), "9223372036854775808", "", true},
		// uint32 stays a full 32 bits — it fits in a signed bigint with room.
		{"uint32 accepts MaxUint32", reflect.TypeOf([]uint32{}), "4294967295", "4294967295", false},

		// strconv.ParseFloat accepts these, but they are not JSON numbers:
		// MySQL fails the containment with ERROR 3141 and DynamoDB rejects
		// them as an N attribute.
		{"float64 rejects NaN", reflect.TypeOf([]float64{}), "NaN", "", true},
		{"float64 rejects Inf", reflect.TypeOf([]float64{}), "Inf", "", true},
		{"float64 rejects +Inf", reflect.TypeOf([]float64{}), "+Inf", "", true},
		{"float64 rejects -Inf", reflect.TypeOf([]float64{}), "-Inf", "", true},
		{"float64 rejects Infinity", reflect.TypeOf([]float64{}), "Infinity", "", true},
		{"float32 rejects NaN", reflect.TypeOf([]float32{}), "NaN", "", true},
		{"float32 rejects -Inf", reflect.TypeOf([]float32{}), "-Inf", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeArrayElemValue("field", tt.typ, tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeArrayElemValue(%q) = %q, want an error", tt.raw, got.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeArrayElemValue(%q): %v", tt.raw, err)
			}
			if got.String() != tt.want {
				t.Errorf("normalizeArrayElemValue(%q) = %q, want %q", tt.raw, got.String(), tt.want)
			}
		})
	}
}

// columnName is the single place the go-lucene column shapes are recognised,
// so both drivers agree on what counts as a column reference.
func TestColumnName(t *testing.T) {
	tests := []struct {
		name   string
		in     any
		want   string
		wantOK bool
	}{
		{"expr.Column", expr.Column("tags"), "tags", true},
		{"plain string", "tags", "tags", true},
		{"literal wrapping a column", &expr.Expression{Op: expr.Literal, Left: expr.Column("tags")}, "tags", true},
		{"literal wrapping a value", &expr.Expression{Op: expr.Literal, Left: "golang"}, "", false},
		{"non-literal expression", &expr.Expression{Op: expr.And}, "", false},
		{"unrelated type", 42, "", false},
		{"nil", nil, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := columnName(tt.in)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("columnName(%v) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
