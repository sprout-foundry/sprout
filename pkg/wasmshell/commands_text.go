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
	targets := []string{}

	for idx := 0; idx < len(args); idx++ {
		a := args[idx]
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
		} else if strings.HasPrefix(a, "-") && len(a) > 1 && a != "-n" {
			if parsed, err := strconv.ParseInt(a[1:], 10, 64); err == nil {
				n = parsed
				continue
			}
		}
		targets = append(targets, a)
	}

	var input string
	if len(targets) > 0 {
		data, err := os.ReadFile(ResolvePath(targets[0]))
		if err != nil {
			return CmdResult{"", fmt.Sprintf("head: %s: %s\n", targets[0], err.Error()), 1}
		}
		input = string(data)
	} else {
		input = stdin
	}

	lines := strings.Split(input, "\n")
	if n < int64(len(lines)) {
		lines = lines[:n]
	}
	return CmdResult{strings.Join(lines, "\n") + "\n", "", 0}
}

func cmdTail(args []string, stdin string) CmdResult {
	n := int64(10)
	targets := []string{}

	for idx := 0; idx < len(args); idx++ {
		a := args[idx]
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
		} else if strings.HasPrefix(a, "-") && len(a) > 1 && a != "-n" {
			if parsed, err := strconv.ParseInt(a[1:], 10, 64); err == nil {
				n = parsed
				continue
			}
		}
		targets = append(targets, a)
	}

	var input string
	if len(targets) > 0 {
		data, err := os.ReadFile(ResolvePath(targets[0]))
		if err != nil {
			return CmdResult{"", fmt.Sprintf("tail: %s: %s\n", targets[0], err.Error()), 1}
		}
		input = string(data)
	} else {
		input = stdin
	}

	lines := strings.Split(input, "\n")
	if int64(len(lines)) > n {
		lines = lines[len(lines)-int(n):]
	}
	return CmdResult{strings.Join(lines, "\n") + "\n", "", 0}
}

func cmdWc(args []string, stdin string) CmdResult {
	linesOnly := false
	charsOnly := false
	wordsOnly := false
	targets := []string{}

	for _, a := range args {
		switch a {
		case "-l":
			linesOnly = true
		case "-c", "-m":
			charsOnly = true
		case "-w":
			wordsOnly = true
		default:
			targets = append(targets, a)
		}
	}

	var input string
	if len(targets) > 0 {
		data, err := os.ReadFile(ResolvePath(targets[0]))
		if err != nil {
			return CmdResult{"", fmt.Sprintf("wc: %s: %s\n", targets[0], err.Error()), 1}
		}
		input = string(data)
	} else {
		input = stdin
	}

	lineCount := int64(strings.Count(input, "\n"))
	wordCount := int64(len(strings.Fields(input)))
	charCount := int64(utf8.RuneCountInString(input))

	if linesOnly {
		return CmdResult{fmt.Sprintf("%d\n", lineCount), "", 0}
	}
	if wordsOnly {
		return CmdResult{fmt.Sprintf("%d\n", wordCount), "", 0}
	}
	if charsOnly {
		return CmdResult{fmt.Sprintf("%d\n", charCount), "", 0}
	}

	return CmdResult{fmt.Sprintf("%8d %8d %8d\n", lineCount, wordCount, charCount), "", 0}
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
