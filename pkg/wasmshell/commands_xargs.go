package wasmshell

import (
	"fmt"
	"strconv"
	"strings"
)

// biXargs builds command lines from standard input and runs them.
func biXargs(sh *interp, args []string, in *ioIn) CmdResult {
	maxArgs, maxLines := 0, 0
	replace := ""
	delim := ""
	nulDelim, noRunEmpty, trace := false, false, false
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			break
		}
		if a == "--" {
			i++
			break
		}
		value := func(flag string) (string, bool) {
			if len(a) > len(flag) {
				return strings.TrimPrefix(strings.TrimPrefix(a[len(flag):], "="), ""), true
			}
			if i+1 < len(args) {
				i++
				return args[i], true
			}
			return "", false
		}
		switch {
		case a == "-0" || a == "--null":
			nulDelim = true
		case a == "-r" || a == "--no-run-if-empty":
			noRunEmpty = true
		case a == "-t" || a == "--verbose":
			trace = true
		case strings.HasPrefix(a, "-n"):
			v, ok := value("-n")
			n, err := strconv.Atoi(v)
			if !ok || err != nil || n < 1 {
				return usage("xargs", "invalid number for -n", 1)
			}
			maxArgs = n
		case strings.HasPrefix(a, "-L"):
			v, ok := value("-L")
			n, err := strconv.Atoi(v)
			if !ok || err != nil || n < 1 {
				return usage("xargs", "invalid number for -L", 1)
			}
			maxLines = n
		case a == "-I" || a == "-i" || strings.HasPrefix(a, "-I") || strings.HasPrefix(a, "--replace"):
			if a == "-i" || a == "--replace" {
				replace = "{}"
			} else if v, ok := value("-I"); ok {
				replace = v
			}
		case strings.HasPrefix(a, "-d"):
			v, ok := value("-d")
			if !ok {
				return usage("xargs", "option requires an argument -- 'd'", 1)
			}
			delim = unescapeC(v, false)
		case strings.HasPrefix(a, "-P"), strings.HasPrefix(a, "-s"), strings.HasPrefix(a, "-E"):
			if len(a) == 2 {
				i++
			}
		case a == "-x" || a == "--exit" || a == "-p" || a == "--interactive":
		default:
			return usage("xargs", "invalid option -- '"+strings.TrimLeft(a, "-")+"'", 1)
		}
	}
	cmd := args[i:]
	if len(cmd) == 0 {
		cmd = []string{"echo"}
	}

	input := in.rest()
	var items []string
	var lines [][]string
	switch {
	case nulDelim:
		items = splitNonEmpty(input, "\x00")
	case delim != "":
		items = strings.Split(strings.TrimSuffix(input, "\n"), delim)
	case replace != "":
		for _, l := range strings.Split(input, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				items = append(items, l)
			}
		}
	default:
		for _, l := range strings.Split(input, "\n") {
			if toks := Tokenize(l, false); len(toks) > 0 {
				items = append(items, toks...)
				lines = append(lines, toks)
			}
		}
	}

	var batches [][]string
	switch {
	case replace != "":
		for _, it := range items {
			argv := make([]string, len(cmd))
			for k, c := range cmd {
				argv[k] = strings.ReplaceAll(c, replace, it)
			}
			batches = append(batches, argv)
		}
	case maxLines > 0 && lines != nil:
		for k := 0; k < len(lines); k += maxLines {
			var group []string
			for _, l := range lines[k:min(k+maxLines, len(lines))] {
				group = append(group, l...)
			}
			batches = append(batches, append(append([]string{}, cmd...), group...))
		}
	case maxArgs > 0:
		for k := 0; k < len(items); k += maxArgs {
			batches = append(batches, append(append([]string{}, cmd...), items[k:min(k+maxArgs, len(items))]...))
		}
	default:
		if len(items) > 0 || !noRunEmpty {
			batches = append(batches, append(append([]string{}, cmd...), items...))
		}
	}

	var out CmdResult
	for _, argv := range batches {
		if trace {
			out.writeErr(strings.Join(argv, " ") + "\n")
		}
		r := sh.runArgv(argv, "")
		out.appendOutput(r)
		switch {
		case r.ExitCode == ExitCommandNotFound:
			out.ExitCode = ExitCommandNotFound
			return out
		case r.ExitCode == 255:
			out.writeErr(fmt.Sprintf("xargs: %s: exited with status 255; aborting\n", argv[0]))
			out.ExitCode = 124
			return out
		case r.ExitCode != 0:
			out.ExitCode = 123
		}
	}
	return out
}

func splitNonEmpty(s, sep string) []string {
	var out []string
	for _, p := range strings.Split(s, sep) {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
