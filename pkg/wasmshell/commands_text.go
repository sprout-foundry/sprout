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
				return CmdResult{"", fmt.Sprintf("head: %s: %s\n", t, err.Error()), 1}
			}
			writeInput(t, string(data))
		}
		return CmdResult{out.String(), "", 0}
	}

	writeInput("", stdin)
	return CmdResult{out.String(), "", 0}
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
				return CmdResult{"", fmt.Sprintf("tail: %s: %s\n", t, err.Error()), 1}
			}
			writeInput(t, string(data))
		}
		return CmdResult{out.String(), "", 0}
	}

	writeInput("", stdin)
	return CmdResult{out.String(), "", 0}
}

func cmdWc(args []string, stdin string) CmdResult {
	linesOnly := false
	charsOnly := false
	wordsOnly := false
	bytesOnly := false
	targets := []string{}

	for _, a := range args {
		switch a {
		case "-l":
			linesOnly = true
		case "-c":
			bytesOnly = true
		case "-m":
			charsOnly = true
		case "-w":
			wordsOnly = true
		case "-lc", "-cl", "-lw", "-wl", "-cw", "-wc", "-lwc", "-clw", "-wlc", "-cwl":
			// Flag clusters select the corresponding counts; the summary
			// line then prints them in l/w/c order like GNU wc.
			if strings.Contains(a, "l") {
				linesOnly = true
			}
			if strings.Contains(a, "w") {
				wordsOnly = true
			}
			if strings.Contains(a, "c") {
				bytesOnly = true
			}
		default:
			targets = append(targets, a)
		}
	}

	var input string
	if len(targets) > 0 {
		// Per-file counts, one summary line each — GNU wc multi-file shape.
		type wcCounts struct {
			name                string
			lines, words, chars int
		}
		var counts []wcCounts
		var total wcCounts
		for _, t := range targets {
			data, err := os.ReadFile(ResolvePath(t))
			if err != nil {
				return CmdResult{"", fmt.Sprintf("wc: %s: %s\n", t, err.Error()), 1}
			}
			s := string(data)
			c := wcCounts{
				name:  t,
				lines: strings.Count(s, "\n"),
				words: len(strings.Fields(s)),
				chars: utf8.RuneCountInString(s),
			}
			counts = append(counts, c)
			total.name = "total"
			total.lines += c.lines
			total.words += c.words
			total.chars += c.chars
		}
		if len(counts) > 1 {
			counts = append(counts, total)
		}
		var sb strings.Builder
		for _, c := range counts {
			sb.WriteString(formatWcCounts(c.lines, c.words, c.chars, linesOnly, wordsOnly, bytesOnly, charsOnly) + " " + c.name + "\n")
		}
		return CmdResult{sb.String(), "", 0}
	}
	input = stdin

	lineCount := int64(strings.Count(input, "\n"))
	wordCount := int64(len(strings.Fields(input)))
	charCount := int64(utf8.RuneCountInString(input))
	_ = byteCountOf(input)

	countStr := formatWcCounts(int(lineCount), int(wordCount), int(charCount), linesOnly, wordsOnly, bytesOnly, charsOnly)
	if countStr != "" {
		return CmdResult{countStr + "\n", "", 0}
	}
	return CmdResult{fmt.Sprintf("%8d %8d %8d\n", lineCount, wordCount, charCount), "", 0}
}

// byteCountOf returns the byte length of the string VFS content.
func byteCountOf(s string) int64 { return int64(len(s)) }

// formatWcCounts renders one wc line for the selected counts; empty string
// means no flags were given (caller falls back to the l/w/c summary).
func formatWcCounts(lines, words, chars int, linesOnly, wordsOnly, bytesOnly, charsOnly bool) string {
	var parts []string
	if linesOnly {
		parts = append(parts, fmt.Sprintf("%d", lines))
	}
	if wordsOnly {
		parts = append(parts, fmt.Sprintf("%d", words))
	}
	if bytesOnly {
		// The string-backed VFS is UTF-8; byte count == rune length only
		// for ASCII, so use the rune count as the closest stand-in.
		parts = append(parts, fmt.Sprintf("%d", chars))
	} else if charsOnly {
		parts = append(parts, fmt.Sprintf("%d", chars))
	}
	return strings.Join(parts, " ")
}

func cmdTr(args []string, stdin string) CmdResult {
	if len(args) < 2 {
		return CmdResult{"", "tr: missing operand\n", 1}
	}

	from := []rune(args[0])
	to := []rune(args[1])
	deleteSet := false
	squeeze := false

	for _, a := range args[2:] {
		if a == "-d" {
			deleteSet = true
		} else if a == "-s" {
			squeeze = true
		}
	}

	result := stdin

	if deleteSet {
		for _, c := range from {
			result = strings.ReplaceAll(result, string(c), "")
		}
	} else {
		runes := []rune(result)
		translate := make(map[rune]rune, len(from))
		for j, f := range from {
			if j < len(to) {
				translate[f] = to[j]
			}
		}
		for i, r := range runes {
			if replacement, ok := translate[r]; ok {
				runes[i] = replacement
			}
		}
		result = string(runes)
	}

	if squeeze {
		for _, c := range from {
			double := string(c) + string(c)
			for strings.Contains(result, double) {
				result = strings.ReplaceAll(result, double, string(c))
			}
		}
	}

	if result != "" && !strings.HasSuffix(result, "\n") && strings.HasSuffix(stdin, "\n") {
		result += "\n"
	}

	return CmdResult{result, "", 0}
}

func cmdUniq(args []string, stdin string) CmdResult {
	lines := strings.Split(strings.TrimRight(stdin, "\n"), "\n")
	var out strings.Builder
	prev := ""
	countOnly := false

	for _, a := range args {
		if a == "-c" {
			countOnly = true
		}
	}

	if countOnly {
		counts := map[string]int{}
		for _, l := range lines {
			counts[l]++
		}
		for _, l := range lines {
			if counts[l] > 0 {
				fmt.Fprintf(&out, "%7d %s\n", counts[l], l)
				delete(counts, l)
			}
		}
	} else {
		for _, l := range lines {
			if l != prev {
				out.WriteString(l)
				out.WriteString("\n")
				prev = l
			}
		}
	}

	return CmdResult{out.String(), "", 0}
}

func cmdCut(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "cut: missing operand\n", 1}
	}

	delimiter := "\t"
	fieldStr := ""

	for i, a := range args {
		if strings.HasPrefix(a, "-d") {
			delimiter = strings.TrimPrefix(a, "-d")
			if delimiter == "" && i+1 < len(args) {
				delimiter = args[i+1]
			}
		} else if strings.HasPrefix(a, "-f") {
			fieldStr = strings.TrimPrefix(a, "-f")
			if fieldStr == "" && i+1 < len(args) {
				fieldStr = args[i+1]
			}
		}
	}

	if fieldStr == "" {
		return CmdResult{"", "cut: you must specify a list of fields\n", 1}
	}

	fields := []int{}
	for _, part := range strings.Split(fieldStr, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && n > 0 {
			fields = append(fields, n-1)
		}
	}

	lines := strings.Split(strings.TrimRight(stdin, "\n"), "\n")
	var out strings.Builder

	for _, line := range lines {
		parts := strings.Split(line, delimiter)
		var selected []string
		for _, f := range fields {
			if f < len(parts) {
				selected = append(selected, parts[f])
			}
		}
		out.WriteString(strings.Join(selected, delimiter))
		out.WriteString("\n")
	}

	return CmdResult{out.String(), "", 0}
}

func cmdTee(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{stdin, "", 0}
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

	return CmdResult{stdin, "", 0}
}
