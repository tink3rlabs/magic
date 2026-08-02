package lucene

import (
	"reflect"
	"testing"

	"github.com/grindlemire/go-lucene/pkg/lucene/expr"
)

// The Postgres cast must name the column's exact element type — `@>` rejects
// both narrowing and widening — so this pins every row of arrayElemSpecs.
// Without it, a wrong cast is invisible until a query fails against a real
// database.
func TestArrayElemCast(t *testing.T) {
	tests := []struct {
		typ  reflect.Type
		want string
	}{
		{reflect.TypeOf([]string{}), ""},
		{reflect.TypeOf([]int{}), "::bigint[]"},
		{reflect.TypeOf([]int64{}), "::bigint[]"},
		{reflect.TypeOf([]uint{}), "::bigint[]"},
		{reflect.TypeOf([]uint64{}), "::bigint[]"},
		{reflect.TypeOf([]uint32{}), "::bigint[]"},
		{reflect.TypeOf([]int8{}), "::smallint[]"},
		{reflect.TypeOf([]int16{}), "::smallint[]"},
		{reflect.TypeOf([]int32{}), "::int[]"},
		{reflect.TypeOf([]uint16{}), "::int[]"},
		{reflect.TypeOf([]float64{}), "::double precision[]"},
		{reflect.TypeOf([]float32{}), "::real[]"},
		{reflect.TypeOf([]bool{}), "::boolean[]"},

		// Not arrays, or excluded from array handling entirely.
		{nil, ""},
		{reflect.TypeOf(""), ""},
		{reflect.TypeOf(0), ""},
		{reflect.TypeOf([]byte{}), ""},

		// A pointer to a slice is still that slice.
		{reflect.TypeOf(&[]int{}), "::bigint[]"},
	}

	for _, tt := range tests {
		name := "nil"
		if tt.typ != nil {
			name = tt.typ.String()
		}
		t.Run(name, func(t *testing.T) {
			if got := arrayElemCast(tt.typ); got != tt.want {
				t.Errorf("arrayElemCast(%s) = %q, want %q", name, got, tt.want)
			}
		})
	}
}

// isNumericArray decides whether a value may be bound as a string, so a wrong
// answer means silently matching nothing rather than an error.
func TestIsNumericArray(t *testing.T) {
	numeric := []reflect.Type{
		reflect.TypeOf([]int{}), reflect.TypeOf([]int8{}), reflect.TypeOf([]int16{}),
		reflect.TypeOf([]int32{}), reflect.TypeOf([]int64{}), reflect.TypeOf([]uint{}),
		reflect.TypeOf([]uint16{}), reflect.TypeOf([]uint32{}), reflect.TypeOf([]uint64{}),
		reflect.TypeOf([]float32{}), reflect.TypeOf([]float64{}), reflect.TypeOf([]bool{}),
	}
	for _, tp := range numeric {
		if !isNumericArray(tp) {
			t.Errorf("isNumericArray(%s) = false, want true", tp)
		}
	}

	// Strings bind as-is; []byte is a scalar blob, never an array.
	for _, tp := range []reflect.Type{reflect.TypeOf([]string{}), reflect.TypeOf([]byte{}), nil} {
		if isNumericArray(tp) {
			t.Errorf("isNumericArray(%v) = true, want false", tp)
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
					t.Fatalf("normalizeArrayElemValue(%q) = %q, want an error", tt.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeArrayElemValue(%q): %v", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("normalizeArrayElemValue(%q) = %q, want %q", tt.raw, got, tt.want)
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
