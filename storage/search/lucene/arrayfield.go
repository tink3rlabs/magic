package lucene

import (
	"fmt"
	"math"
	"reflect"
	"strconv"

	"github.com/grindlemire/go-lucene/pkg/lucene/expr"
)

// This file owns everything that depends on an array field's ELEMENT TYPE:
// how a Go type is classified as multi-valued, and how a bound value is
// validated and normalized into a typed elemValue for that element.
//
// It is dialect-neutral by intent — both SQLDriver and DynamoDBPartiQLDriver
// consume it — so per-provider rendering (including any Postgres cast) stays
// in the driver files.

// arrayElemKind returns the element kind of a multi-valued field, or
// reflect.Invalid when fieldType is not a slice or array.
//
// fieldType is the FIELD's type (e.g. []int), not the element's.
func arrayElemKind(fieldType reflect.Type) reflect.Kind {
	if fieldType == nil {
		return reflect.Invalid
	}
	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	if fieldType.Kind() != reflect.Slice && fieldType.Kind() != reflect.Array {
		return reflect.Invalid
	}
	// Dereference the ELEMENT too, so []*int behaves like []int. isArrayField
	// accepts []*T, and without this the kind is reflect.Pointer, which matches
	// no case in normalizeArrayElemValue: validation is skipped entirely and
	// the value is bound as text, so nums:abc returns no error and nums:5
	// silently matches nothing on the JSON providers.
	elem := fieldType.Elem()
	for elem.Kind() == reflect.Pointer {
		elem = elem.Elem()
	}
	return elem.Kind()
}

// arrayElemBits maps a numeric element kind to the strconv bit size used to
// range-check a bound value. A kind absent from this map has no width to check.
//
// reflect.Uint8 is deliberately absent: isArrayField excludes byte slices and
// arrays as scalar blobs, so a uint8 element cannot reach this layer.
//
// The 64-bit unsigned kinds carry 63, not 64. A Go []uint64 maps to a SIGNED
// bigint[] column, so a value above math.MaxInt64 cannot be stored there and
// Postgres rejects it ("value ... is out of range for type bigint"). Checking
// here turns that 500 into a 400. MySQL and SQLite hold the full uint64 range
// in JSON, so this narrows those two by one bit — the deliberate price of
// validating once instead of per-dialect.
var arrayElemBits = map[reflect.Kind]int{
	reflect.Int:     64,
	reflect.Int64:   64,
	reflect.Uint:    63,
	reflect.Uint64:  63,
	reflect.Uint32:  32,
	reflect.Int8:    8,
	reflect.Int16:   16,
	reflect.Int32:   32,
	reflect.Uint16:  16,
	reflect.Float64: 64,
	reflect.Float32: 32,
}

// elemValue is a validated array element.
//
// Val is deliberately restricted to int64, float64, bool and string so every
// dialect can switch over it exhaustively. Unsigned kinds are range-checked to
// 63 bits and then carried as int64, which keeps them inside database/sql's
// driver.Value set (uint64 with the high bit set is not a valid driver.Value).
//
// Kind is the ORIGINAL element kind, retained for error messages and for
// dialects that need to distinguish e.g. bool from a numeric.
type elemValue struct {
	Kind reflect.Kind
	Val  any
}

// String renders the canonical JSON scalar literal for this element:
// "5", "1.5", "true", or the bare string.
func (v elemValue) String() string {
	switch t := v.Val.(type) {
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		bits := 64
		if v.Kind == reflect.Float32 {
			bits = 32
		}
		return strconv.FormatFloat(t, 'g', -1, bits)
	case bool:
		return strconv.FormatBool(t)
	case string:
		return t
	default:
		return fmt.Sprintf("%v", v.Val)
	}
}

// isStringArray reports whether a field's elements are strings, i.e. the only
// element kind for which substring matching is meaningful.
func isStringArray(fieldType reflect.Type) bool {
	return arrayElemKind(fieldType) == reflect.String
}

// normalizeArrayElemValue validates a containment value against the array's
// element type.
//
// Values reach this layer already stringified, so without this check a
// non-numeric value on an integer column reaches the database and fails there —
// a 500 for what is really a malformed filter, which is the whole class of bug
// array support exists to remove.
//
// fieldType is the FIELD's type (e.g. []int), not the element's.
func normalizeArrayElemValue(fieldName string, fieldType reflect.Type, raw string) (elemValue, error) {
	k := arrayElemKind(fieldType)
	bits := arrayElemBits[k]

	switch k {
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return elemValue{}, fmt.Errorf("invalid value %q for boolean array field '%s'", raw, fieldName)
		}
		return elemValue{Kind: k, Val: b}, nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, bits)
		if err != nil {
			return elemValue{}, fmt.Errorf("invalid value %q for integer array field '%s'", raw, fieldName)
		}
		return elemValue{Kind: k, Val: n}, nil

	case reflect.Uint, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, bits)
		if err != nil {
			return elemValue{}, fmt.Errorf("invalid value %q for unsigned integer array field '%s'", raw, fieldName)
		}
		// Range-checked to at most 63 bits above, so int64 cannot overflow.
		return elemValue{Kind: k, Val: int64(n)}, nil

	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, bits)
		if err != nil {
			return elemValue{}, fmt.Errorf("invalid value %q for float array field '%s'", raw, fieldName)
		}
		// ParseFloat accepts "NaN", "Inf" and "Infinity", none of them JSON
		// numbers. MySQL rejects the containment (ERROR 3141) and DynamoDB will
		// not accept them as an N attribute, so a filter that can never match
		// becomes a 500 on two of the four providers.
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return elemValue{}, fmt.Errorf("invalid value %q for float array field '%s'", raw, fieldName)
		}
		return elemValue{Kind: k, Val: f}, nil

	default:
		return elemValue{Kind: k, Val: raw}, nil
	}
}

// columnName extracts a model field name from the shapes go-lucene uses for a
// column reference: a bare expr.Column, a plain string, or a Literal
// expression wrapping a Column.
//
// Both drivers resolve columns, so the set of shapes is recognised in exactly
// one place — a fourth shape appearing upstream is then a single edit.
func columnName(in any) (string, bool) {
	switch v := in.(type) {
	case expr.Column:
		return string(v), true
	case string:
		return v, true
	case *expr.Expression:
		if v.Op == expr.Literal && v.Left != nil {
			if col, ok := v.Left.(expr.Column); ok {
				return string(col), true
			}
		}
	}
	return "", false
}
