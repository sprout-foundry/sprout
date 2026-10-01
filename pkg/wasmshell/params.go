package wasmshell

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// lookup resolves a variable: positional parameters, then shell variables.
func (sh *interp) lookup(name string) (string, bool) {
	if n, err := strconv.Atoi(name); err == nil {
		if n == 0 {
			return sh.name, true
		}
		if n <= len(sh.positional) {
			return sh.positional[n-1], true
		}
		return "", false
	}
	if v, ok := ShellEnv.Vars[name]; ok {
		return v, true
	}
	return os.LookupEnv(name)
}

func opLen(double bool) int {
	if double {
		return 2
	}
	return 1
}

func (sh *interp) special(c string) string {
	switch c {
	case "?":
		return strconv.Itoa(sh.lastExit)
	case "#":
		return strconv.Itoa(len(sh.positional))
	case "@", "*":
		return strings.Join(sh.positional, " ")
	case "$":
		return "4242"
	case "!":
		return ""
	case "-":
		flags := ""
		if sh.errexit {
			flags += "e"
		}
		if sh.nounset {
			flags += "u"
		}
		return flags
	}
	v, _ := sh.lookup(c)
	return v
}

var paramNameRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*|[0-9]+|[?#@*$!\-])`)

// paramExpand evaluates the inside of ${...}.
func (sh *interp) paramExpand(inner string) (string, error) {
	if inner == "#" {
		return sh.special("#"), nil
	}
	if strings.HasPrefix(inner, "#") {
		name := inner[1:]
		if !paramNameRe.MatchString(name) || paramNameRe.FindString(name) != name {
			return "", fmt.Errorf("${%s}: bad substitution", inner)
		}
		v := sh.paramValue(name)
		return strconv.Itoa(utf8.RuneCountInString(v)), nil
	}
	if strings.HasPrefix(inner, "!") {
		return "", errUnsupported("indirect expansion ${!...}")
	}
	name := paramNameRe.FindString(inner)
	if name == "" {
		return "", fmt.Errorf("${%s}: bad substitution", inner)
	}
	rest := inner[len(name):]
	val := sh.paramValue(name)
	_, set := sh.paramSet(name)
	if rest == "" {
		if !set && sh.nounset {
			return "", fmt.Errorf("%s: unbound variable", name)
		}
		return val, nil
	}
	if strings.HasPrefix(rest, "[") {
		return "", errUnsupported("arrays")
	}

	for _, op := range []string{":-", ":=", ":+", ":?", "-", "=", "+", "?"} {
		if !strings.HasPrefix(rest, op) {
			continue
		}
		word := rest[len(op):]
		colon := strings.HasPrefix(op, ":")
		unset := !set || (colon && val == "")
		switch strings.TrimPrefix(op, ":") {
		case "-":
			if unset {
				return sh.expandString(word)
			}
			return val, nil
		case "=":
			if unset {
				v, err := sh.expandString(word)
				if err != nil {
					return "", err
				}
				ShellEnv.Set(name, v)
				return v, nil
			}
			return val, nil
		case "+":
			if unset {
				return "", nil
			}
			return sh.expandString(word)
		case "?":
			if unset {
				msg, _ := sh.expandString(word)
				if msg == "" {
					msg = "parameter null or not set"
				}
				return "", fmt.Errorf("%s: %s", name, msg)
			}
			return val, nil
		}
	}

	switch {
	case strings.HasPrefix(rest, "##"), strings.HasPrefix(rest, "#"):
		longest := strings.HasPrefix(rest, "##")
		pat, err := sh.patternArg(rest[opLen(longest):])
		if err != nil {
			return "", err
		}
		return trimPrefixGlob(val, pat, longest), nil
	case strings.HasPrefix(rest, "%%"), strings.HasPrefix(rest, "%"):
		longest := strings.HasPrefix(rest, "%%")
		pat, err := sh.patternArg(rest[opLen(longest):])
		if err != nil {
			return "", err
		}
		return trimSuffixGlob(val, pat, longest), nil
	case strings.HasPrefix(rest, "/"):
		all := strings.HasPrefix(rest, "//")
		body := strings.TrimPrefix(strings.TrimPrefix(rest, "/"), "/")
		anchor := ""
		if strings.HasPrefix(body, "#") || strings.HasPrefix(body, "%") {
			anchor, body = body[:1], body[1:]
		}
		patRaw, repRaw, _ := strings.Cut(body, "/")
		pat, err := sh.patternArg(patRaw)
		if err != nil {
			return "", err
		}
		rep, err := sh.expandString(repRaw)
		if err != nil {
			return "", err
		}
		return replaceGlob(val, pat, rep, all, anchor), nil
	case rest == "^^":
		return strings.ToUpper(val), nil
	case rest == ",,":
		return strings.ToLower(val), nil
	case rest == "^":
		return upperFirst(val, true), nil
	case rest == ",":
		return upperFirst(val, false), nil
	case strings.HasPrefix(rest, ":"):
		return sh.substring(val, rest[1:])
	}
	return "", fmt.Errorf("${%s}: bad substitution", inner)
}

func (sh *interp) paramValue(name string) string {
	switch name {
	case "?", "#", "@", "*", "$", "!", "-":
		return sh.special(name)
	}
	v, _ := sh.lookup(name)
	return v
}

func (sh *interp) paramSet(name string) (string, bool) {
	switch name {
	case "?", "#", "@", "*", "$", "-":
		return sh.special(name), true
	}
	return sh.lookup(name)
}

// patternArg expands a ${var#pattern} operand into an anchored-free regexp.
func (sh *interp) patternArg(raw string) (string, error) {
	re, err := sh.expandPattern(raw)
	if err != nil {
		return "", err
	}
	s := re.String()
	return strings.TrimSuffix(strings.TrimPrefix(s, "^(?s:"), ")$"), nil
}

func fullMatch(pat, s string) bool {
	re, err := regexp.Compile("^(?s:" + pat + ")$")
	return err == nil && re.MatchString(s)
}

func trimPrefixGlob(val, pat string, longest bool) string {
	idx := runeOffsets(val)
	if longest {
		for k := len(idx) - 1; k >= 0; k-- {
			if fullMatch(pat, val[:idx[k]]) {
				return val[idx[k]:]
			}
		}
		return val
	}
	for _, off := range idx {
		if fullMatch(pat, val[:off]) {
			return val[off:]
		}
	}
	return val
}

func trimSuffixGlob(val, pat string, longest bool) string {
	idx := runeOffsets(val)
	if longest {
		for _, off := range idx {
			if fullMatch(pat, val[off:]) {
				return val[:off]
			}
		}
		return val
	}
	for k := len(idx) - 1; k >= 0; k-- {
		if fullMatch(pat, val[idx[k]:]) {
			return val[:idx[k]]
		}
	}
	return val
}

// runeOffsets lists every rune boundary of s, including 0 and len(s).
func runeOffsets(s string) []int {
	offs := make([]int, 0, len(s)+1)
	for i := range s {
		offs = append(offs, i)
	}
	return append(offs, len(s))
}

func replaceGlob(val, pat, rep string, all bool, anchor string) string {
	switch anchor {
	case "#":
		re, err := regexp.Compile("^(?s:" + pat + ")")
		if err != nil {
			return val
		}
		return re.ReplaceAllLiteralString(val, rep)
	case "%":
		re, err := regexp.Compile("(?s:" + pat + ")$")
		if err != nil {
			return val
		}
		return re.ReplaceAllLiteralString(val, rep)
	}
	re, err := regexp.Compile("(?s:" + pat + ")")
	if err != nil || pat == "" {
		return val
	}
	if all {
		return re.ReplaceAllLiteralString(val, rep)
	}
	loc := re.FindStringIndex(val)
	if loc == nil {
		return val
	}
	return val[:loc[0]] + rep + val[loc[1]:]
}

func upperFirst(s string, upper bool) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return s
	}
	if upper {
		return strings.ToUpper(string(r)) + s[size:]
	}
	return strings.ToLower(string(r)) + s[size:]
}

// substring evaluates ${var:offset:length}; negative values count from
// the end, as in bash.
func (sh *interp) substring(val, spec string) (string, error) {
	rs := []rune(val)
	offStr, lenStr, hasLen := strings.Cut(spec, ":")
	off, err := sh.arith(offStr)
	if err != nil {
		return "", err
	}
	if off < 0 {
		off += int64(len(rs))
		if off < 0 {
			off = 0
		}
	}
	if off > int64(len(rs)) {
		return "", nil
	}
	end := int64(len(rs))
	if hasLen {
		n, err := sh.arith(lenStr)
		if err != nil {
			return "", err
		}
		if n < 0 {
			end += n
		} else {
			end = off + n
		}
		if end > int64(len(rs)) {
			end = int64(len(rs))
		}
		if end < off {
			return "", fmt.Errorf("%s: substring expression < 0", lenStr)
		}
	}
	return string(rs[off:end]), nil
}
