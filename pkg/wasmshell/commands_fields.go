package wasmshell

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
)

func init() {
	CmdRegistry["tr"] = cmdTr
	CmdRegistry["uniq"] = cmdUniq
	CmdRegistry["cut"] = cmdCut
	CmdRegistry["paste"] = cmdPaste
	CmdRegistry["rev"] = cmdRev
	CmdRegistry["tac"] = cmdTac
	CmdRegistry["nl"] = cmdNl
	CmdRegistry["column"] = cmdColumn
	CmdRegistry["fold"] = cmdFold
}

// errIsDirectory reads back as coreutils' "Is a directory".
var errIsDirectory = errors.New("is a directory")

func readFileArg(name string) (string, error) {
	path := ResolvePath(name)
	if info, err := os.Stat(path); err == nil && info.IsDir() { //nolint:gosec // G703: shell commands act on the paths the user names
		return "", errIsDirectory
	}
	data, err := os.ReadFile(path) //nolint:gosec // G703: shell commands act on the paths the user names
	return string(data), err
}

var trClasses = map[string]func(rune) bool{
	"alpha":  unicode.IsLetter,
	"digit":  unicode.IsDigit,
	"alnum":  func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) },
	"upper":  unicode.IsUpper,
	"lower":  unicode.IsLower,
	"space":  unicode.IsSpace,
	"blank":  func(r rune) bool { return r == ' ' || r == '\t' },
	"punct":  unicode.IsPunct,
	"cntrl":  unicode.IsControl,
	"print":  unicode.IsPrint,
	"graph":  func(r rune) bool { return unicode.IsPrint(r) && r != ' ' },
	"xdigit": func(r rune) bool { return isHex(r) },
}

// expandTrSet expands ranges (a-z), classes ([:upper:]) and escapes.
func expandTrSet(set string) []rune {
	rs := []rune(unescapeC(set, false))
	var out []rune
	for i := 0; i < len(rs); i++ {
		if rs[i] == '[' && i+1 < len(rs) && rs[i+1] == ':' {
			if end := strings.Index(string(rs[i:]), ":]"); end > 0 {
				name := string(rs[i+2 : i+end])
				if pred, ok := trClasses[name]; ok {
					for r := rune(0); r < 128; r++ {
						if pred(r) {
							out = append(out, r)
						}
					}
					i += end + 1
					continue
				}
			}
		}
		if i+2 < len(rs) && rs[i+1] == '-' && rs[i+2] >= rs[i] {
			for r := rs[i]; r <= rs[i+2]; r++ {
				out = append(out, r)
			}
			i += 2
			continue
		}
		out = append(out, rs[i])
	}
	return out
}

func cmdTr(args []string, stdin string) CmdResult {
	del, squeeze, complement, truncate := false, false, false, false
	var sets []string
	for _, a := range args {
		if len(a) > 1 && a[0] == '-' && strings.Trim(a[1:], "dscCt") == "" && len(sets) == 0 {
			del = del || strings.Contains(a, "d")
			squeeze = squeeze || strings.Contains(a, "s")
			complement = complement || strings.ContainsAny(a, "cC")
			truncate = truncate || strings.Contains(a, "t")
			continue
		}
		sets = append(sets, a)
	}
	if len(sets) == 0 || (len(sets) < 2 && !del && !squeeze) {
		return CmdResult{"", "tr: missing operand\n", 1}
	}
	set1 := expandTrSet(sets[0])
	in1 := map[rune]bool{}
	for _, r := range set1 {
		in1[r] = true
	}
	inSet1 := func(r rune) bool { return in1[r] != complement }

	var set2 []rune
	if len(sets) > 1 {
		set2 = expandTrSet(sets[1])
	}
	var b strings.Builder
	switch {
	case del:
		for _, r := range stdin {
			if !inSet1(r) {
				b.WriteRune(r)
			}
		}
	case len(set2) > 0:
		trans := map[rune]rune{}
		if !complement {
			if truncate && len(set1) > len(set2) {
				set1 = set1[:len(set2)]
			}
			for k, r := range set1 {
				to := set2[len(set2)-1]
				if k < len(set2) {
					to = set2[k]
				}
				trans[r] = to
			}
		}
		for _, r := range stdin {
			switch {
			case complement && !in1[r]:
				b.WriteRune(set2[len(set2)-1])
			case !complement:
				if to, ok := trans[r]; ok {
					b.WriteRune(to)
				} else {
					b.WriteRune(r)
				}
			default:
				b.WriteRune(r)
			}
		}
	default:
		b.WriteString(stdin)
	}
	result := b.String()
	if squeeze {
		sq := set1
		sqComplement := complement
		if len(set2) > 0 {
			sq, sqComplement = set2, false
		}
		inSq := map[rune]bool{}
		for _, r := range sq {
			inSq[r] = true
		}
		var s strings.Builder
		var prev rune = -1
		for _, r := range result {
			if r == prev && inSq[r] != sqComplement {
				continue
			}
			s.WriteRune(r)
			prev = r
		}
		result = s.String()
	}
	return CmdResult{result, "", 0}
}

func cmdUniq(args []string, stdin string) CmdResult {
	count, dupsOnly, uniqueOnly, fold := false, false, false, false
	skipFields, skipChars := 0, 0
	var files []string
	args = expandClusters(args, "cdui", "fs")
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "-c", "--count":
			count = true
		case "-d", "--repeated":
			dupsOnly = true
		case "-u", "--unique":
			uniqueOnly = true
		case "-i", "--ignore-case":
			fold = true
		case "-f", "-s":
			if i+1 < len(args) {
				i++
				n, _ := strconv.Atoi(args[i])
				if a == "-f" {
					skipFields = n
				} else {
					skipChars = n
				}
			}
		default:
			files = append(files, a)
		}
	}
	input := stdin
	if len(files) > 0 && files[0] != "-" {
		data, err := readFileArg(files[0])
		if err != nil {
			return CmdResult{"", fmt.Sprintf("uniq: %s: %s\n", files[0], describeErr(err)), 1}
		}
		input = data
	}
	key := func(l string) string {
		for f := 0; f < skipFields; f++ {
			l = strings.TrimLeft(l, " \t")
			if i := strings.IndexAny(l, " \t"); i >= 0 {
				l = l[i:]
			} else {
				l = ""
			}
		}
		if skipChars < len(l) {
			l = l[skipChars:]
		} else {
			l = ""
		}
		if fold {
			l = strings.ToLower(l)
		}
		return l
	}
	lines := splitLines(input)
	var out strings.Builder
	for i := 0; i < len(lines); {
		j := i + 1
		for j < len(lines) && key(lines[j]) == key(lines[i]) {
			j++
		}
		n := j - i
		if (!dupsOnly || n > 1) && (!uniqueOnly || n == 1) {
			if count {
				fmt.Fprintf(&out, "%7d %s\n", n, lines[i])
			} else {
				out.WriteString(lines[i] + "\n")
			}
		}
		i = j
	}
	if len(files) > 1 {
		if err := SyncWriteFile(ResolvePath(files[1]), out.String()); err != nil {
			return CmdResult{"", "uniq: " + err.Error() + "\n", 1}
		}
		return CmdResult{}
	}
	return CmdResult{out.String(), "", 0}
}

type cutRange struct{ from, to int }

func parseCutList(list string) ([]cutRange, error) {
	var rs []cutRange
	for _, part := range strings.Split(list, ",") {
		from, to, isRange := strings.Cut(part, "-")
		r := cutRange{1, 1 << 30}
		var err error
		if from != "" {
			if r.from, err = strconv.Atoi(from); err != nil || r.from < 1 {
				return nil, fmt.Errorf("invalid field value '%s'", part)
			}
		}
		if !isRange {
			r.to = r.from
		} else if to != "" {
			if r.to, err = strconv.Atoi(to); err != nil {
				return nil, fmt.Errorf("invalid field range '%s'", part)
			}
		}
		rs = append(rs, r)
	}
	return rs, nil
}

func inCutRanges(rs []cutRange, n int) bool {
	for _, r := range rs {
		if n >= r.from && n <= r.to {
			return true
		}
	}
	return false
}

func cmdCut(args []string, stdin string) CmdResult {
	delim, outDelim := "\t", ""
	var list string
	mode := ""
	onlyDelimited, complement := false, false
	var files []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		takes := func(flag string) (string, bool) {
			if a == flag {
				if i+1 < len(args) {
					i++
					return args[i], true
				}
				return "", true
			}
			if strings.HasPrefix(a, flag) {
				return a[len(flag):], true
			}
			return "", false
		}
		if v, ok := takes("-d"); ok {
			delim = v
			continue
		}
		if strings.HasPrefix(a, "--output-delimiter=") {
			outDelim = strings.TrimPrefix(a, "--output-delimiter=")
			continue
		}
		if strings.HasPrefix(a, "--delimiter=") {
			delim = strings.TrimPrefix(a, "--delimiter=")
			continue
		}
		matched := false
		for _, flag := range []string{"-f", "-c", "-b"} {
			if v, ok := takes(flag); ok {
				mode, list, matched = flag, v, true
				break
			}
		}
		if matched {
			continue
		}
		switch a {
		case "-s", "--only-delimited":
			onlyDelimited = true
		case "--complement":
			complement = true
		default:
			files = append(files, a)
		}
	}
	if mode == "" {
		return CmdResult{"", "cut: you must specify a list of bytes, characters, or fields\n", 1}
	}
	ranges, err := parseCutList(list)
	if err != nil {
		return CmdResult{"", "cut: " + err.Error() + "\n", 1}
	}
	input, errRes := readInputs("cut", files, stdin)
	if errRes != nil {
		errRes.ExitCode = 1
		return *errRes
	}
	if outDelim == "" {
		outDelim = delim
		if mode != "-f" {
			outDelim = ""
		}
	}
	var out strings.Builder
	for _, line := range splitLines(input) {
		if mode == "-f" {
			if !strings.Contains(line, delim) {
				if !onlyDelimited {
					out.WriteString(line + "\n")
				}
				continue
			}
			parts := strings.Split(line, delim)
			var sel []string
			for k, p := range parts {
				if inCutRanges(ranges, k+1) != complement {
					sel = append(sel, p)
				}
			}
			out.WriteString(strings.Join(sel, outDelim) + "\n")
			continue
		}
		units := []rune(line)
		var sel strings.Builder
		for k, r := range units {
			if inCutRanges(ranges, k+1) != complement {
				sel.WriteRune(r)
			}
		}
		out.WriteString(sel.String() + "\n")
	}
	return CmdResult{out.String(), "", 0}
}
