package wasmshell

import (
	"fmt"
	"strconv"
	"strings"
)

type awkTok struct {
	kind string // "num", "str", "re", "name", "func", "kw", "op", "nl", "eof"
	text string
	num  float64
}

var awkKeywords = words("BEGIN", "END", "function", "func", "if", "else", "while", "for", "do", "break", "continue",
	"next", "nextfile", "exit", "return", "delete", "in", "getline", "print", "printf")

var awkBuiltinFuncs = words("length", "substr", "index", "split", "sub", "gsub", "match", "sprintf", "sin", "cos",
	"atan2", "exp", "log", "sqrt", "int", "rand", "srand", "tolower", "toupper", "system", "close", "fflush")

var awkOps = []string{"**=", "^=", "+=", "-=", "*=", "/=", "%=", "==", "<=", ">=", "!=", "++", "--", "&&", "||", ">>", "!~", "**",
	"{", "}", "(", ")", "[", "]", ";", ",", "+", "-", "*", "/", "%", "^", "!", ">", "<", "|", "?", ":", "~", "$", "="}

func awkLex(src string) ([]awkTok, error) {
	var toks []awkTok
	operand := func() bool {
		if len(toks) == 0 {
			return false
		}
		t := toks[len(toks)-1]
		switch t.kind {
		case "num", "str", "name", "re":
			return true
		case "op":
			return t.text == ")" || t.text == "]" || t.text == "$" || t.text == "++" || t.text == "--"
		}
		return false
	}
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '\\' && i+1 < len(src) && src[i+1] == '\n':
			i += 2
		case c == '\n':
			toks = append(toks, awkTok{kind: "nl"})
			i++
		case c == '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '"':
			var b strings.Builder
			j := i + 1
			for ; j < len(src) && src[j] != '"'; j++ {
				if src[j] == '\\' && j+1 < len(src) {
					j++
					switch src[j] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					case 'r':
						b.WriteByte('\r')
					case '\\':
						b.WriteByte('\\')
					case '"':
						b.WriteByte('"')
					case '/':
						b.WriteByte('/')
					default:
						b.WriteByte('\\')
						b.WriteByte(src[j])
					}
					continue
				}
				b.WriteByte(src[j])
			}
			if j >= len(src) {
				return nil, fmt.Errorf("unterminated string")
			}
			toks = append(toks, awkTok{kind: "str", text: b.String()})
			i = j + 1
		case c == '/' && !operand():
			var b strings.Builder
			j := i + 1
			inClass := false
			for ; j < len(src) && (src[j] != '/' || inClass); j++ {
				if src[j] == '\\' && j+1 < len(src) {
					if src[j+1] != '/' {
						b.WriteByte('\\')
					}
					j++
					b.WriteByte(src[j])
					continue
				}
				switch src[j] {
				case '[':
					inClass = true
				case ']':
					inClass = false
				}
				b.WriteByte(src[j])
			}
			if j >= len(src) {
				return nil, fmt.Errorf("unterminated regex")
			}
			toks = append(toks, awkTok{kind: "re", text: b.String()})
			i = j + 1
		case isDigit(c) || (c == '.' && i+1 < len(src) && isDigit(src[i+1])):
			j := i
			for j < len(src) && (isDigit(src[j]) || src[j] == '.' || src[j] == 'e' || src[j] == 'E' ||
				((src[j] == '+' || src[j] == '-') && (src[j-1] == 'e' || src[j-1] == 'E'))) {
				j++
			}
			if strings.HasPrefix(src[i:], "0x") || strings.HasPrefix(src[i:], "0X") {
				j = i + 2
				for j < len(src) && isHex(rune(src[j])) {
					j++
				}
			}
			n, err := strconv.ParseFloat(src[i:j], 64)
			if err != nil {
				v, herr := strconv.ParseInt(src[i:j], 0, 64)
				if herr != nil {
					return nil, fmt.Errorf("bad number %q", src[i:j])
				}
				n = float64(v)
			}
			toks = append(toks, awkTok{kind: "num", num: n, text: src[i:j]})
			i = j
		case c == '_' || isAlpha(c):
			j := i
			for j < len(src) && (src[j] == '_' || isAlnum(src[j])) {
				j++
			}
			w := src[i:j]
			kind := "name"
			switch {
			case awkKeywords[w]:
				kind = "kw"
			case awkBuiltinFuncs[w]:
				kind = "func"
			}
			toks = append(toks, awkTok{kind: kind, text: w})
			i = j
		default:
			matched := false
			for _, op := range awkOps {
				if strings.HasPrefix(src[i:], op) {
					toks = append(toks, awkTok{kind: "op", text: op})
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
	return append(toks, awkTok{kind: "eof"}), nil
}
