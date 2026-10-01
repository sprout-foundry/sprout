package wasmshell

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// jqObj is a JSON object that keeps its keys in input order, as jq does.
type jqObj struct {
	keys []string
	m    map[string]any
}

func newJqObj() *jqObj { return &jqObj{m: map[string]any{}} }

func (o *jqObj) set(k string, v any) {
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

func (o *jqObj) clone() *jqObj {
	c := &jqObj{keys: append([]string(nil), o.keys...), m: make(map[string]any, len(o.m))}
	for k, v := range o.m {
		c.m[k] = v
	}
	return c
}

// decodeJSONStream reads every JSON value in the input.
func decodeJSONStream(data string) ([]any, error) {
	dec := json.NewDecoder(strings.NewReader(data))
	dec.UseNumber()
	var out []any
	for {
		v, err := decodeJSONValue(dec)
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, v)
	}
}

func decodeJSONValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := newJqObj()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				v, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				o.set(k, v)
			}
			_, err := dec.Token()
			return o, err
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			_, err := dec.Token()
			return arr, err
		}
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	return tok, nil
}

func jqNumberString(f float64) string {
	switch {
	case math.IsNaN(f):
		return "null"
	case math.IsInf(f, 1):
		return "1.7976931348623157e+308"
	case math.IsInf(f, -1):
		return "-1.7976931348623157e+308"
	case f == math.Trunc(f) && math.Abs(f) < 1e17:
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func jqQuote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

// encodeJSON renders a value; indent "" means compact.
func encodeJSON(v any, indent string, sortKeys bool) string {
	var b strings.Builder
	writeJSON(&b, v, indent, sortKeys, 0)
	return b.String()
}

func writeJSON(b *strings.Builder, v any, indent string, sortKeys bool, level int) {
	nl := func(l int) {
		if indent != "" {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(indent, l))
		}
	}
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case float64:
		b.WriteString(jqNumberString(t))
	case string:
		b.WriteString(jqQuote(t))
	case []any:
		if len(t) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteByte('[')
		for i, x := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			nl(level + 1)
			writeJSON(b, x, indent, sortKeys, level+1)
		}
		nl(level)
		b.WriteByte(']')
	case *jqObj:
		if len(t.keys) == 0 {
			b.WriteString("{}")
			return
		}
		keys := t.keys
		if sortKeys {
			keys = append([]string(nil), keys...)
			sort.Strings(keys)
		}
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			nl(level + 1)
			b.WriteString(jqQuote(k))
			b.WriteByte(':')
			if indent != "" {
				b.WriteByte(' ')
			}
			writeJSON(b, t.m[k], indent, sortKeys, level+1)
		}
		nl(level)
		b.WriteByte('}')
	default:
		fmt.Fprintf(b, "%v", t)
	}
}

func jqTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case *jqObj:
		return "object"
	}
	return "unknown"
}

func jqTypeRank(v any) int {
	switch t := v.(type) {
	case nil:
		return 0
	case bool:
		if t {
			return 2
		}
		return 1
	case float64:
		return 3
	case string:
		return 4
	case []any:
		return 5
	}
	return 6
}

// jqCompare orders values the way jq sorts: null, false, true, numbers,
// strings, arrays, objects.
func jqCompare(a, b any) int {
	ra, rb := jqTypeRank(a), jqTypeRank(b)
	if ra != rb {
		return ra - rb
	}
	switch x := a.(type) {
	case float64:
		y := b.(float64)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	case string:
		return strings.Compare(x, b.(string))
	case []any:
		y := b.([]any)
		for i := 0; i < len(x) && i < len(y); i++ {
			if c := jqCompare(x[i], y[i]); c != 0 {
				return c
			}
		}
		return len(x) - len(y)
	case *jqObj:
		y := b.(*jqObj)
		kx, ky := append([]string(nil), x.keys...), append([]string(nil), y.keys...)
		sort.Strings(kx)
		sort.Strings(ky)
		if c := jqCompare(toAnySlice(kx), toAnySlice(ky)); c != 0 {
			return c
		}
		for _, k := range kx {
			if c := jqCompare(x.m[k], y.m[k]); c != 0 {
				return c
			}
		}
	}
	return 0
}

func toAnySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func jqTruthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	}
	return true
}

func jqToString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return encodeJSON(v, "", false)
}
