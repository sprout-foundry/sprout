package wasmshell

// commands_find.go — the WASM shell's find / sort / tree commands:
// cmdFind (with its predicate parsing, matchFindGroups / matchPred /
// matchPathGlob / findDepth helpers), cmdSort, and cmdTree. Split out of
// commands_search.go.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

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

// cmdFind implements the find(1) subset the agent audit showed: -name,
// -path, -type, -maxdepth, -not, and -o (top-level alternation between
// predicate groups). Implicit AND joins predicates within a group.
func cmdFind(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		args = []string{"."}
	}

	startDir := ResolvePath(args[0])
	maxDepth := -1

	// Tokenize the predicate tail into groups separated by -o.
	var groups [][]string
	var current []string
	expectValue := "" // "", "-name", "-path", "-type"
	negateNext := false

	for i := 1; i < len(args); i++ {
		a := args[i]

		if expectValue != "" {
			current = append(current, expectValue+":"+a)
			if negateNext {
				current[len(current)-1] = "!" + current[len(current)-1]
				negateNext = false
			}
			expectValue = ""
			continue
		}

		switch a {
		case "-name", "-path", "-type":
			expectValue = a
		case "-o", "-or":
			groups = append(groups, current)
			current = nil
		case "-not", "!":
			negateNext = true
		case "-maxdepth":
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil {
					maxDepth = n
					i++
				}
			}
		default:
			// Unmodeled predicates (-exec, -newer, -size, -prune, …) make
			// the query unanswerable in-browser; 127 lets the escalation
			// surface take it to a container rather than answer wrongly.
			return CmdResult{"", fmt.Sprintf("find: unsupported predicate: %s (read-only predicates only in browser shell)\n", a), 127}
		}
	}
	if expectValue != "" {
		return CmdResult{"", fmt.Sprintf("find: missing argument to %s\n", expectValue), 1}
	}
	groups = append(groups, current)

	parsed := make([][]pred, 0, len(groups))
	for _, g := range groups {
		preds, err := parseFindGroup(g)
		if err != nil {
			return CmdResult{"", fmt.Sprintf("find: %s\n", err.Error()), 1}
		}
		parsed = append(parsed, preds)
	}

	var out strings.Builder
	walkErr := WalkCompat(startDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		depth := findDepth(startDir, path)
		if maxDepth >= 0 && depth > maxDepth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !matchFindGroups(parsed, info) {
			return nil
		}

		out.WriteString(path)
		out.WriteString("\n")
		return nil
	})

	if walkErr != nil {
		return CmdResult{"", fmt.Sprintf("find: %s\n", walkErr.Error()), 1}
	}

	return CmdResult{out.String(), "", 0}
}

// pred is a single parsed find predicate.
type pred struct {
	kind    string // "name", "path", "type"
	pattern string
	negate  bool
}

func parseFindGroup(tokens []string) ([]pred, error) {
	var preds []pred
	for _, tok := range tokens {
		negate := strings.HasPrefix(tok, "!")
		tok = strings.TrimPrefix(tok, "!")
		switch {
		case strings.HasPrefix(tok, "-name:"):
			preds = append(preds, pred{kind: "name", pattern: strings.TrimPrefix(tok, "-name:"), negate: negate})
		case strings.HasPrefix(tok, "-path:"):
			preds = append(preds, pred{kind: "findpath", pattern: strings.TrimPrefix(tok, "-path:"), negate: negate})
		case strings.HasPrefix(tok, "-type:"):
			preds = append(preds, pred{kind: "type", pattern: strings.TrimPrefix(tok, "-type:"), negate: negate})
		default:
			return nil, fmt.Errorf("unknown predicate: %s", tok)
		}
	}
	return preds, nil
}

// matchFindGroups returns whether the entry matches ANY group (find -o
// semantics) — each group is an AND of its predicates. No groups means
// no predicates: everything matches.
func matchFindGroups(groups [][]pred, info os.FileInfo) bool {
	if len(groups) == 0 {
		return true
	}
	for _, g := range groups {
		all := true
		for _, p := range g {
			if !matchPred(p, info) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func matchPred(p pred, info os.FileInfo) bool {
	var matched bool
	switch p.kind {
	case "name":
		m, err := filepath.Match(p.pattern, info.Name())
		matched = err == nil && m
	case "findpath":
		matched = matchPathGlob(p.pattern, info.Name())
	case "type":
		switch p.pattern {
		case "f":
			matched = !info.IsDir()
		case "d":
			matched = info.IsDir()
		default:
			matched = false
		}
	default:
		matched = false
	}
	if p.negate {
		return !matched
	}
	return matched
}

// matchPathGlob matches a -path glob against the entry name — find(1)'s
// -path matches the whole path string; the wasmshell walk feeds relative
// names so basename matching keeps parity for the audit's usage.
func matchPathGlob(pattern, path string) bool {
	m, err := filepath.Match(pattern, path)
	return err == nil && m
}

func findDepth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(rel, string(os.PathSeparator)) + 1
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

	err := WalkCompat(root, func(p string, info os.FileInfo, err error) error {
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
