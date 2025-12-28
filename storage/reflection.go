package storage

import (
	"fmt"
	"reflect"
	"strings"
)

// getValue is a function that uses reflection to extract the value
// of `field` from `item`
// Its useful when writing type-agnostic logic, such as Magic
// storage adapters that accept `any` type.
func GetValue(item any, field string) (any, error) {
	val := reflect.ValueOf(item)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct fot %s", val.Kind())
	}

	for fIdx := range val.NumField() {
		f := val.Type().Field(fIdx)
		if strings.EqualFold(f.Name, field) || strings.EqualFold(strings.Split(f.Tag.Get("json"), ",")[0], field) {
			return val.Field(fIdx).Interface(), nil
		}
	}
	return nil, nil
}

// typeName is a function that uses reflection to resolve the
// `item` type name.
// It is useful when trying to match between the object passed to
// a storage adapter, and the target table with which it interacts.
func typeName(item any) string {
	typeOf := reflect.TypeOf(item)
	valueOf := reflect.ValueOf(item)
	if valueOf.Kind() == reflect.Pointer {
		elemVal := valueOf.Elem()
		if elemVal.Kind() == reflect.Slice {
			sliceType := elemVal.Type()
			sliceElemType := sliceType.Elem()
			if sliceElemType.Kind() == reflect.Pointer {
				typeOf = sliceElemType.Elem()
			}
		}
	}
	return typeOf.Name()
}
