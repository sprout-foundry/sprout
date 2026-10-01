package wasmshell

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

func cmdHead(args []string, stdin string) CmdResult {
	n := int64(10)
	byteMode := false
	quiet := false
	targets := []string{}

	for idx := 0; idx < len(args); idx++ {
		a := args[idx]
		if a == "-c" {
			byteMode = true
			if idx+1 < len(args) {
				if parsed, err := strconv.ParseInt(args[idx+1], 10, 64); err == nil {
					n = parsed
					idx++
				}
			}
			continue
		}
		if a == "-q" || a == "--quiet" || a == "--silent" {
			quiet = true
			continue
		}
		if a == "-v" || a == "--verbose" {
			quiet = false
			continue
		}
		if strings.HasPrefix(a, "-n") {
			val := strings.TrimPrefix(a, "-n")
			if val == "" && idx+1 < len(args) {
				val = args[idx+1]
				idx++
			}
			if parsed, err := strconv.ParseInt(val, 10, 64); err == nil {
				n = parsed
				continue
			}
		} else if strings.HasPrefix(a, "-c") && len(a) > 2 {
			if parsed, err := strconv.ParseInt(a[2:], 10, 64); err == nil {
				n = parsed
				byteMode = true
				continue
			}
		} else if strings.HasPrefix(a, "-") && len(a) > 1 && a != "-n" {
			if parsed, err := strconv.ParseInt(a[1:], 10, 64); err == nil {
				n = parsed
				continue
			}
		}
		targets = append(targets, a)
	}

	var out strings.Builder
	writeInput := func(label string, input string) {
		if label != "" && len(targets) > 1 && !quiet {
			fmt.Fprintf(&out, "==> %s <==\n", label)
		}
		if byteMode {
			if int64(len(input)) > n && n >= 0 {
				input = input[:n]
			}
			out.WriteString(input)
			return
		}
		lines := strings.Split(input, "\n")
		// A trailing newline produces one empty final element; GNU head
		// treats it as end-of-file, not a printable line.
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		if n >= 0 && n < int64(len(lines)) {
			lines = lines[:n]
		}
		if len(lines) > 0 {
			out.WriteString(strings.Join(lines, "\n") + "\n")
		}
	}

	if len(targets) > 0 {
		for _, t := range targets {
			data, err := os.ReadFile(ResolvePath(t))
			if err != nil {
				return CmdResult{Stdout: "", Stderr: fmt.Sprintf("head: %s: %s\n", t, describeErr(err)), ExitCode: 1}
			}
			writeInput(t, string(data))
		}
		return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: 0}
	}

	writeInput("", stdin)
	return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: 0}
}

func cmdTail(args []string, stdin string) CmdResult {
	n := int64(10)
	byteMode := false
	quiet := false
	fromLine := int64(0) // tail -n +K: starting at line K
	targets := []string{}

	for idx := 0; idx < len(args); idx++ {
		a := args[idx]
		if a == "-c" {
			byteMode = true
			if idx+1 < len(args) {
				if parsed, err := strconv.ParseInt(args[idx+1], 10, 64); err == nil {
					n = parsed
					idx++
				}
			}
			continue
		}
		if a == "-q" || a == "--quiet" || a == "--silent" {
			quiet = true
			continue
		}
		if a == "-v" || a == "--verbose" {
			quiet = false
			continue
		}
		if strings.HasPrefix(a, "-n") {
			val := strings.TrimPrefix(a, "-n")
			if val == "" && idx+1 < len(args) {
				val = args[idx+1]
				idx++
			}
			if strings.HasPrefix(val, "+") {
				if parsed, err := strconv.ParseInt(val[1:], 10, 64); err == nil {
					fromLine = parsed
					continue
				}
			}
			if parsed, err := strconv.ParseInt(val, 10, 64); err == nil {
				n = parsed
				continue
			}
		} else if strings.HasPrefix(a, "-c") && len(a) > 2 {
			if parsed, err := strconv.ParseInt(a[2:], 10, 64); err == nil {
				n = parsed
				byteMode = true
				continue
			}
		} else if strings.HasPrefix(a, "-") && len(a) > 1 && a != "-n" {
			if parsed, err := strconv.ParseInt(a[1:], 10, 64); err == nil {
				n = parsed
				continue
			}
		}
		targets = append(targets, a)
	}

	var out strings.Builder
	writeInput := func(label string, input string) {
		if label != "" && len(targets) > 1 && !quiet {
			fmt.Fprintf(&out, "==> %s <==\n", label)
		}
		if byteMode {
			if n >= 0 && int64(len(input)) > n {
				input = input[int64(len(input))-n:]
			}
			out.WriteString(input)
			return
		}
		lines := strings.Split(input, "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		if fromLine > 0 {
			if fromLine <= int64(len(lines)) {
				lines = lines[fromLine-1:]
			} else {
				lines = nil
			}
		} else if int64(len(lines)) > n {
			lines = lines[int64(len(lines))-n:]
		}
		if len(lines) > 0 {
			out.WriteString(strings.Join(lines, "\n") + "\n")
		}
	}

	if len(targets) > 0 {
		for _, t := range targets {
			data, err := os.ReadFile(ResolvePath(t))
			if err != nil {
				return CmdResult{Stdout: "", Stderr: fmt.Sprintf("tail: %s: %s\n", t, describeErr(err)), ExitCode: 1}
			}
			writeInput(t, string(data))
		}
		return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: 0}
	}

	writeInput("", stdin)
	return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: 0}
}

type wcCounts struct {
	name                                string
	lines, words, chars, bytes, maxLine int
}

func countText(name, s string) wcCounts {
	c := wcCounts{name: name, lines: strings.Count(s, "\n"), words: len(strings.Fields(s)), chars: utf8.RuneCountInString(s), bytes: len(s)}
	for _, l := range strings.Split(s, "\n") {
		c.maxLine = max(c.maxLine, utf8.RuneCountInString(l))
	}
	return c
}

func cmdWc(args []string, stdin string) CmdResult {
	flags, targets := splitFlags(args)
	for _, a := range args {
		switch a {
		case "--lines":
			flags += "l"
		case "--words":
			flags += "w"
		case "--bytes":
			flags += "c"
		case "--chars":
			flags += "m"
		}
	}
	if flags == "" {
		flags = "lwc"
	}
	var counts []wcCounts
	var errs strings.Builder
	if len(targets) == 0 {
		counts = append(counts, countText("", stdin))
	}
	for _, t := range targets {
		data := stdin
		if t != "-" {
			var err error
			if data, err = readFileArg(t); err != nil {
				fmt.Fprintf(&errs, "wc: %s: %s\n", t, describeErrText(err))
				continue
			}
		}
		counts = append(counts, countText(t, data))
	}
	if len(counts) > 1 {
		total := wcCounts{name: "total"}
		for _, c := range counts {
			total.lines += c.lines
			total.words += c.words
			total.chars += c.chars
			total.bytes += c.bytes
			total.maxLine = max(total.maxLine, c.maxLine)
		}
		counts = append(counts, total)
	}
	columns := 0
	for _, f := range "lwmcL" {
		if strings.ContainsRune(flags, f) {
			columns++
		}
	}
	width := 1
	if columns > 1 || len(counts) > 1 {
		width = 7
		if len(targets) > 0 {
			width = 1
			for _, c := range counts {
				width = max(width, len(fmt.Sprint(max(c.lines, c.words, c.bytes, c.chars))))
			}
		}
	}
	var out strings.Builder
	for _, c := range counts {
		var parts []string
		for _, f := range "lwmcL" {
			if !strings.ContainsRune(flags, f) {
				continue
			}
			v := map[rune]int{'l': c.lines, 'w': c.words, 'm': c.chars, 'c': c.bytes, 'L': c.maxLine}[f]
			parts = append(parts, fmt.Sprintf("%*d", width, v))
		}
		line := strings.Join(parts, " ")
		if c.name != "" {
			line += " " + c.name
		}
		out.WriteString(line + "\n")
	}
	code := 0
	if errs.Len() > 0 {
		code = 1
	}
	return CmdResult{Stdout: out.String(), Stderr: errs.String(), ExitCode: code}
}

func cmdTee(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{Stdout: stdin, Stderr: "", ExitCode: 0}
	}

	appendMode := false
	targets := []string{}

	for _, a := range args {
		if a == "-a" {
			appendMode = true
		} else {
			targets = append(targets, a)
		}
	}

	for _, t := range targets {
		path := ResolvePath(t)
		if appendMode {
			existing := ""
			if data, err := os.ReadFile(path); err == nil {
				existing = string(data)
			}
			SyncWriteFile(path, existing+stdin)
		} else {
			SyncWriteFile(path, stdin)
		}
	}

	return CmdResult{Stdout: stdin, Stderr: "", ExitCode: 0}
}
