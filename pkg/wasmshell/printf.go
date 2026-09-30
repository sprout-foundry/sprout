package wasmshell

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

func init() {
	CmdRegistry["printf"] = cmdPrintf
}

// cmdEcho implements echo -n/-e/-E. Options are only recognized before
// the first operand, and only when every letter is an echo option.
func cmdEcho(args []string, stdin string) CmdResult {
	newline, escapes := true, false
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if len(a) < 2 || a[0] != '-' || strings.Trim(a[1:], "neE") != "" {
			break
		}
		for _, ch := range a[1:] {
			switch ch {
			case 'n':
				newline = false
			case 'e':
				escapes = true
			case 'E':
				escapes = false
			}
		}
	}
	line := strings.Join(args[i:], " ")
	if escapes {
		var stopped bool
		line, stopped = unescapeCStop(line, true)
		if stopped {
			return CmdResult{Stdout: line, Stderr: "", ExitCode: 0}
		}
	}
	if newline {
		line += "\n"
	}
	return CmdResult{Stdout: line, Stderr: "", ExitCode: 0}
}

// cmdPrintf implements printf FORMAT [ARG...]: the format is reused until
// the arguments run out, as coreutils does.
func cmdPrintf(args []string, _ string) CmdResult {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return CmdResult{Stdout: "", Stderr: "printf: usage: printf format [arguments]\n", ExitCode: 2}
	}
	if args[0] == "-v" {
		return CmdResult{Stdout: "", Stderr: "printf: -v is not supported by the in-browser shell\n", ExitCode: ExitCommandNotFound}
	}
	format, rest := args[0], args[1:]
	var out, errs strings.Builder
	code := 0
	for {
		consumed, stop, err := printfOnce(&out, format, rest)
		if err != "" {
			errs.WriteString("printf: " + err + "\n")
			code = 1
		}
		if stop || consumed == 0 || consumed >= len(rest) {
			break
		}
		rest = rest[consumed:]
	}
	return CmdResult{Stdout: out.String(), Stderr: errs.String(), ExitCode: code}
}

// printfOnce renders format once, returning how many arguments it used.
func printfOnce(out *strings.Builder, format string, args []string) (int, bool, string) {
	used := 0
	errMsg := ""
	next := func() (string, bool) {
		if used < len(args) {
			used++
			return args[used-1], true
		}
		return "", false
	}
	rs := []rune(format)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if c == '\\' && i+1 < len(rs) {
			j := escapeEnd(rs, i)
			s, stop := unescapeCStop(string(rs[i:j]), true)
			out.WriteString(s)
			if stop {
				return used, true, errMsg
			}
			i = j - 1
			continue
		}
		if c != '%' {
			out.WriteRune(c)
			continue
		}
		if i+1 < len(rs) && rs[i+1] == '%' {
			out.WriteByte('%')
			i++
			continue
		}
		j := i + 1
		for j < len(rs) && strings.ContainsRune("-+ #0", rs[j]) {
			j++
		}
		for j < len(rs) && (rs[j] == '*' || (rs[j] >= '0' && rs[j] <= '9') || rs[j] == '.') {
			j++
		}
		if j >= len(rs) {
			out.WriteString(string(rs[i:]))
			break
		}
		spec := string(rs[i+1 : j])
		if strings.Contains(spec, "*") {
			parts := strings.Split(spec, "*")
			var b strings.Builder
			for k, piece := range parts {
				b.WriteString(piece)
				if k < len(parts)-1 {
					a, _ := next()
					n, _ := strconv.Atoi(a)
					b.WriteString(strconv.Itoa(n))
				}
			}
			spec = b.String()
		}
		verb := rs[j]
		arg, _ := next()
		switch verb {
		case 's':
			fmt.Fprintf(out, "%"+spec+"s", arg)
		case 'b':
			s, stop := unescapeCStop(arg, true)
			fmt.Fprintf(out, "%"+spec+"s", s)
			if stop {
				return used, true, errMsg
			}
		case 'q':
			out.WriteString(shellQuote(arg))
		case 'c':
			r, _ := utf8.DecodeRuneInString(arg)
			if arg != "" {
				fmt.Fprintf(out, "%"+spec+"c", r)
			}
		case 'd', 'i':
			n, err := parsePrintfInt(arg)
			if err != nil {
				errMsg = fmt.Sprintf("%s: invalid number", arg)
			}
			fmt.Fprintf(out, "%"+spec+"d", n)
		case 'u':
			n, err := parsePrintfInt(arg)
			if err != nil {
				errMsg = fmt.Sprintf("%s: invalid number", arg)
			}
			fmt.Fprintf(out, "%"+spec+"d", uint64(n)) //nolint:gosec // G115: %u prints negatives in two's complement, as coreutils does
		case 'x', 'X', 'o':
			n, err := parsePrintfInt(arg)
			if err != nil {
				errMsg = fmt.Sprintf("%s: invalid number", arg)
			}
			fmt.Fprintf(out, "%"+spec+string(verb), n)
		case 'f', 'F', 'e', 'E', 'g', 'G':
			f, err := strconv.ParseFloat(strings.TrimSpace(arg), 64)
			if err != nil && arg != "" {
				errMsg = fmt.Sprintf("%s: invalid number", arg)
			}
			fmt.Fprintf(out, "%"+spec+string(verb), f)
		default:
			out.WriteString(string(rs[i : j+1]))
			used--
			if used < 0 {
				used = 0
			}
		}
		i = j
	}
	return used, false, errMsg
}

func parsePrintfInt(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if len(s) > 1 && (s[0] == '\'' || s[0] == '"') {
		r, _ := utf8.DecodeRuneInString(s[1:])
		return int64(r), nil
	}
	if n, err := strconv.ParseInt(s, 0, 64); err == nil {
		return n, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	return int64(f), err
}

// shellQuote quotes s so the shell reads it back as one word.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !strings.ContainsRune("_-./:=+,@", r) && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// escapeEnd returns the index just past the backslash escape at rs[i].
func escapeEnd(rs []rune, i int) int {
	j := i + 2
	var limit int
	switch rs[i+1] {
	case '0', '1', '2', '3', '4', '5', '6', '7':
		limit = i + 4
		for j < len(rs) && j < limit && rs[j] >= '0' && rs[j] <= '7' {
			j++
		}
		return j
	case 'x':
		limit = i + 4
	case 'u':
		limit = i + 6
	case 'U':
		limit = i + 10
	default:
		return j
	}
	for j < len(rs) && j < limit && isHex(rs[j]) {
		j++
	}
	return j
}
