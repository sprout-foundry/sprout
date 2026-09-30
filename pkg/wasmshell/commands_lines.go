package wasmshell

import (
	"fmt"
	"strconv"
	"strings"
)

func cmdPaste(args []string, stdin string) CmdResult {
	delims := []rune{'\t'}
	serial := false
	var files []string
	args = expandClusters(args, "s", "d")
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-s" || a == "--serial":
			serial = true
		case a == "-d" && i+1 < len(args):
			i++
			delims = []rune(unescapeC(args[i], false))
		case strings.HasPrefix(a, "-d") && len(a) > 2:
			delims = []rune(unescapeC(a[2:], false))
		default:
			files = append(files, a)
		}
	}
	if len(files) == 0 {
		files = []string{"-"}
	}
	var cols [][]string
	for _, f := range files {
		data := stdin
		if f != "-" {
			var err error
			if data, err = readFileArg(f); err != nil {
				return CmdResult{Stdout: "", Stderr: fmt.Sprintf("paste: %s: %s\n", f, describeErr(err)), ExitCode: 1}
			}
		}
		cols = append(cols, splitLines(data))
	}
	join := func(items []string) string {
		var b strings.Builder
		for k, it := range items {
			if k > 0 && len(delims) > 0 {
				b.WriteRune(delims[(k-1)%len(delims)])
			}
			b.WriteString(it)
		}
		return b.String()
	}
	var out strings.Builder
	if serial {
		for _, c := range cols {
			out.WriteString(join(c) + "\n")
		}
		return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: 0}
	}
	rows := 0
	for _, c := range cols {
		rows = max(rows, len(c))
	}
	for r := 0; r < rows; r++ {
		row := make([]string, len(cols))
		for k, c := range cols {
			if r < len(c) {
				row[k] = c[r]
			}
		}
		out.WriteString(join(row) + "\n")
	}
	return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: 0}
}

func linesFromArgs(cmd string, args []string, stdin string) ([]string, *CmdResult) {
	var files []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") || a == "-" {
			files = append(files, a)
		}
	}
	input, errRes := readInputs(cmd, files, stdin)
	if errRes != nil {
		errRes.ExitCode = 1
		return nil, errRes
	}
	return splitLines(input), nil
}

func cmdRev(args []string, stdin string) CmdResult {
	lines, errRes := linesFromArgs("rev", args, stdin)
	if errRes != nil {
		return *errRes
	}
	var out strings.Builder
	for _, l := range lines {
		rs := []rune(l)
		for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
			rs[i], rs[j] = rs[j], rs[i]
		}
		out.WriteString(string(rs) + "\n")
	}
	return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: 0}
}

func cmdTac(args []string, stdin string) CmdResult {
	lines, errRes := linesFromArgs("tac", args, stdin)
	if errRes != nil {
		return *errRes
	}
	var out strings.Builder
	for i := len(lines) - 1; i >= 0; i-- {
		out.WriteString(lines[i] + "\n")
	}
	return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: 0}
}

func cmdNl(args []string, stdin string) CmdResult {
	style, format, width, sep, start := "t", "rn", 6, "\t", 1
	var files []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		val := func() string {
			if len(a) > 2 {
				return a[2:]
			}
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch {
		case strings.HasPrefix(a, "-b"):
			style = val()
		case strings.HasPrefix(a, "-n"):
			format = val()
		case strings.HasPrefix(a, "-w"):
			width, _ = strconv.Atoi(val())
		case strings.HasPrefix(a, "-s"):
			sep = val()
		case strings.HasPrefix(a, "-v"):
			start, _ = strconv.Atoi(val())
		default:
			files = append(files, a)
		}
	}
	input, errRes := readInputs("nl", files, stdin)
	if errRes != nil {
		errRes.ExitCode = 1
		return *errRes
	}
	var out strings.Builder
	n := start
	for _, l := range splitLines(input) {
		if style == "t" && strings.TrimSpace(l) == "" || style == "n" {
			out.WriteString(strings.Repeat(" ", width+len(sep)) + l + "\n")
			continue
		}
		var num string
		switch format {
		case "ln":
			num = fmt.Sprintf("%-*d", width, n)
		case "rz":
			num = fmt.Sprintf("%0*d", width, n)
		default:
			num = fmt.Sprintf("%*d", width, n)
		}
		out.WriteString(num + sep + l + "\n")
		n++
	}
	return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: 0}
}

func cmdColumn(args []string, stdin string) CmdResult {
	table := false
	sepChars, outSep := "", "  "
	var files []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-t" || a == "--table":
			table = true
		case a == "-s" && i+1 < len(args):
			i++
			sepChars = unescapeC(args[i], false)
		case strings.HasPrefix(a, "-s") && len(a) > 2:
			sepChars = unescapeC(a[2:], false)
		case a == "-o" && i+1 < len(args):
			i++
			outSep = args[i]
		case strings.HasPrefix(a, "-"):
		default:
			files = append(files, a)
		}
	}
	input, errRes := readInputs("column", files, stdin)
	if errRes != nil {
		errRes.ExitCode = 1
		return *errRes
	}
	if !table {
		return CmdResult{Stdout: input, Stderr: "", ExitCode: 0}
	}
	var rows [][]string
	var widths []int
	for _, l := range splitLines(input) {
		var cells []string
		if sepChars == "" {
			cells = strings.Fields(l)
		} else {
			cells = strings.FieldsFunc(l, func(r rune) bool { return strings.ContainsRune(sepChars, r) })
		}
		for k, c := range cells {
			if k >= len(widths) {
				widths = append(widths, 0)
			}
			widths[k] = max(widths[k], len([]rune(c)))
		}
		rows = append(rows, cells)
	}
	var out strings.Builder
	for _, row := range rows {
		for k, c := range row {
			if k == len(row)-1 {
				out.WriteString(c)
				break
			}
			out.WriteString(c + strings.Repeat(" ", widths[k]-len([]rune(c))) + outSep)
		}
		out.WriteString("\n")
	}
	return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: 0}
}

func cmdFold(args []string, stdin string) CmdResult {
	width := 80
	spaces := false
	var files []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-w" && i+1 < len(args):
			i++
			width, _ = strconv.Atoi(args[i])
		case strings.HasPrefix(a, "-w"):
			width, _ = strconv.Atoi(a[2:])
		case a == "-s":
			spaces = true
		case a == "-b":
		default:
			files = append(files, a)
		}
	}
	if width < 1 {
		return CmdResult{Stdout: "", Stderr: "fold: invalid number of columns\n", ExitCode: 1}
	}
	input, errRes := readInputs("fold", files, stdin)
	if errRes != nil {
		errRes.ExitCode = 1
		return *errRes
	}
	var out strings.Builder
	for _, l := range splitLines(input) {
		rs := []rune(l)
		for len(rs) > width {
			cut := width
			if spaces {
				if k := strings.LastIndex(string(rs[:width]), " "); k > 0 {
					cut = len([]rune(string(rs[:width])[:k+1]))
				}
			}
			out.WriteString(string(rs[:cut]) + "\n")
			rs = rs[cut:]
		}
		out.WriteString(string(rs) + "\n")
	}
	return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: 0}
}
