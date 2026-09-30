package wasmshell

import (
	"fmt"
	"strconv"
	"strings"
)

type jqTok struct {
	kind  string // "num", "str", "ident", "var", "field", "format", "op", "eof"
	text  string
	parts []jqStrPart
}

type jqStrPart struct {
	lit  string
	expr jqNode
}

func jqLex(src string) ([]jqTok, error) {
	var toks []jqTok
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '"':
			parts, end, err := jqLexString(src, i+1)
			if err != nil {
				return nil, err
			}
			toks = append(toks, jqTok{kind: "str", parts: parts})
			i = end
		case isDigit(c):
			j := i
			for j < len(src) && (isDigit(src[j]) || src[j] == '.' || src[j] == 'e' || src[j] == 'E' ||
				((src[j] == '-' || src[j] == '+') && (src[j-1] == 'e' || src[j-1] == 'E'))) {
				j++
			}
			toks = append(toks, jqTok{kind: "num", text: src[i:j]})
			i = j
		case c == '$' || c == '@':
			j := i + 1
			for j < len(src) && (src[j] == '_' || isAlnum(src[j])) {
				j++
			}
			kind := "var"
			if c == '@' {
				kind = "format"
			}
			toks = append(toks, jqTok{kind: kind, text: src[i+1 : j]})
			i = j
		case c == '.' && i+1 < len(src) && (src[i+1] == '_' || isAlpha(src[i+1])):
			j := i + 1
			for j < len(src) && (src[j] == '_' || isAlnum(src[j])) {
				j++
			}
			toks = append(toks, jqTok{kind: "field", text: src[i+1 : j]})
			i = j
		case c == '_' || isAlpha(c):
			j := i
			for j < len(src) && (src[j] == '_' || isAlnum(src[j]) || (src[j] == ':' && j+1 < len(src) && src[j+1] == ':')) {
				j++
			}
			toks = append(toks, jqTok{kind: "ident", text: src[i:j]})
			i = j
		default:
			matched := false
			for _, op := range []string{"|=", "+=", "-=", "*=", "/=", "//=", "..", "==", "!=", "<=", ">=", "//", "?//",
				"|", ",", ".", "[", "]", "(", ")", "{", "}", ":", ";", "+", "-", "*", "/", "%", "<", ">", "?", "="} {
				if strings.HasPrefix(src[i:], op) {
					toks = append(toks, jqTok{kind: "op", text: op})
					i += len(op)
					matched = true
					break
				}
			}
			if !matched {
				return nil, fmt.Errorf("unexpected character %q", string(c))
			}
		}
	}
	return append(toks, jqTok{kind: "eof"}), nil
}

// jqLexString reads a string literal body, splitting out \(...) parts.
func jqLexString(src string, i int) ([]jqStrPart, int, error) {
	var parts []jqStrPart
	var lit strings.Builder
	for i < len(src) {
		c := src[i]
		if c == '"' {
			parts = append(parts, jqStrPart{lit: lit.String()})
			return parts, i + 1, nil
		}
		if c != '\\' || i+1 >= len(src) {
			lit.WriteByte(c)
			i++
			continue
		}
		n := src[i+1]
		switch n {
		case '(':
			depth, j := 1, i+2
			for j < len(src) && depth > 0 {
				switch src[j] {
				case '(':
					depth++
				case ')':
					depth--
				case '"':
					_, end, err := jqLexString(src, j+1)
					if err != nil {
						return nil, 0, err
					}
					j = end - 1
				}
				j++
			}
			if depth != 0 {
				return nil, 0, fmt.Errorf("unterminated string interpolation")
			}
			node, err := parseJq(src[i+2 : j-1])
			if err != nil {
				return nil, 0, err
			}
			parts = append(parts, jqStrPart{lit: lit.String()}, jqStrPart{expr: node})
			lit.Reset()
			i = j
			continue
		case 'n':
			lit.WriteByte('\n')
		case 't':
			lit.WriteByte('\t')
		case 'r':
			lit.WriteByte('\r')
		case 'b':
			lit.WriteByte('\b')
		case 'f':
			lit.WriteByte('\f')
		case 'u':
			if i+6 <= len(src) {
				if v, err := strconv.ParseUint(src[i+2:i+6], 16, 32); err == nil {
					lit.WriteRune(rune(v)) //nolint:gosec // G115: four hex digits fit in a rune
					i += 6
					continue
				}
			}
			lit.WriteString(`\u`)
		default:
			lit.WriteByte(n)
		}
		i += 2
	}
	return nil, 0, fmt.Errorf("unterminated string")
}
