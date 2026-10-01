package wasmshell

import (
	"encoding/base64"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

func (e *jqEnv) stringFn(c *jqCall, in any, arg func(int) (any, error)) ([]any, error) {
	strArg := func(k int) (string, error) {
		v, err := arg(k)
		if err != nil {
			return "", err
		}
		s, ok := v.(string)
		if !ok {
			return "", jqErrf("%s: argument must be a string", c.name)
		}
		return s, nil
	}
	switch c.name {
	case "join":
		arr, ok := in.([]any)
		if !ok {
			return nil, jqErrf("Cannot iterate over %s", jqDescribe(in))
		}
		sep, err := strArg(0)
		if err != nil {
			return nil, err
		}
		parts := make([]string, len(arr))
		for i, v := range arr {
			if v != nil {
				parts[i] = jqToString(v)
			}
		}
		return []any{strings.Join(parts, sep)}, nil
	case "contains", "inside":
		other, err := arg(0)
		if err != nil {
			return nil, err
		}
		if c.name == "inside" {
			return []any{jqContains(other, in)}, nil
		}
		return []any{jqContains(in, other)}, nil
	case "implode":
		arr, _ := in.([]any)
		var b strings.Builder
		for _, v := range arr {
			f, _ := v.(float64)
			b.WriteRune(rune(f))
		}
		return []any{b.String()}, nil
	}
	s, ok := in.(string)
	if !ok {
		if in == nil && (c.name == "ltrimstr" || c.name == "rtrimstr") {
			return []any{nil}, nil
		}
		return nil, jqErrf("%s cannot be used with %s", c.name, jqDescribe(in))
	}
	switch c.name {
	case "ascii_downcase":
		return []any{strings.ToLower(s)}, nil
	case "ascii_upcase":
		return []any{strings.ToUpper(s)}, nil
	case "trim":
		return []any{strings.TrimSpace(s)}, nil
	case "ltrim":
		return []any{strings.TrimLeft(s, " \t\n\r")}, nil
	case "rtrim":
		return []any{strings.TrimRight(s, " \t\n\r")}, nil
	case "explode":
		var out []any
		for _, r := range s {
			out = append(out, float64(r))
		}
		return []any{out}, nil
	}
	a, err := strArg(0)
	if err != nil {
		return nil, err
	}
	switch c.name {
	case "ltrimstr":
		return []any{strings.TrimPrefix(s, a)}, nil
	case "rtrimstr":
		return []any{strings.TrimSuffix(s, a)}, nil
	case "startswith":
		return []any{strings.HasPrefix(s, a)}, nil
	case "endswith":
		return []any{strings.HasSuffix(s, a)}, nil
	case "split":
		return []any{toAnySlice(strings.Split(s, a))}, nil
	case "index", "rindex", "indices":
		var idxs []any
		for i := 0; a != "" && i+len(a) <= len(s); i++ {
			if s[i:i+len(a)] == a {
				idxs = append(idxs, float64(utf8.RuneCountInString(s[:i])))
			}
		}
		switch {
		case c.name == "indices":
			if idxs == nil {
				idxs = []any{}
			}
			return []any{idxs}, nil
		case len(idxs) == 0:
			return []any{nil}, nil
		case c.name == "index":
			return idxs[:1], nil
		}
		return idxs[len(idxs)-1:], nil
	}
	flags := ""
	if len(c.args) > 1 && (c.name == "test" || len(c.args) > 2) {
		k := 1
		if c.name != "test" {
			k = 2
		}
		if f, err := strArg(k); err == nil {
			flags = f
		}
	}
	prefix := ""
	if strings.Contains(flags, "i") {
		prefix += "i"
	}
	if strings.Contains(flags, "x") {
		a = regexp.MustCompile(`\s+`).ReplaceAllString(a, "")
	}
	if prefix != "" {
		a = "(?" + prefix + ")" + a
	}
	re, rerr := regexp.Compile(a)
	if rerr != nil {
		return nil, jqErrf("%s (at offset 0) is not a valid regex", a)
	}
	switch c.name {
	case "test":
		return []any{re.MatchString(s)}, nil
	}
	repl, err := strArg(1)
	if err != nil {
		return nil, err
	}
	if c.name == "gsub" || strings.Contains(flags, "g") {
		return []any{re.ReplaceAllLiteralString(s, repl)}, nil
	}
	loc := re.FindStringIndex(s)
	if loc == nil {
		return []any{s}, nil
	}
	return []any{s[:loc[0]] + repl + s[loc[1]:]}, nil
}

func jqContains(a, b any) bool {
	switch av := a.(type) {
	case string:
		bs, ok := b.(string)
		return ok && strings.Contains(av, bs)
	case []any:
		bv, ok := b.([]any)
		if !ok {
			return false
		}
		for _, x := range bv {
			found := false
			for _, y := range av {
				if jqContains(y, x) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	case *jqObj:
		bo, ok := b.(*jqObj)
		if !ok {
			return false
		}
		for _, k := range bo.keys {
			v, ok := av.m[k]
			if !ok || !jqContains(v, bo.m[k]) {
				return false
			}
		}
		return true
	}
	return jqCompare(a, b) == 0
}

func jqFormatValue(name string, v any) (string, error) {
	switch name {
	case "text":
		return jqToString(v), nil
	case "json":
		return encodeJSON(v, "", false), nil
	case "base64":
		return base64.StdEncoding.EncodeToString([]byte(jqToString(v))), nil
	case "base64d":
		d, err := base64.StdEncoding.DecodeString(jqToString(v))
		if err != nil {
			d, err = base64.RawStdEncoding.DecodeString(jqToString(v))
		}
		if err != nil {
			return "", jqErrf("%s is not valid base64 data", jqDescribe(v))
		}
		return string(d), nil
	case "uri":
		return url.QueryEscape(jqToString(v)), nil
	case "html":
		return strings.NewReplacer("<", "&lt;", ">", "&gt;", "&", "&amp;", "'", "&#39;", `"`, "&quot;").Replace(jqToString(v)), nil
	case "sh":
		if arr, ok := v.([]any); ok {
			parts := make([]string, len(arr))
			for i, x := range arr {
				parts[i] = "'" + strings.ReplaceAll(jqToString(x), "'", `'\''`) + "'"
			}
			return strings.Join(parts, " "), nil
		}
		return "'" + strings.ReplaceAll(jqToString(v), "'", `'\''`) + "'", nil
	case "csv", "tsv":
		arr, ok := v.([]any)
		if !ok {
			return "", jqErrf("%s cannot be %s-formatted, only an array can be", jqDescribe(v), name)
		}
		parts := make([]string, len(arr))
		for i, x := range arr {
			switch t := x.(type) {
			case string:
				if name == "csv" {
					parts[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
				} else {
					parts[i] = strings.NewReplacer("\\", "\\\\", "\t", "\\t", "\n", "\\n", "\r", "\\r").Replace(t)
				}
			case nil:
				parts[i] = ""
			default:
				parts[i] = jqToString(t)
			}
		}
		sep := ","
		if name == "tsv" {
			sep = "\t"
		}
		return strings.Join(parts, sep), nil
	}
	return "", jqErrf("%s is not a valid format", name)
}
