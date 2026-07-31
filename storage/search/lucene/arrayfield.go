package lucene

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/grindlemire/go-lucene/pkg/lucene/expr"
)

// This file owns everything that depends on an array field's ELEMENT TYPE:
// how a Go type is classified as multi-valued, and how a bound value is
// validated, normalized and cast for that element.
//
// It is dialect-neutral by intent — both SQLDriver and DynamoDBPartiQLDriver
// consume it — so per-provider rendering stays in the driver files. The one
// deliberate exception is the Postgres cast, which lives in the table below
// rather than in sql_driver.go: every other per-kind fact is already a column
// of that table, and splitting one column out is precisely what let the kind
// lists drift apart before.

// elemSpec is the complete per-kind description of an array element.
//
// One row per supported kind, so adding or removing a kind is a single edit
// and cannot leave the Postgres cast, the bind type and the strconv width
// disagreeing with each other.
type elemSpec struct {
	// pgCast is the Postgres array cast suffix; "" when the element is text.
	//
	// Parameters are always serialized as Go strings (serializeValue
	// stringifies via fmt.Sprintf), so `int[] @> ARRAY[$1]` fails with
	// "operator does not exist: integer[] @> text[]" without an explicit cast.
	//
	// The cast must name the column's ACTUAL element type. `@>` requires
	// exactly matching array types and neither narrowing nor widening rescues
	// a mismatch: `bigint[] @> ARRAY['9999999999']::int[]` errors with "value
	// out of range for type integer", `int[] @> ARRAY['5']::bigint[]` errors
	// with "no operator matches", and `smallint[] @> ARRAY['1']::int[]` errors
	// the same way. Likewise numeric[] does not satisfy a float8[] column.
	//
	// So each kind maps to the natural Postgres array type for that kind — the
	// width that round-trips it exactly, not a convenient superset. Each signed
	// width picks the smallest Postgres integer that holds it; unsigned kinds
	// need one more bit and so step up a width.
	pgCast string

	// bits is the strconv bit size used to range-check the value. 0 for
	// non-numeric kinds, which have no width.
	bits int

	// numeric marks kinds that must NOT be bound as a string: a JSON string
	// never equals a JSON number, and Postgres will not compare text to an
	// integer array.
	numeric bool
}

// arrayElemSpecs maps an array's element kind to how it is handled.
//
// reflect.Uint8 is deliberately absent: isArrayField excludes any slice or
// array of bytes as a scalar blob, so a uint8 element cannot reach this layer.
// A row for it would imply a byte-array code path that does not exist.
//
// A kind with no row is treated as text — bound as a plain string, uncast.
var arrayElemSpecs = map[reflect.Kind]elemSpec{
	reflect.String: {},
	reflect.Bool:   {pgCast: "::boolean[]", numeric: true},

	// Go int is 64-bit; uint32 needs 33 bits, so it too only fits bigint.
	reflect.Int:    {pgCast: "::bigint[]", bits: 64, numeric: true},
	reflect.Int64:  {pgCast: "::bigint[]", bits: 64, numeric: true},
	reflect.Uint:   {pgCast: "::bigint[]", bits: 64, numeric: true},
	reflect.Uint64: {pgCast: "::bigint[]", bits: 64, numeric: true},
	reflect.Uint32: {pgCast: "::bigint[]", bits: 32, numeric: true},

	reflect.Int8:  {pgCast: "::smallint[]", bits: 8, numeric: true},
	reflect.Int16: {pgCast: "::smallint[]", bits: 16, numeric: true},

	reflect.Int32:  {pgCast: "::int[]", bits: 32, numeric: true},
	reflect.Uint16: {pgCast: "::int[]", bits: 16, numeric: true},

	reflect.Float64: {pgCast: "::double precision[]", bits: 64, numeric: true},
	reflect.Float32: {pgCast: "::real[]", bits: 32, numeric: true},
}

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
	return fieldType.Elem().Kind()
}

// arrayElemSpecOf returns the spec for a field's element kind. The zero spec
// (text, no cast, not numeric) is the fallback for anything unlisted.
func arrayElemSpecOf(fieldType reflect.Type) elemSpec {
	return arrayElemSpecs[arrayElemKind(fieldType)]
}

// isNumericArray reports whether a field's elements are numeric or boolean,
// i.e. values that must not be bound as strings.
func isNumericArray(fieldType reflect.Type) bool {
	return arrayElemSpecOf(fieldType).numeric
}

// arrayElemCast returns the Postgres cast suffix for a field's elements.
func arrayElemCast(fieldType reflect.Type) string {
	return arrayElemSpecOf(fieldType).pgCast
}

// normalizeArrayElemValue validates a containment value against the array's
// element type and returns it as the canonical JSON scalar literal every
// provider accepts ("5", "1.5", "true").
//
// Values reach this layer already stringified, so without this check a
// non-numeric value on an integer column reaches the database and fails there —
// a 500 for what is really a malformed filter, which is the whole class of bug
// array support exists to remove.
//
// fieldType is the FIELD's type (e.g. []int), not the element's.
func normalizeArrayElemValue(fieldName string, fieldType reflect.Type, raw string) (string, error) {
	k := arrayElemKind(fieldType)
	bits := arrayElemSpecs[k].bits

	switch k {
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return "", fmt.Errorf("invalid value %q for boolean array field '%s'", raw, fieldName)
		}
		return strconv.FormatBool(b), nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, bits)
		if err != nil {
			return "", fmt.Errorf("invalid value %q for integer array field '%s'", raw, fieldName)
		}
		return strconv.FormatInt(n, 10), nil

	case reflect.Uint, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, bits)
		if err != nil {
			return "", fmt.Errorf("invalid value %q for unsigned integer array field '%s'", raw, fieldName)
		}
		return strconv.FormatUint(n, 10), nil

	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, bits)
		if err != nil {
			return "", fmt.Errorf("invalid value %q for float array field '%s'", raw, fieldName)
		}
		return strconv.FormatFloat(f, 'g', -1, bits), nil

	default:
		return raw, nil
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
