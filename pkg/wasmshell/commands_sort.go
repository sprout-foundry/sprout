package wasmshell

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

func init() {
	CmdRegistry["sort"] = cmdSort
}

type sortKey struct {
	startField, startChar int
	endField, endChar     int
	opts                  sortFlags
	hasOpts               bool
}

type sortFlags struct {
	numeric, general, human, version, reverse, fold, blanks bool
}

type sortSpec struct {
	global sortFlags
	keys   []sortKey
	sep    string
	unique bool
	stable bool
	check  bool
	output string
}

func cmdSort(args []string, stdin string) CmdResult {
	sp := &sortSpec{}
	var files []string
	args = expandClusters(args, "nrufhVbgscMR", "kto")
	for i := 0; i < len(args); i++ {
		a := args[i]
		if name, val, ok := strings.Cut(a, "="); ok && strings.HasPrefix(a, "--") {
			a = name
			args = append(args[:i+1], append([]string{val}, args[i+1:]...)...)
		}
		switch a {
		case "-n", "--numeric-sort":
			sp.global.numeric = true
		case "-g", "--general-numeric-sort":
			sp.global.general = true
		case "-h", "--human-numeric-sort":
			sp.global.human = true
		case "-V", "--version-sort":
			sp.global.version = true
		case "-r", "--reverse":
			sp.global.reverse = true
		case "-f", "--ignore-case":
			sp.global.fold = true
		case "-b", "--ignore-leading-blanks":
			sp.global.blanks = true
		case "-u", "--unique":
			sp.unique = true
		case "-s", "--stable":
			sp.stable = true
		case "-c", "--check":
			sp.check = true
		case "-M", "-R", "--random-sort", "--month-sort":
		case "-k", "--key", "-t", "--field-separator", "-o", "--output":
			if i+1 >= len(args) {
				return CmdResult{Stdout: "", Stderr: "sort: option requires an argument -- '" + strings.TrimLeft(a, "-") + "'\n", ExitCode: 2}
			}
			i++
			switch a {
			case "-k", "--key":
				k, err := parseSortKey(args[i])
				if err != nil {
					return CmdResult{Stdout: "", Stderr: "sort: " + err.Error() + "\n", ExitCode: 2}
				}
				sp.keys = append(sp.keys, k)
			case "-t", "--field-separator":
				sp.sep = unescapeC(args[i], false)
			default:
				sp.output = args[i]
			}
		default:
			if strings.HasPrefix(a, "-") && a != "-" {
				return CmdResult{Stdout: "", Stderr: "sort: invalid option -- '" + strings.TrimLeft(a, "-") + "'\n", ExitCode: 2}
			}
			files = append(files, a)
		}
	}
	input, errRes := readInputs("sort", files, stdin)
	if errRes != nil {
		return *errRes
	}
	lines := splitLines(input)
	cmp := sp.compare
	if sp.check {
		for k := 1; k < len(lines); k++ {
			if c := cmp(lines[k-1], lines[k]); c > 0 || (sp.unique && c == 0) {
				return CmdResult{Stdout: "", Stderr: fmt.Sprintf("sort: -:%d: disorder: %s\n", k+1, lines[k]), ExitCode: 1}
			}
		}
		return CmdResult{}
	}
	sort.SliceStable(lines, func(a, b int) bool { return cmp(lines[a], lines[b]) < 0 })
	if sp.unique {
		var kept []string
		for k, l := range lines {
			if k == 0 || sp.keyCompare(lines[k-1], l) != 0 {
				kept = append(kept, l)
			}
		}
		lines = kept
	}
	out := ""
	if len(lines) > 0 {
		out = strings.Join(lines, "\n") + "\n"
	}
	if sp.output != "" {
		if err := SyncWriteFile(ResolvePath(sp.output), out); err != nil {
			return CmdResult{Stdout: "", Stderr: "sort: " + err.Error() + "\n", ExitCode: 2}
		}
		return CmdResult{}
	}
	return CmdResult{Stdout: out, Stderr: "", ExitCode: 0}
}

// readInputs concatenates the named files ("-" is stdin), or returns
// stdin when there are none.
func readInputs(cmd string, files []string, stdin string) (string, *CmdResult) {
	if len(files) == 0 {
		return stdin, nil
	}
	var b strings.Builder
	for _, f := range files {
		if f == "-" {
			b.WriteString(stdin)
			continue
		}
		data, err := readFileArg(f)
		if err != nil {
			return "", &CmdResult{Stdout: "", Stderr: fmt.Sprintf("%s: %s: %s\n", cmd, f, describeErr(err)), ExitCode: 2}
		}
		b.WriteString(data)
		if len(data) > 0 && !strings.HasSuffix(data, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}

// splitLines splits text into lines, dropping the empty element a final
// newline produces.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func parseSortKey(spec string) (sortKey, error) {
	var k sortKey
	start, end, hasEnd := strings.Cut(spec, ",")
	parsePos := func(p string) (int, int, sortFlags, bool, error) {
		var f sortFlags
		has := false
		digits := strings.TrimRightFunc(p, unicode.IsLetter)
		for _, c := range p[len(digits):] {
			has = true
			switch c {
			case 'n':
				f.numeric = true
			case 'g':
				f.general = true
			case 'h':
				f.human = true
			case 'V':
				f.version = true
			case 'r':
				f.reverse = true
			case 'f':
				f.fold = true
			case 'b':
				f.blanks = true
			}
		}
		fieldStr, charStr, _ := strings.Cut(digits, ".")
		field, err := strconv.Atoi(fieldStr)
		if err != nil || field < 1 {
			return 0, 0, f, has, fmt.Errorf("invalid key: %s", spec)
		}
		ch := 0
		if charStr != "" {
			if ch, err = strconv.Atoi(charStr); err != nil {
				return 0, 0, f, has, fmt.Errorf("invalid key: %s", spec)
			}
		}
		return field, ch, f, has, nil
	}
	var err error
	var f sortFlags
	if k.startField, k.startChar, k.opts, k.hasOpts, err = parsePos(start); err != nil {
		return k, err
	}
	if hasEnd {
		var has bool
		if k.endField, k.endChar, f, has, err = parsePos(end); err != nil {
			return k, err
		}
		if has {
			k.opts, k.hasOpts = f, true
		}
	}
	return k, nil
}

func (sp *sortSpec) fields(line string) []string {
	if sp.sep != "" {
		return strings.Split(line, sp.sep)
	}
	var fs []string
	start := 0
	for i := 1; i <= len(line); i++ {
		if i == len(line) || ((line[i] == ' ' || line[i] == '\t') && line[i-1] != ' ' && line[i-1] != '\t') {
			fs = append(fs, line[start:i])
			start = i
		}
	}
	return fs
}

func (sp *sortSpec) extract(line string, k sortKey) string {
	fs := sp.fields(line)
	if k.startField > len(fs) {
		return ""
	}
	endField := len(fs)
	if k.endField > 0 && k.endField < endField {
		endField = k.endField
	}
	joiner := sp.sep
	parts := fs[k.startField-1 : max(endField, k.startField)]
	s := strings.Join(parts, joiner)
	if k.startChar > 1 && k.startChar-1 <= len(s) {
		s = s[k.startChar-1:]
	}
	return s
}

func (sp *sortSpec) keyCompare(a, b string) int {
	if len(sp.keys) == 0 {
		return compareWith(a, b, sp.global)
	}
	for _, k := range sp.keys {
		f := sp.global
		if k.hasOpts {
			f = k.opts
		}
		if c := compareWith(sp.extract(a, k), sp.extract(b, k), f); c != 0 {
			return c
		}
	}
	return 0
}

func (sp *sortSpec) compare(a, b string) int {
	if c := sp.keyCompare(a, b); c != 0 || sp.stable || sp.unique {
		return c
	}
	c := strings.Compare(a, b)
	if sp.global.reverse {
		c = -c
	}
	return c
}

func compareWith(a, b string, f sortFlags) int {
	if f.blanks || f.numeric || f.general || f.human {
		a, b = strings.TrimLeft(a, " \t"), strings.TrimLeft(b, " \t")
	}
	var c int
	switch {
	case f.numeric, f.general:
		c = compareFloat(leadingNumber(a), leadingNumber(b))
	case f.human:
		c = compareFloat(humanValue(a), humanValue(b))
	case f.version:
		c = compareVersion(a, b)
	case f.fold:
		c = strings.Compare(strings.ToLower(a), strings.ToLower(b))
	default:
		c = strings.Compare(a, b)
	}
	if f.reverse {
		c = -c
	}
	return c
}

func compareFloat(x, y float64) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

func leadingNumber(s string) float64 {
	end := 0
	for end < len(s) && (isDigit(s[end]) || s[end] == '.' || (end == 0 && (s[end] == '-' || s[end] == '+')) || ((s[end] == 'e' || s[end] == 'E') && end > 0)) {
		end++
	}
	for end > 0 {
		if v, err := strconv.ParseFloat(s[:end], 64); err == nil {
			return v
		}
		end--
	}
	return 0
}

func humanValue(s string) float64 {
	v := leadingNumber(s)
	num := strings.TrimLeft(s, "+-0123456789.")
	if num != "" {
		if idx := strings.IndexRune("KMGTPE", unicode.ToUpper(rune(num[0]))); idx >= 0 {
			v *= math.Pow(1024, float64(idx+1))
		}
	}
	return v
}

// compareVersion orders strings with embedded numbers naturally
// (file2 < file10, 1.9 < 1.10).
func compareVersion(a, b string) int {
	for a != "" && b != "" {
		ad, bd := isDigit(a[0]), isDigit(b[0])
		if ad && bd {
			i, j := 0, 0
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			x, _ := strconv.ParseUint(a[:i], 10, 64)
			y, _ := strconv.ParseUint(b[:j], 10, 64)
			if x != y {
				if x < y {
					return -1
				}
				return 1
			}
			a, b = a[i:], b[j:]
			continue
		}
		if a[0] != b[0] {
			if a[0] < b[0] {
				return -1
			}
			return 1
		}
		a, b = a[1:], b[1:]
	}
	return compareFloat(float64(len(a)), float64(len(b)))
}
