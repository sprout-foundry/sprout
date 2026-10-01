package wasmshell

import (
	"strconv"
	"strings"
)

// braceExpand performs bash brace expansion on a raw word: a{b,c}d and
// {1..5} / {a..e} / {1..10..2}. Quoted text and ${...} are left alone.
func braceExpand(raw string) []string {
	open, close, ok := findBraces(raw)
	if !ok {
		return []string{raw}
	}
	prefix, body, suffix := raw[:open], raw[open+1:close], raw[close+1:]
	alts := splitBraceBody(body)
	if len(alts) < 2 {
		seq, isSeq := braceSequence(body)
		if !isSeq {
			// Not an expansion: keep this brace pair literally and look for
			// another one further along.
			rest := braceExpand(suffix)
			out := make([]string, len(rest))
			for i, r := range rest {
				out[i] = raw[:close+1] + r
			}
			return out
		}
		alts = seq
	}
	var out []string
	suffixes := braceExpand(suffix)
	for _, a := range alts {
		for _, head := range braceExpand(prefix + a) {
			for _, s := range suffixes {
				out = append(out, head+s)
			}
		}
	}
	return out
}

// findBraces locates the first unquoted {...} pair that isn't part of a
// ${...} parameter expansion.
func findBraces(raw string) (int, int, bool) {
	inS, inD := false, false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c == '\\' && !inS:
			i++
		case c == '\'' && !inD:
			inS = !inS
		case c == '"' && !inS:
			inD = !inD
		case c == '$' && !inS && i+1 < len(raw) && (raw[i+1] == '{' || raw[i+1] == '('):
			openCh, closeCh := raw[i+1], byte('}')
			if openCh == '(' {
				closeCh = ')'
			}
			depth := 0
			for j := i + 1; j < len(raw); j++ {
				if raw[j] == openCh {
					depth++
				} else if raw[j] == closeCh {
					depth--
					if depth == 0 {
						i = j
						break
					}
				}
			}
		case c == '{' && !inS && !inD:
			depth := 0
			for j := i; j < len(raw); j++ {
				switch raw[j] {
				case '\\':
					j++
				case '{':
					depth++
				case '}':
					depth--
					if depth == 0 {
						return i, j, true
					}
				}
			}
			return 0, 0, false
		}
	}
	return 0, 0, false
}

func splitBraceBody(body string) []string {
	var parts []string
	depth, start := 0, 0
	inS, inD := false, false
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case c == '\\':
			i++
		case c == '\'' && !inD:
			inS = !inS
		case c == '"' && !inS:
			inD = !inD
		case inS || inD:
		case c == '{':
			depth++
		case c == '}':
			depth--
		case c == ',' && depth == 0:
			parts = append(parts, body[start:i])
			start = i + 1
		}
	}
	return append(parts, body[start:])
}

func braceSequence(body string) ([]string, bool) {
	parts := strings.Split(body, "..")
	if len(parts) != 2 && len(parts) != 3 {
		return nil, false
	}
	step := 1
	if len(parts) == 3 {
		n, err := strconv.Atoi(parts[2])
		if err != nil || n == 0 {
			return nil, false
		}
		step = max(n, -n)
	}
	a, errA := strconv.Atoi(parts[0])
	b, errB := strconv.Atoi(parts[1])
	var out []string
	switch {
	case errA == nil && errB == nil:
		width := 0
		if (strings.HasPrefix(parts[0], "0") && len(parts[0]) > 1) || (strings.HasPrefix(parts[1], "0") && len(parts[1]) > 1) {
			width = max(len(parts[0]), len(parts[1]))
		}
		for i := a; (a <= b && i <= b) || (a > b && i >= b); {
			s := strconv.Itoa(i)
			if len(s) < width {
				s = strings.Repeat("0", width-len(s)) + s
			}
			out = append(out, s)
			if a <= b {
				i += step
			} else {
				i -= step
			}
			if len(out) > maxLoopIterations {
				break
			}
		}
	case len(parts[0]) == 1 && len(parts[1]) == 1:
		x, y := rune(parts[0][0]), rune(parts[1][0])
		if step > 0x7f {
			return nil, false
		}
		delta := rune(step) //nolint:gosec // G115: bounded to ASCII just above
		for r := x; (x <= y && r <= y) || (x > y && r >= y); {
			out = append(out, string(r))
			if x <= y {
				r += delta
			} else {
				r -= delta
			}
		}
	default:
		return nil, false
	}
	return out, true
}
