package wasmshell

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func cmdGrep(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "grep: missing pattern\n", 1}
	}

	caseInsensitive := false
	invert := false
	lineNum := false
	count := false
	pattern := ""

	targets := []string{}
	for idx := 0; idx < len(args); idx++ {
		a := args[idx]
		if a == "-i" {
			caseInsensitive = true
		} else if a == "-v" {
			invert = true
		} else if a == "-n" {
			lineNum = true
		} else if a == "-c" {
			count = true
		} else if a == "-e" && idx+1 < len(args) {
			pattern = args[idx+1]
			idx++
		} else if pattern == "" && !strings.HasPrefix(a, "-") {
			pattern = a
		} else if !strings.HasPrefix(a, "-") {
			targets = append(targets, a)
		}
	}

	if pattern == "" {
		return CmdResult{"", "grep: missing pattern\n", 1}
	}

	var input string
	if len(targets) > 0 {
		data, err := os.ReadFile(ResolvePath(targets[0]))
		if err != nil {
			return CmdResult{"", fmt.Sprintf("grep: %s: %s\n", targets[0], err.Error()), 1}
		}
		input = string(data)
	} else {
		input = stdin
	}

	flags := ""
	if caseInsensitive {
		flags = "(?i)"
	}
	re, err := regexp.Compile(flags + pattern)
	if err != nil {
		return CmdResult{"", fmt.Sprintf("grep: invalid pattern: %s\n", err.Error()), 2}
	}

	lines := strings.Split(input, "\n")
	var out strings.Builder
	matchedCount := 0

	for i, line := range lines {
		matched := re.MatchString(line)
		if invert {
			matched = !matched
		}
		if matched {
			matchedCount++
			if count {
				continue
			}
			if lineNum {
				fmt.Fprintf(&out, "%d:", i+1)
			}
			out.WriteString(line)
			out.WriteString("\n")
		}
	}

	if count {
		return CmdResult{fmt.Sprintf("%d\n", matchedCount), "", 0}
	}

	return CmdResult{out.String(), "", 0}
}

func cmdSort(args []string, stdin string) CmdResult {
	numeric := false
	reverse := false
	unique := false
	paths := []string{}

	for _, a := range args {
		switch a {
		case "-n", "--numeric-sort":
			numeric = true
		case "-r", "--reverse":
			reverse = true
		case "-u", "--unique":
			unique = true
		default:
			paths = append(paths, a)
		}
	}

	var input string
	if len(paths) > 0 {
		data, err := os.ReadFile(ResolvePath(paths[0]))
		if err != nil {
			return CmdResult{"", fmt.Sprintf("sort: %s: %s\n", paths[0], err.Error()), 1}
		}
		input = string(data)
	} else {
		input = stdin
	}

	lines := strings.Split(strings.TrimSpace(input), "\n")

	if numeric {
		sort.Slice(lines, func(i, j int) bool {
			a, _ := strconv.ParseFloat(strings.TrimSpace(lines[i]), 64)
			b, _ := strconv.ParseFloat(strings.TrimSpace(lines[j]), 64)
			if reverse {
				return a >= b
			}
			return a <= b
		})
	} else {
		if reverse {
			sort.Sort(sort.Reverse(sort.StringSlice(lines)))
		} else {
			sort.Strings(lines)
		}
	}

	if unique {
		seen := map[string]bool{}
		filtered := []string{}
		for _, l := range lines {
			key := l
			if numeric {
				key = strings.TrimSpace(l)
			}
			if !seen[key] {
				seen[key] = true
				filtered = append(filtered, l)
			}
		}
		lines = filtered
	}

	return CmdResult{strings.Join(lines, "\n") + "\n", "", 0}
}

func cmdFind(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		args = []string{"."}
	}

	startDir := ResolvePath(args[0])
	namePattern := ""
	filterType := ""

	for i := 1; i < len(args); i++ {
		if args[i] == "-name" && i+1 < len(args) {
			namePattern = args[i+1]
			i++
		} else if args[i] == "-type" && i+1 < len(args) {
			filterType = args[i+1]
			i++
		}
	}

	var out strings.Builder
	err := filepath.Walk(startDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if namePattern != "" {
			matched, err := filepath.Match(namePattern, info.Name())
			if err != nil || !matched {
				return nil
			}
		}

		if filterType != "" {
			if filterType == "f" && info.IsDir() {
				return nil
			}
			if filterType == "d" && !info.IsDir() {
				return nil
			}
		}

		out.WriteString(path)
		out.WriteString("\n")
		return nil
	})

	if err != nil {
		return CmdResult{"", fmt.Sprintf("find: %s\n", err.Error()), 1}
	}

	return CmdResult{out.String(), "", 0}
}

func cmdTree(args []string, stdin string) CmdResult {
	showHidden := false
	maxDepth := -1
	path := "."
	targets := []string{}

	for i, a := range args {
		if a == "-a" {
			showHidden = true
		} else if strings.HasPrefix(a, "-L") {
			val := strings.TrimPrefix(a, "-L")
			if val == "" && i+1 < len(args) {
				val = args[i+1]
			}
			if parsed, err := strconv.Atoi(val); err == nil {
				maxDepth = parsed
			}
		} else if !strings.HasPrefix(a, "-") {
			targets = append(targets, a)
		}
	}

	if len(targets) > 0 {
		path = targets[0]
	}

	root := ResolvePath(path)
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n", root)

	counts := []int{0, 0} // [dirs, files]

	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}

		if !showHidden && strings.HasPrefix(filepath.Base(p), ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if maxDepth > 0 {
			depth := strings.Count(rel, string(os.PathSeparator))
			if depth > maxDepth {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		depth := strings.Count(rel, string(os.PathSeparator))
		prefix := ""
		for j := 0; j < depth; j++ {
			prefix += "│   "
		}

		branch := "├── "
		if info.IsDir() {
			branch = "├── "
			counts[0]++
		} else {
			counts[1]++
		}

		fmt.Fprintf(&out, "%s%s%s\n", prefix, branch, info.Name())
		return nil
	})

	if err != nil {
		return CmdResult{"", fmt.Sprintf("tree: %s\n", err.Error()), 1}
	}

	fmt.Fprintf(&out, "\n%d directories, %d files\n", counts[0], counts[1])
	return CmdResult{out.String(), "", 0}
}
