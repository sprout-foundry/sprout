package wasmshell

import (
	"fmt"
	"regexp"
	"strings"
)

func init() {
	CmdRegistry["sed"] = cmdSed
}

type sedAddr struct {
	kind  byte // 'n' line, '$' last, '/' regex, '~' step
	line  int
	step  int
	re    *regexp.Regexp
	plusN int // addr,+N
}

type sedCmd struct {
	a1, a2   *sedAddr
	negate   bool
	name     byte
	text     string
	re       *regexp.Regexp
	repl     string
	global   bool
	nth      int
	print    bool
	label    string
	block    []*sedCmd
	from, to []rune
	exitCode int
	inRange  bool
	rangeEnd int
}

type sedProg struct {
	cmds   []*sedCmd
	ere    bool
	lastRe *regexp.Regexp
}

func cmdSed(args []string, stdin string) CmdResult {
	quiet, inPlace, ere, separate := false, false, false, false
	var scripts, files []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-n" || a == "--quiet" || a == "--silent":
			quiet = true
		case a == "-E" || a == "-r" || a == "--regexp-extended":
			ere = true
		case a == "-s" || a == "--separate":
			separate = true
		case a == "-i" || strings.HasPrefix(a, "-i.") || strings.HasPrefix(a, "--in-place"):
			inPlace = true
		case a == "-e" || a == "--expression":
			if i+1 >= len(args) {
				return CmdResult{Stdout: "", Stderr: "sed: option requires an argument -- 'e'\n", ExitCode: 1}
			}
			i++
			scripts = append(scripts, args[i])
		case a == "-f" || a == "--file":
			if i+1 >= len(args) {
				return CmdResult{Stdout: "", Stderr: "sed: option requires an argument -- 'f'\n", ExitCode: 1}
			}
			i++
			data, err := readFileArg(args[i])
			if err != nil {
				return CmdResult{Stdout: "", Stderr: fmt.Sprintf("sed: couldn't open file %s: %s\n", args[i], describeErr(err)), ExitCode: 1}
			}
			scripts = append(scripts, data)
		case a == "--posix" || a == "-u" || a == "--unbuffered" || a == "--debug":
		case len(a) > 1 && a[0] == '-' && strings.Trim(a[1:], "nEris") == "":
			quiet = quiet || strings.Contains(a, "n")
			ere = ere || strings.ContainsAny(a, "Er")
			inPlace = inPlace || strings.Contains(a, "i")
			separate = separate || strings.Contains(a, "s")
		case len(a) > 1 && a[0] == '-' && a != "-":
			return CmdResult{Stdout: "", Stderr: fmt.Sprintf("sed: unsupported option %s\n", a), ExitCode: ExitCommandNotFound}
		default:
			if len(scripts) == 0 && !hasScriptFromFlags(args[:i]) {
				scripts = append(scripts, a)
			} else {
				files = append(files, a)
			}
		}
	}
	if len(scripts) == 0 {
		return CmdResult{Stdout: "", Stderr: "sed: no script specified\n", ExitCode: 1}
	}
	prog := &sedProg{ere: ere}
	cmds, err := prog.parse(strings.Join(scripts, "\n"))
	if err != nil {
		code := 1
		if strings.Contains(err.Error(), "not supported") {
			code = ExitCommandNotFound
		}
		return CmdResult{Stdout: "", Stderr: "sed: -e expression: " + err.Error() + "\n", ExitCode: code}
	}
	prog.cmds = cmds

	if inPlace {
		if len(files) == 0 {
			return CmdResult{Stdout: "", Stderr: "sed: no input files\n", ExitCode: 1}
		}
		var errs strings.Builder
		for _, f := range files {
			data, err := readFileArg(f)
			if err != nil {
				fmt.Fprintf(&errs, "sed: can't read %s: %s\n", f, describeErr(err))
				continue
			}
			out, _ := prog.run(data, quiet)
			if err := SyncWriteFile(ResolvePath(f), out); err != nil {
				fmt.Fprintf(&errs, "sed: couldn't write %s: %s\n", f, err)
			}
		}
		code := 0
		if errs.Len() > 0 {
			code = 2
		}
		return CmdResult{Stdout: "", Stderr: errs.String(), ExitCode: code}
	}

	if separate && len(files) > 0 {
		var out strings.Builder
		code := 0
		for _, f := range files {
			data, err := readFileArg(f)
			if err != nil {
				return CmdResult{Stdout: out.String(), Stderr: fmt.Sprintf("sed: can't read %s: %s\n", f, describeErr(err)), ExitCode: 2}
			}
			o, c := prog.run(data, quiet)
			out.WriteString(o)
			code = c
		}
		return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: code}
	}
	input := stdin
	if len(files) > 0 {
		var b strings.Builder
		for _, f := range files {
			if f == "-" {
				b.WriteString(stdin)
				continue
			}
			data, err := readFileArg(f)
			if err != nil {
				return CmdResult{Stdout: "", Stderr: fmt.Sprintf("sed: can't read %s: %s\n", f, describeErr(err)), ExitCode: 2}
			}
			b.WriteString(data)
			if data != "" && !strings.HasSuffix(data, "\n") {
				b.WriteByte('\n')
			}
		}
		input = b.String()
	}
	out, code := prog.run(input, quiet)
	return CmdResult{Stdout: out, Stderr: "", ExitCode: code}
}

func hasScriptFromFlags(prev []string) bool {
	for i, a := range prev {
		if (a == "-e" || a == "-f" || a == "--expression" || a == "--file") && i+1 < len(prev) {
			return true
		}
	}
	return false
}
