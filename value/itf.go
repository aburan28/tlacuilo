package value

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
)

// ITF value encoding, per Apalache ADR-015 ("Informal Trace Format"):
//
//	booleans, strings      -> JSON bool / string
//	integers               -> JSON number, or {"#bigint": "N"} outside
//	                          the safe integer range
//	tuples                 -> {"#tup": [...]}
//	sets                   -> {"#set": [...]}
//	functions              -> {"#map": [[k, v], ...]}
//	records                -> plain JSON objects
//	unserializable values  -> {"#unserializable": "text"}

// maxSafeITFInt is the largest integer ITF encodes as a plain JSON number
// (2^53 - 1, the double-precision safe-integer bound).
var maxSafeITFInt = new(big.Int).SetUint64(1<<53 - 1)

// maxIntervalExpansion bounds Interval-to-#set expansion in ToITF.
const maxIntervalExpansion = 1 << 16

// ToITF converts a Value to its ITF representation, as a tree of
// json-marshalable Go values.
func ToITF(v Value) (any, error) {
	switch v := v.(type) {
	case Bool:
		return bool(v), nil
	case Int:
		if v.X.CmpAbs(maxSafeITFInt) <= 0 {
			return json.Number(v.X.String()), nil
		}
		return map[string]any{"#bigint": v.X.String()}, nil
	case String:
		return string(v), nil
	case ModelValue:
		return string(v), nil
	case Set:
		elems, err := toITFSlice(v.Elems)
		if err != nil {
			return nil, err
		}
		return map[string]any{"#set": elems}, nil
	case Tuple:
		elems, err := toITFSlice(v.Elems)
		if err != nil {
			return nil, err
		}
		return map[string]any{"#tup": elems}, nil
	case Record:
		m := make(map[string]any, len(v.Fields))
		for _, f := range v.Fields {
			fv, err := ToITF(f.V)
			if err != nil {
				return nil, err
			}
			m[f.Name] = fv
		}
		return m, nil
	case Func:
		pairs := make([]any, len(v.Entries))
		for i, e := range v.Entries {
			k, err := ToITF(e.K)
			if err != nil {
				return nil, err
			}
			val, err := ToITF(e.V)
			if err != nil {
				return nil, err
			}
			pairs[i] = []any{k, val}
		}
		return map[string]any{"#map": pairs}, nil
	case Interval:
		size := new(big.Int).Sub(v.Hi, v.Lo)
		if size.Sign() >= 0 && size.Cmp(big.NewInt(maxIntervalExpansion)) > 0 {
			return nil, fmt.Errorf("interval %s too large to expand for ITF", v)
		}
		var elems []any
		for i := new(big.Int).Set(v.Lo); i.Cmp(v.Hi) <= 0; i.Add(i, big.NewInt(1)) {
			e, err := ToITF(Int{new(big.Int).Set(i)})
			if err != nil {
				return nil, err
			}
			elems = append(elems, e)
		}
		if elems == nil {
			elems = []any{}
		}
		return map[string]any{"#set": elems}, nil
	}
	return nil, fmt.Errorf("cannot encode %T as ITF", v)
}

func toITFSlice(vs []Value) ([]any, error) {
	out := make([]any, len(vs))
	for i, v := range vs {
		e, err := ToITF(v)
		if err != nil {
			return nil, err
		}
		out[i] = e
	}
	return out, nil
}

// FromITF converts a decoded ITF JSON tree (as produced by
// encoding/json with UseNumber) into a Value. JSON strings decode as
// String values; ITF does not distinguish strings from model values.
func FromITF(x any) (Value, error) {
	switch x := x.(type) {
	case bool:
		return Bool(x), nil
	case string:
		return String(x), nil
	case json.Number:
		i, ok := new(big.Int).SetString(x.String(), 10)
		if !ok {
			return nil, fmt.Errorf("ITF number %q is not an integer", x)
		}
		return Int{i}, nil
	case float64: // decoded without UseNumber
		i, acc := big.NewFloat(x).Int(nil)
		if acc != big.Exact {
			return nil, fmt.Errorf("ITF number %v is not an integer", x)
		}
		return Int{i}, nil
	case []any:
		// Bare arrays appear in some producers for tuples.
		elems, err := fromITFSlice(x)
		if err != nil {
			return nil, err
		}
		return Tuple{Elems: elems}, nil
	case map[string]any:
		if b, ok := x["#bigint"]; ok {
			s, ok := b.(string)
			if !ok {
				return nil, fmt.Errorf("#bigint value must be a string")
			}
			i, ok2 := new(big.Int).SetString(s, 10)
			if !ok2 {
				return nil, fmt.Errorf("invalid #bigint %q", s)
			}
			return Int{i}, nil
		}
		if t, ok := x["#tup"]; ok {
			arr, ok := t.([]any)
			if !ok {
				return nil, fmt.Errorf("#tup value must be an array")
			}
			elems, err := fromITFSlice(arr)
			if err != nil {
				return nil, err
			}
			return Tuple{Elems: elems}, nil
		}
		if s, ok := x["#set"]; ok {
			arr, ok := s.([]any)
			if !ok {
				return nil, fmt.Errorf("#set value must be an array")
			}
			elems, err := fromITFSlice(arr)
			if err != nil {
				return nil, err
			}
			return NewSet(elems...), nil
		}
		if m, ok := x["#map"]; ok {
			arr, ok := m.([]any)
			if !ok {
				return nil, fmt.Errorf("#map value must be an array of pairs")
			}
			entries := make([]FuncEntry, 0, len(arr))
			for _, p := range arr {
				pair, ok := p.([]any)
				if !ok || len(pair) != 2 {
					return nil, fmt.Errorf("#map entries must be [key, value] pairs")
				}
				k, err := FromITF(pair[0])
				if err != nil {
					return nil, err
				}
				v, err := FromITF(pair[1])
				if err != nil {
					return nil, err
				}
				entries = append(entries, FuncEntry{K: k, V: v})
			}
			return NewFunc(entries...), nil
		}
		if u, ok := x["#unserializable"]; ok {
			s, _ := u.(string)
			return ModelValue(s), nil
		}
		fields := make([]RecField, 0, len(x))
		for name, fv := range x {
			v, err := FromITF(fv)
			if err != nil {
				return nil, err
			}
			fields = append(fields, RecField{Name: name, V: v})
		}
		return NewRecord(fields...), nil
	}
	return nil, fmt.Errorf("cannot decode %T from ITF", x)
}

func fromITFSlice(xs []any) ([]Value, error) {
	out := make([]Value, len(xs))
	for i, x := range xs {
		v, err := FromITF(x)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// MarshalITF renders a Value as ITF JSON.
func MarshalITF(v Value) ([]byte, error) {
	t, err := ToITF(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(t)
}

// UnmarshalITF parses ITF JSON into a Value, preserving big integers.
func UnmarshalITF(data []byte) (Value, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var x any
	if err := dec.Decode(&x); err != nil {
		return nil, err
	}
	return FromITF(x)
}
