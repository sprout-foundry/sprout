package wasmshell

// commands_search.go — the WASM shell's grep command: the grepOptions
// struct, cmdGrep, and the argument parsing (parseGrepArgs), the content /
// recursive matchers (grepContent, grepRecursive), and the include-glob
// matcher (matchIncludeGlobs). The find / sort / tree commands live in
// commands_find.go.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
