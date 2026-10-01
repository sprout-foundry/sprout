package wasmshell

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// globParts turns one expanded field into words: pathname expansion when
// an unquoted part has glob characters and something matches, otherwise
// the literal text.
func globParts(parts []wordPart) []string {
	var lit, pat strings.Builder
	glob := false
	for _, p := range parts {
		lit.WriteString(p.text)
		if p.quoted {
			for _, r := range p.text {
				if strings.ContainsRune(`*?[\`, r) {
					pat.WriteByte('\\')
				}
				pat.WriteRune(r)
			}
			continue
		}
		if strings.ContainsAny(p.text, "*?[") {
			glob = true
		}
		pat.WriteString(p.text)
	}
	if glob {
		if matches := globPath(pat.String()); len(matches) > 0 {
			return matches
		}
	}
	return []string{lit.String()}
}

// globPath expands a pattern the way the shell does: relative patterns
// yield relative paths and dotfiles need an explicit leading dot.
func globPath(pattern string) []string {
	abs := pattern
	cwd, _ := os.Getwd()
	if !filepath.IsAbs(pattern) {
		abs = filepath.Join(cwd, pattern)
	}
	matches, err := filepath.Glob(abs)
	if err != nil || len(matches) == 0 {
		return nil
	}
	hiddenOK := strings.HasPrefix(filepath.Base(pattern), ".")
	var out []string
	for _, m := range matches {
		if !hiddenOK && strings.HasPrefix(filepath.Base(m), ".") {
			continue
		}
		if !filepath.IsAbs(pattern) {
			if rel, err := filepath.Rel(cwd, m); err == nil {
				m = rel
				if strings.HasPrefix(pattern, "./") {
					m = "./" + m
				}
			}
		}
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// globToRegexp converts a shell glob (no anchors) to regexp syntax.
func globToRegexp(g string) string {
	var b strings.Builder
	rs := []rune(g)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '[':
			j := i + 1
			if j < len(rs) && (rs[j] == '!' || rs[j] == '^') {
				j++
			}
			if j < len(rs) && rs[j] == ']' {
				j++
			}
			for j < len(rs) && rs[j] != ']' {
				j++
			}
			if j >= len(rs) {
				b.WriteString(`\[`)
				continue
			}
			class := string(rs[i+1 : j])
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i = j
		case '\\':
			if i+1 < len(rs) {
				i++
				b.WriteString(regexp.QuoteMeta(string(rs[i])))
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}

func globMatch(glob, s string) bool {
	re, err := regexp.Compile("^(?s:" + globToRegexp(glob) + ")$")
	return err == nil && re.MatchString(s)
}

// unescapeC decodes C-style escapes ($'...', echo -e, printf). With
// stopAtC, \c ends the output (echo -e / printf %b).
func unescapeC(s string, stopAtC bool) string {
	out, _ := unescapeCStop(s, stopAtC)
	return out
}

func unescapeCStop(s string, stopAtC bool) (string, bool) {
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		if rs[i] != '\\' || i+1 >= len(rs) {
			b.WriteRune(rs[i])
			continue
		}
		i++
		switch rs[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		case 'e', 'E':
			b.WriteByte(0x1b)
		case '\\':
			b.WriteByte('\\')
		case '\'':
			b.WriteByte('\'')
		case '"':
			b.WriteByte('"')
		case 'c':
			if stopAtC {
				return b.String(), true
			}
			b.WriteString(`\c`)
		case '0', '1', '2', '3', '4', '5', '6', '7':
			j, v := i, 0
			for j < len(rs) && j < i+3 && rs[j] >= '0' && rs[j] <= '7' {
				v = v*8 + int(rs[j]-'0')
				j++
			}
			b.WriteByte(byte(v))
			i = j - 1
		case 'x':
			j, v := i+1, 0
			for j < len(rs) && j < i+3 && isHex(rs[j]) {
				d, _ := strconv.ParseInt(string(rs[j]), 16, 32)
				v = v*16 + int(d)
				j++
			}
			if j == i+1 {
				b.WriteString(`\x`)
				continue
			}
			b.WriteByte(byte(v))
			i = j - 1
		case 'u', 'U':
			width := 4
			if rs[i] == 'U' {
				width = 8
			}
			j := i + 1
			for j < len(rs) && j < i+1+width && isHex(rs[j]) {
				j++
			}
			if v, err := strconv.ParseInt(string(rs[i+1:j]), 16, 32); err == nil {
				b.WriteRune(rune(v))
				i = j - 1
			} else {
				b.WriteRune('\\')
				b.WriteRune(rs[i])
			}
		default:
			b.WriteRune('\\')
			b.WriteRune(rs[i])
		}
	}
	return b.String(), false
}

func isHex(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
