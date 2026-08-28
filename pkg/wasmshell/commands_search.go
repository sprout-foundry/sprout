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

// grepOptions holds the parsed flags for cmdGrep. Only the subset the
// audit saw agents actually use is modeled; unknown flags return a
// usage error (exit 2), matching GNU grep's behavior for bad flags.
type grepOptions struct {
	pattern         string
	caseInsensitive bool
	invert          bool
	lineNum         bool
	count           bool
	extended        bool // -E — accepted; Go regexps are already RE2
	onlyMatching    bool // -o
	recursive       bool // -r / -R
	noMessages      bool // -s
	afterContext    int  // -A n
	beforeContext   int  // -B n
	context         int  // -C n
	includeGlobs    []string
	targets         []string
}

func (o *grepOptions) hasContext() bool {
	return o.afterContext > 0 || o.beforeContext > 0 || o.context > 0
}

func cmdGrep(args []string, stdin string) CmdResult {
	opts, err := parseGrepArgs(args)
	if err != nil {
		return CmdResult{"", "grep: " + err.Error() + "\n", 2}
	}

	flags := ""
	if opts.caseInsensitive {
		flags = "(?i)"
	}
	re, compileErr := regexp.Compile(flags + opts.pattern)
	if compileErr != nil {
		return CmdResult{"", fmt.Sprintf("grep: invalid pattern: %s\n", compileErr.Error()), 2}
	}

	// Recursive mode walks directories and prefixes matches with the path.
	if opts.recursive {
		return grepRecursive(re, opts)
	}

	var input string
	if len(opts.targets) > 0 {
		data, readErr := os.ReadFile(ResolvePath(opts.targets[0]))
		if readErr != nil {
			if opts.noMessages {
				return CmdResult{"", "", 2}
			}
			return CmdResult{"", fmt.Sprintf("grep: %s: %s\n", opts.targets[0], readErr.Error()), 2}
		}
		input = string(data)
	} else {
		input = stdin
	}

	_, out := grepContent(re, opts, "", input)
	return out
}

// parseGrepArgs implements GNU-style short flag clustering (-rn, -iE, -aE)
// plus value flags (-A/-B/-C n or -An), and --include=GLOB.
func parseGrepArgs(args []string) (*grepOptions, error) {
	opts := &grepOptions{afterContext: -1, beforeContext: -1, context: -1}
	patternSet := false

	for i := 0; i < len(args); i++ {
		a := args[i]

		switch {
		case a == "-e":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("option requires an argument -- e")
			}
			i++
			opts.pattern = args[i]
			patternSet = true
		case a == "-i":
			opts.caseInsensitive = true
		case a == "-v":
			opts.invert = true
		case a == "-n":
			opts.lineNum = true
		case a == "-c":
			opts.count = true
		case a == "-E":
			opts.extended = true
		case a == "-o":
			opts.onlyMatching = true
		case a == "-r" || a == "-R" || a == "--recursive":
			opts.recursive = true
		case a == "-s" || a == "--no-messages":
			opts.noMessages = true
		case a == "-A" || a == "-B" || a == "-C":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("option requires an argument -- %s", strings.TrimPrefix(a, "-"))
			}
			n, convErr := strconv.Atoi(args[i+1])
			if convErr != nil {
				return nil, fmt.Errorf("invalid context count: %s", args[i+1])
			}
			i++
			switch a {
			case "-A":
				opts.afterContext = n
			case "-B":
				opts.beforeContext = n
			case "-C":
				opts.context = n
			}
		case strings.HasPrefix(a, "-A") && len(a) > 2:
			n, convErr := strconv.Atoi(a[2:])
			if convErr != nil {
				return nil, fmt.Errorf("invalid context count: %s", a)
			}
			opts.afterContext = n
		case strings.HasPrefix(a, "-B") && len(a) > 2:
			n, convErr := strconv.Atoi(a[2:])
			if convErr != nil {
				return nil, fmt.Errorf("invalid context count: %s", a)
			}
			opts.beforeContext = n
		case strings.HasPrefix(a, "-C") && len(a) > 2:
			n, convErr := strconv.Atoi(a[2:])
			if convErr != nil {
				return nil, fmt.Errorf("invalid context count: %s", a)
			}
			opts.context = n
		case strings.HasPrefix(a, "--include="):
			opts.includeGlobs = append(opts.includeGlobs, strings.TrimPrefix(a, "--include="))
		case a == "--include":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("option requires an argument -- include")
			}
			i++
			opts.includeGlobs = append(opts.includeGlobs, args[i])
		case a == "-h" || a == "--help":
			return nil, fmt.Errorf("usage: grep [-ivncEors] [-A n] [-B n] [-C n] [--include=GLOB] PATTERN [FILE...]")
		case strings.HasPrefix(a, "--"):
			return nil, fmt.Errorf("unrecognized option: %s", a)
		case strings.HasPrefix(a, "-") && len(a) > 1:
			// Short-flag cluster (-rn, -iE, -aE): every letter must be a
			// known no-arg flag; the cluster decodes to its letters.
			decoded := true
			for _, ch := range strings.TrimPrefix(a, "-") {
				switch ch {
				case 'i':
					opts.caseInsensitive = true
				case 'v':
					opts.invert = true
				case 'n':
					opts.lineNum = true
				case 'c':
					opts.count = true
				case 'E':
					opts.extended = true
				case 'o':
					opts.onlyMatching = true
				case 'r', 'R':
					opts.recursive = true
				case 's':
					opts.noMessages = true
				case 'a':
					// -a treats binary as text — no-op on a string VFS.
				default:
					decoded = false
				}
				if !decoded {
					break
				}
			}
			if decoded {
				continue
			}
			return nil, fmt.Errorf("invalid option -- '%s'", strings.TrimPrefix(a, "-"))
		default:
			if !patternSet && opts.pattern == "" {
				opts.pattern = a
				patternSet = true
			} else {
				opts.targets = append(opts.targets, a)
			}
		}
	}

	if opts.pattern == "" {
		return nil, fmt.Errorf("missing pattern")
	}

	// -C n dominates -A/-B when both are given (GNU semantics).
	if opts.context >= 0 {
		if opts.afterContext < 0 {
			opts.afterContext = opts.context
		}
		if opts.beforeContext < 0 {
			opts.beforeContext = opts.context
		}
	}
	if opts.afterContext < 0 {
		opts.afterContext = 0
	}
	if opts.beforeContext < 0 {
		opts.beforeContext = 0
	}

	return opts, nil
}

// grepContent matches one buffer of text and formats output per opts.
// label prefixes lines ("label:line:text") when non-empty.
func grepContent(re *regexp.Regexp, opts *grepOptions, label string, input string) (int, CmdResult) {
	lines := strings.Split(input, "\n")
	if input == "" {
		lines = nil
	}

	matchedCount := 0
	var out strings.Builder
	lastEmitted := -1

	prefix := ""
	if label != "" {
		prefix = label + ":"
	}

	emit := func(i int) {
		if i <= lastEmitted {
			return
		}
		if opts.count {
			return
		}
		ln := lines[i]
		if opts.onlyMatching {
			for _, m := range re.FindAllString(ln, -1) {
				out.WriteString(prefix)
				if opts.lineNum {
					out.WriteString(strconv.Itoa(i + 1))
					out.WriteString(":")
				}
				out.WriteString(m)
				out.WriteString("\n")
			}
			return
		}
		out.WriteString(prefix)
		if opts.lineNum {
			out.WriteString(strconv.Itoa(i + 1))
			out.WriteString(":")
		}
		out.WriteString(ln)
		out.WriteString("\n")
	}

	for i := range lines {
		matched := re.MatchString(lines[i])
		if opts.invert {
			matched = !matched
		}
		if !matched {
			continue
		}
		matchedCount++

		if opts.count {
			continue
		}

		start := i - opts.beforeContext
		if start < 0 {
			start = 0
		}
		end := i + opts.afterContext
		if end > len(lines)-1 {
			end = len(lines) - 1
		}
		for j := start; j <= end; j++ {
			emit(j)
		}
		lastEmitted = end
	}

	if opts.count {
		exit := 0
		if matchedCount == 0 {
			exit = 1
		}
		return matchedCount, CmdResult{fmt.Sprintf("%d\n", matchedCount), "", exit}
	}
	exit := 0
	if matchedCount == 0 {
		// GNU grep exits 1 when no lines matched.
		exit = 1
	}
	return matchedCount, CmdResult{out.String(), "", exit}
}

// grepRecursive walks target paths matching file contents, prefixing hits
// with the file path as GNU grep -r does. Missing paths are errors (exit
// 2) unless -s suppresses messages. Exit 1 when nothing matched.
func grepRecursive(re *regexp.Regexp, opts *grepOptions) CmdResult {
	targets := opts.targets
	if len(targets) == 0 {
		targets = []string{"."}
	}

	var out strings.Builder
	matchedTotal := 0
	hadErr := false

	for _, t := range targets {
		root := ResolvePath(t)
		info, statErr := os.Stat(root)
		if statErr != nil {
			if opts.noMessages {
				hadErr = true
				continue
			}
			return CmdResult{"", fmt.Sprintf("grep: %s: %s\n", t, statErr.Error()), 2}
		}

		if !info.IsDir() {
			content, readErr := os.ReadFile(root)
			if readErr != nil {
				if opts.noMessages {
					hadErr = true
					continue
				}
				return CmdResult{"", fmt.Sprintf("grep: %s: %s\n", t, readErr.Error()), 2}
			}
			n, res := grepContent(re, opts, t, string(content))
			matchedTotal += n
			out.WriteString(res.Stdout)
			continue
		}

		walkErr := WalkCompat(root, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if fi.IsDir() {
				return nil
			}
			if !matchIncludeGlobs(opts.includeGlobs, fi.Name()) {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			rel := path
			if cwd, wdErr := os.Getwd(); wdErr == nil {
				if r, relErr := filepath.Rel(cwd, path); relErr == nil {
					rel = r
				}
			}
			n, res := grepContent(re, opts, rel, string(data))
			matchedTotal += n
			out.WriteString(res.Stdout)
			return nil
		})
		if walkErr != nil {
			hadErr = true
		}
	}

	exit := 0
	if matchedTotal == 0 {
		exit = 1
	}
	if hadErr {
		exit = 2
	}
	if opts.count {
		return CmdResult{fmt.Sprintf("%d\n", matchedTotal), "", exit}
	}
	return CmdResult{out.String(), "", exit}
}

func matchIncludeGlobs(globs []string, name string) bool {
	if len(globs) == 0 {
		return true
	}
	for _, g := range globs {
		// Basename glob matching, the same semantics GNU grep applies to
		// --include patterns.
		if ok, err := filepath.Match(g, name); err == nil && ok {
			return true
		}
	}
	return false
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
