package value

import (
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"strings"
)

// FromGo converts a Go value into a TLA+ Value, for recording
// implementation states in traces:
//
//	bool                  -> Bool
//	integers, *big.Int    -> Int
//	string                -> String
//	slices and arrays     -> Tuple (a TLA+ sequence)
//	maps                  -> Func (explicit function)
//	structs               -> Record of the exported fields; a
//	                         `tla:"name"` tag renames a field and
//	                         `tla:"-"` skips it
//	pointers, interfaces  -> the pointed-to value; nil becomes the
//	                         model value NULL
//	Value                 -> passed through unchanged
//
// Floats, channels, and funcs are rejected: they have no TLC value
// representation.
func FromGo(x any) (Value, error) {
	if x == nil {
		return ModelValue("NULL"), nil
	}
	if v, ok := x.(Value); ok {
		return v, nil
	}
	if b, ok := x.(*big.Int); ok {
		if b == nil {
			return ModelValue("NULL"), nil
		}
		return Int{new(big.Int).Set(b)}, nil
	}
	return fromReflect(reflect.ValueOf(x))
}

func fromReflect(rv reflect.Value) (Value, error) {
	switch rv.Kind() {
	case reflect.Bool:
		return Bool(rv.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return NewInt(rv.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return Int{new(big.Int).SetUint64(rv.Uint())}, nil
	case reflect.String:
		return String(rv.String()), nil
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return Tuple{}, nil // nil slice records as the empty sequence
		}
		elems := make([]Value, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			v, err := fromReflect(rv.Index(i))
			if err != nil {
				return nil, fmt.Errorf("index %d: %w", i, err)
			}
			elems[i] = v
		}
		return Tuple{Elems: elems}, nil
	case reflect.Map:
		entries := make([]FuncEntry, 0, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			k, err := fromReflect(iter.Key())
			if err != nil {
				return nil, fmt.Errorf("map key: %w", err)
			}
			v, err := fromReflect(iter.Value())
			if err != nil {
				return nil, fmt.Errorf("map value for %s: %w", k, err)
			}
			entries = append(entries, FuncEntry{K: k, V: v})
		}
		sort.SliceStable(entries, func(i, j int) bool {
			return Compare(entries[i].K, entries[j].K) < 0
		})
		return Func{Entries: entries}, nil
	case reflect.Struct:
		return fromStruct(rv)
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return ModelValue("NULL"), nil
		}
		if v, ok := rv.Interface().(Value); ok {
			return v, nil
		}
		return fromReflect(rv.Elem())
	}
	return nil, fmt.Errorf("cannot represent Go %s as a TLA+ value", rv.Kind())
}

func fromStruct(rv reflect.Value) (Value, error) {
	t := rv.Type()
	var fields []RecField
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Name
		if tag, ok := f.Tag.Lookup("tla"); ok {
			base, _, _ := strings.Cut(tag, ",")
			if base == "-" {
				continue
			}
			if base != "" {
				name = base
			}
		}
		v, err := fromReflect(rv.Field(i))
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", f.Name, err)
		}
		fields = append(fields, RecField{Name: name, V: v})
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("struct %s has no exported fields (TLA+ has no empty record)", t)
	}
	return NewRecord(fields...), nil
}
