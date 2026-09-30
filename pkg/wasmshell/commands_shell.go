package wasmshell

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func init() {
	CmdRegistry["true"] = func([]string, string) CmdResult { return CmdResult{} }
	CmdRegistry[":"] = CmdRegistry["true"]
	CmdRegistry["false"] = func([]string, string) CmdResult { return CmdResult{ExitCode: 1} }
	CmdRegistry["seq"] = cmdSeq
	CmdRegistry["sleep"] = cmdSleep
	CmdRegistry["printenv"] = cmdPrintenv
}

func cmdClear(args []string, stdin string) CmdResult {
	return CmdResult{"\x1b[H\x1b[2J", "", 0}
}

func cmdHelp(args []string, stdin string) CmdResult {
	var out strings.Builder
	out.WriteString("sprout-wasm shell commands:\n\n")
	out.WriteString("Files:    ls cd pwd cat mkdir rm rmdir cp mv touch tree find stat du\n")
	out.WriteString("          readlink realpath basename dirname mktemp tee\n")
	out.WriteString("Text:     grep rg sed awk cut tr sort uniq wc head tail rev tac nl\n")
	out.WriteString("          paste column diff jq base64 md5sum sha1sum sha256sum\n")
	out.WriteString("          sha512sum printf echo seq\n")
	out.WriteString("Shell:    test [ [[ true false read set export unset local eval\n")
	out.WriteString("          source sh bash xargs env command type which time sleep\n")
	out.WriteString("          date whoami history clear help\n")
	out.WriteString("git:      read-only subcommands (status, diff, log, show, branch, ...)\n")
	out.WriteString("\nShell language: pipes, && || ;, redirections (> >> < 2>&1 &> <<EOF <<<),\n")
	out.WriteString("if/elif/else, for (incl. C-style), while/until, case, functions, ( ) and\n")
	out.WriteString("{ } groups, $VAR ${VAR:-x} ${VAR#pat} ${VAR/a/b} $(cmd) $((expr)), globs,\n")
	out.WriteString("set -e / -u / -o pipefail. Compilers, package managers, interpreters and\n")
	out.WriteString("network tools are not available here.\n")
	return CmdResult{out.String(), "", 0}
}

func cmdDate(args []string, stdin string) CmdResult {
	now := time.Now()
	format := "+%a %b %e %H:%M:%S %Z %Y"
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-u" || a == "--utc" || a == "--universal":
			now = now.UTC()
		case a == "-I" || a == "--iso-8601" || a == "-Idate":
			format = "+%Y-%m-%d"
		case a == "-Iseconds" || a == "--iso-8601=seconds":
			format = "+%Y-%m-%dT%H:%M:%S%:z"
		case a == "-R" || a == "--rfc-email":
			format = "+%a, %d %b %Y %H:%M:%S %z"
		case (a == "-d" || a == "--date") && i+1 < len(args):
			i++
			t, ok := parseDateArg(args[i], now)
			if !ok {
				return CmdResult{"", fmt.Sprintf("date: invalid date '%s'\n", args[i]), 1}
			}
			now = t
		case strings.HasPrefix(a, "--date="):
			t, ok := parseDateArg(strings.TrimPrefix(a, "--date="), now)
			if !ok {
				return CmdResult{"", fmt.Sprintf("date: invalid date '%s'\n", a), 1}
			}
			now = t
		case strings.HasPrefix(a, "+"):
			format = a
		}
	}
	return CmdResult{strftime(strings.TrimPrefix(format, "+"), now) + "\n", "", 0}
}

func parseDateArg(s string, now time.Time) (time.Time, bool) {
	if strings.HasPrefix(s, "@") {
		n, err := strconv.ParseInt(s[1:], 10, 64)
		return time.Unix(n, 0).In(now.Location()), err == nil
	}
	switch s {
	case "now", "":
		return now, true
	case "yesterday":
		return now.AddDate(0, 0, -1), true
	case "tomorrow":
		return now.AddDate(0, 0, 1), true
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, now.Location()); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// strftime renders the date(1) format directives.
func strftime(f string, t time.Time) string {
	var b strings.Builder
	for i := 0; i < len(f); i++ {
		if f[i] != '%' || i+1 >= len(f) {
			b.WriteByte(f[i])
			continue
		}
		i++
		c := f[i]
		if c == ':' && i+1 < len(f) && f[i+1] == 'z' {
			i++
			b.WriteString(t.Format("-07:00"))
			continue
		}
		switch c {
		case 'Y':
			b.WriteString(t.Format("2006"))
		case 'y':
			b.WriteString(t.Format("06"))
		case 'm':
			b.WriteString(t.Format("01"))
		case 'd':
			b.WriteString(t.Format("02"))
		case 'e':
			fmt.Fprintf(&b, "%2d", t.Day())
		case 'H':
			b.WriteString(t.Format("15"))
		case 'I':
			b.WriteString(t.Format("03"))
		case 'M':
			b.WriteString(t.Format("04"))
		case 'S':
			b.WriteString(t.Format("05"))
		case 'N':
			fmt.Fprintf(&b, "%09d", t.Nanosecond())
		case 'p':
			b.WriteString(t.Format("PM"))
		case 'a':
			b.WriteString(t.Format("Mon"))
		case 'A':
			b.WriteString(t.Format("Monday"))
		case 'b', 'h':
			b.WriteString(t.Format("Jan"))
		case 'B':
			b.WriteString(t.Format("January"))
		case 'j':
			fmt.Fprintf(&b, "%03d", t.YearDay())
		case 'u':
			wd := int(t.Weekday())
			if wd == 0 {
				wd = 7
			}
			b.WriteString(strconv.Itoa(wd))
		case 'w':
			b.WriteString(strconv.Itoa(int(t.Weekday())))
		case 'Z':
			b.WriteString(t.Format("MST"))
		case 'z':
			b.WriteString(t.Format("-0700"))
		case 's':
			b.WriteString(strconv.FormatInt(t.Unix(), 10))
		case 'F':
			b.WriteString(t.Format("2006-01-02"))
		case 'T':
			b.WriteString(t.Format("15:04:05"))
		case 'D':
			b.WriteString(t.Format("01/02/06"))
		case 'R':
			b.WriteString(t.Format("15:04"))
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(c)
		}
	}
	return b.String()
}

func cmdWhoami(args []string, stdin string) CmdResult {
	return CmdResult{ShellEnv.Get("USER") + "\n", "", 0}
}

func cmdEnvCmd(args []string, stdin string) CmdResult {
	var out strings.Builder
	for _, k := range sortedKeys(ShellEnv.All()) {
		fmt.Fprintf(&out, "%s=%s\n", k, ShellEnv.Get(k))
	}
	return CmdResult{out.String(), "", 0}
}

func cmdPrintenv(args []string, _ string) CmdResult {
	if len(args) == 0 {
		return cmdEnvCmd(nil, "")
	}
	var out strings.Builder
	code := 0
	for _, name := range args {
		if v, ok := ShellEnv.Vars[name]; ok {
			out.WriteString(v + "\n")
		} else {
			code = 1
		}
	}
	return CmdResult{out.String(), "", code}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func cmdExport(args []string, stdin string) CmdResult {
	var names []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			names = append(names, a)
		}
	}
	if len(names) == 0 {
		var out strings.Builder
		for _, k := range sortedKeys(ShellEnv.All()) {
			fmt.Fprintf(&out, "declare -x %s=%q\n", k, ShellEnv.Get(k))
		}
		return CmdResult{out.String(), "", 0}
	}
	for _, arg := range names {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			if val, exists := ShellEnv.Vars[arg]; exists {
				os.Setenv(arg, val)
			}
			continue
		}
		ShellEnv.Set(key, value)
	}
	return CmdResult{"", "", 0}
}

func cmdWhich(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "which: missing argument\n", 1}
	}
	var out strings.Builder
	code := 0
	for _, name := range args {
		if strings.HasPrefix(name, "-") {
			continue
		}
		if isBuiltin(name) {
			fmt.Fprintf(&out, "%s: sprout-wasm built-in command\n", name)
			continue
		}
		code = 1
	}
	return CmdResult{out.String(), "", code}
}

func isBuiltin(name string) bool {
	return CmdRegistry[name] != nil || shellBuiltins[name] != nil
}

func cmdHistory(args []string, stdin string) CmdResult {
	var out strings.Builder
	for i, entry := range commandHistory {
		fmt.Fprintf(&out, "%5d  %s\n", i+1, entry)
	}
	return CmdResult{out.String(), "", 0}
}

func cmdPrintln(args []string, stdin string) CmdResult {
	return CmdResult{strings.Join(args, " ") + "\n", "", 0}
}

func cmdBasename(args []string, stdin string) CmdResult {
	suffix := ""
	multi := false
	var names []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-a" || a == "--multiple":
			multi = true
		case a == "-s" && i+1 < len(args):
			i++
			suffix, multi = args[i], true
		case strings.HasPrefix(a, "--suffix="):
			suffix, multi = strings.TrimPrefix(a, "--suffix="), true
		default:
			names = append(names, a)
		}
	}
	if len(names) == 0 {
		return CmdResult{"", "basename: missing operand\n", 1}
	}
	if !multi && len(names) == 2 {
		suffix, names = names[1], names[:1]
	}
	var out strings.Builder
	for _, n := range names {
		base := filepath.Base(n)
		if suffix != "" && base != suffix {
			base = strings.TrimSuffix(base, suffix)
		}
		out.WriteString(base + "\n")
	}
	return CmdResult{out.String(), "", 0}
}

func cmdDirname(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "dirname: missing operand\n", 1}
	}
	var out strings.Builder
	for _, a := range args {
		out.WriteString(filepath.Dir(a) + "\n")
	}
	return CmdResult{out.String(), "", 0}
}

func cmdRealpath(args []string, stdin string) CmdResult {
	var names []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			names = append(names, a)
		}
	}
	if len(names) == 0 {
		return CmdResult{"", "realpath: missing operand\n", 1}
	}
	var out strings.Builder
	for _, n := range names {
		abs, err := filepath.Abs(ResolvePath(n))
		if err != nil {
			return CmdResult{out.String(), fmt.Sprintf("realpath: %s\n", err.Error()), 1}
		}
		out.WriteString(abs + "\n")
	}
	return CmdResult{out.String(), "", 0}
}

func cmdSeq(args []string, _ string) CmdResult {
	sep, width := "\n", false
	var nums []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-s" && i+1 < len(args):
			i++
			sep = unescapeC(args[i], false)
		case strings.HasPrefix(a, "-s") && len(a) > 2:
			sep = unescapeC(a[2:], false)
		case a == "-w":
			width = true
		default:
			nums = append(nums, a)
		}
	}
	vals := make([]float64, 0, 3)
	decimals := 0
	for _, n := range nums {
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return CmdResult{"", fmt.Sprintf("seq: invalid floating point argument: '%s'\n", n), 1}
		}
		if dot := strings.IndexByte(n, '.'); dot >= 0 && len(n)-dot-1 > decimals {
			decimals = len(n) - dot - 1
		}
		vals = append(vals, f)
	}
	first, step, last := 1.0, 1.0, 0.0
	switch len(vals) {
	case 1:
		last = vals[0]
	case 2:
		first, last = vals[0], vals[1]
	case 3:
		first, step, last = vals[0], vals[1], vals[2]
	default:
		return CmdResult{"", "seq: missing operand\n", 1}
	}
	if step == 0 {
		return CmdResult{"", "seq: invalid Zero increment value\n", 1}
	}
	format := func(v float64) string { return strconv.FormatFloat(v, 'f', decimals, 64) }
	pad := 0
	if width {
		pad = max(len(format(first)), len(format(last)))
	}
	var items []string
	for k := 0; ; k++ {
		v := first + float64(k)*step
		if (step > 0 && v > last+1e-9) || (step < 0 && v < last-1e-9) || k > maxLoopIterations {
			break
		}
		s := format(v)
		if pad > 0 && len(s) < pad {
			s = strings.Repeat("0", pad-len(s)) + s
		}
		items = append(items, s)
	}
	if len(items) == 0 {
		return CmdResult{}
	}
	return CmdResult{strings.Join(items, sep) + "\n", "", 0}
}

func cmdSleep(args []string, _ string) CmdResult {
	var total time.Duration
	for _, a := range args {
		unit := time.Second
		switch {
		case strings.HasSuffix(a, "s"):
			a = strings.TrimSuffix(a, "s")
		case strings.HasSuffix(a, "m"):
			a, unit = strings.TrimSuffix(a, "m"), time.Minute
		case strings.HasSuffix(a, "h"):
			a, unit = strings.TrimSuffix(a, "h"), time.Hour
		case strings.HasSuffix(a, "d"):
			a, unit = strings.TrimSuffix(a, "d"), 24*time.Hour
		}
		f, err := strconv.ParseFloat(a, 64)
		if err != nil || f < 0 || math.IsInf(f, 0) {
			return CmdResult{"", fmt.Sprintf("sleep: invalid time interval '%s'\n", a), 1}
		}
		total += time.Duration(f * float64(unit))
	}
	if len(args) == 0 {
		return CmdResult{"", "sleep: missing operand\n", 1}
	}
	time.Sleep(total)
	return CmdResult{}
}
