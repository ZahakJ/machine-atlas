package obs

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// Value type names, as the catalog declares them and TypeOf reports them.
const (
	TypeInt    = "int"
	TypeFloat  = "float"
	TypeString = "string"
	TypeBool   = "bool"
	TypeTable  = "table"
	TypeNull   = "null"
)

// Normalize converts a value to the canonical in-memory representation used
// everywhere after collection: int64, float64, string, bool, nil, or a table
// as []map[string]any whose cells are themselves canonical scalars.
//
// Collectors may emit int or uint64 for convenience; snapshot decoding yields
// json.Number and []any. Normalizing both to one shape means derived facts
// and lessons see the same Go types whether a snapshot was just collected or
// imported from a file. Integers that fit int64 stay integers, so a value
// written as "2" decodes as int64 2 and re-encodes byte-identically.
func Normalize(v any) (any, error) {
	switch x := v.(type) {
	case nil, string, bool, int64:
		return x, nil
	case int:
		return int64(x), nil
	case int8:
		return int64(x), nil
	case int16:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case uint:
		return fromUint(uint64(x))
	case uint8:
		return int64(x), nil
	case uint16:
		return int64(x), nil
	case uint32:
		return int64(x), nil
	case uint64:
		return fromUint(x)
	case float32:
		return finite(float64(x))
	case float64:
		return finite(x)
	case json.Number:
		if i, err := strconv.ParseInt(string(x), 10, 64); err == nil {
			return i, nil
		}
		f, err := strconv.ParseFloat(string(x), 64)
		if err != nil {
			return nil, fmt.Errorf("number %s out of range", x)
		}
		return finite(f)
	case []map[string]any:
		return normalizeRows(len(x), func(i int) (map[string]any, bool) { return x[i], true })
	case []any:
		return normalizeRows(len(x), func(i int) (map[string]any, bool) {
			m, ok := x[i].(map[string]any)
			return m, ok
		})
	default:
		return nil, fmt.Errorf("unsupported value type %T", v)
	}
}

func fromUint(u uint64) (any, error) {
	if u > math.MaxInt64 {
		return nil, fmt.Errorf("integer %d exceeds int64", u)
	}
	return int64(u), nil
}

func finite(f float64) (any, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, fmt.Errorf("non-finite number %v", f)
	}
	return f, nil
}

func normalizeRows(n int, row func(int) (map[string]any, bool)) (any, error) {
	out := make([]map[string]any, n)
	for i := range n {
		r, ok := row(i)
		if !ok {
			return nil, fmt.Errorf("table row %d is not an object", i)
		}
		cells := make(map[string]any, len(r))
		for k, c := range r {
			nc, err := Normalize(c)
			if err != nil {
				return nil, fmt.Errorf("table row %d column %q: %w", i, k, err)
			}
			if TypeOf(nc) == TypeTable {
				return nil, fmt.Errorf("table row %d column %q: cells must be flat", i, k)
			}
			cells[k] = nc
		}
		out[i] = cells
	}
	return out, nil
}

// TypeOf names the type of a normalized value: "int", "float", "string",
// "bool", "table" or "null". Anything else yields a description of the Go
// type, which never equals a catalog type.
func TypeOf(v any) string {
	switch v.(type) {
	case nil:
		return TypeNull
	case int64:
		return TypeInt
	case float64:
		return TypeFloat
	case string:
		return TypeString
	case bool:
		return TypeBool
	case []map[string]any:
		return TypeTable
	default:
		return fmt.Sprintf("%T", v)
	}
}

// Int returns v as an int64 when it is an integer, or a float64 with no
// fractional part (JSON does not distinguish 2 from 2.0).
func Int(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<63 {
			return int64(x), true
		}
	}
	return 0, false
}

// Float returns v as a float64 when it is any number.
func Float(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}
