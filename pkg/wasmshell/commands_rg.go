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

func init() {
	CmdRegistry["rg"] = cmdRg
}

var rgTypes = map[string][]string{
	"c":        {"*.c", "*.h"},
	"cpp":      {"*.cpp", "*.cc", "*.cxx", "*.hpp", "*.hh", "*.h"},
	"cs":       {"*.cs"},
	"css":      {"*.css", "*.scss", "*.sass", "*.less"},
	"go":       {"*.go"},
	"html":     {"*.html", "*.htm"},
	"java":     {"*.java"},
	"js":       {"*.js", "*.jsx", "*.mjs", "*.cjs"},
	"json":     {"*.json"},
	"kotlin":   {"*.kt", "*.kts"},
	"md":       {"*.md", "*.markdown"},
	"markdown": {"*.md", "*.markdown"},
	"php":      {"*.php"},
	"py":       {"*.py", "*.pyi"},
	"python":   {"*.py", "*.pyi"},
	"rb":       {"*.rb"},
	"ruby":     {"*.rb"},
	"rust":     {"*.rs"},
	"sh":       {"*.sh", "*.bash", "*.zsh"},
	"sql":      {"*.sql"},
	"svelte":   {"*.svelte"},
	"swift":    {"*.swift"},
	"toml":     {"*.toml"},
	"ts":       {"*.ts", "*.tsx", "*.mts", "*.cts"},
	"txt":      {"*.txt"},
	"vue":      {"*.vue"},
	"xml":      {"*.xml"},
	"yaml":     {"*.yaml", "*.yml"},
}

type rgOpts struct {
	s         *searcher
	patterns  []string
	fixed     bool
	word      bool
	line      bool
	caseMode  string // "sensitive", "insensitive", "smart"
	globs     []string
	types     []string
	notTypes  []string
	hidden    bool
	noIgnore  bool
	filesOnly bool
	noName    bool
	forceName bool
	targets   []string
}

// cmdRg implements ripgrep's search: recursive by default, skipping hidden
// files and paths ignored by .gitignore. Output follows rg's non-terminal
// form: "path:line" prefixes appear only with -n.
func cmdRg(args []string, stdin string) CmdResult {
	o := &rgOpts{s: &searcher{binary: "skip"}, caseMode: "sensitive"}
	if res, done := o.parse(args); done {
		return res
	}
	var files []string
	var errs strings.Builder
	hadErr := false
	targets := o.targets
	implicit := len(targets) == 0
	if implicit && !o.filesOnly && stdin != "" {
		re, err := o.matcher()
		if err != nil {
			return CmdResult{Stdout: "", Stderr: "rg: " + err.Error() + "\n", ExitCode: 2}
		}
		o.s.re = re
		o.s.withName = o.forceName
		o.s.search("<stdin>", stdin)
		return grepResult(o.s, "", false)
	}
	if implicit {
		targets = []string{"."}
	}
	singleFile := false
	for _, t := range targets {
		abs := ResolvePath(t)
		info, err := os.Stat(abs)
		if err != nil {
			fmt.Fprintf(&errs, "rg: %s: %s\n", t, describeErr(err))
			hadErr = true
			continue
		}
		if !info.IsDir() {
			files = append(files, t)
			singleFile = len(targets) == 1
			continue
		}
		files = append(files, o.walk(t, abs, implicit)...)
	}
	if o.filesOnly {
		var out strings.Builder
		for _, f := range files {
			out.WriteString(f + "\n")
		}
		code := 0
		if len(files) == 0 {
			code = 1
		}
		return CmdResult{Stdout: out.String(), Stderr: errs.String(), ExitCode: code}
	}
	re, err := o.matcher()
	if err != nil {
		return CmdResult{Stdout: "", Stderr: "rg: " + err.Error() + "\n", ExitCode: 2}
	}
	o.s.re = re
	o.s.withName = (!singleFile && !o.noName) || o.forceName
	for _, f := range files {
		data, err := os.ReadFile(ResolvePath(f))
		if err != nil {
			continue
		}
		o.s.search(f, string(data))
		if o.s.quiet && o.s.matched {
			break
		}
	}
	return grepResult(o.s, errs.String(), hadErr)
}

func (o *rgOpts) matcher() (*regexp.Regexp, error) {
	fold := o.caseMode == "insensitive"
	if o.caseMode == "smart" {
		fold = true
		for _, p := range o.patterns {
			if strings.ToLower(p) != p {
				fold = false
			}
		}
	}
	return buildMatcher(o.patterns, o.fixed, true, o.word, o.line, fold)
}

// walk lists the searchable files under a directory in path order.
func (o *rgOpts) walk(root, abs string, implicit bool) []string {
	var files []string
	ignores := map[string][]ignoreRule{}
	_ = WalkCompat(abs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(abs, path)
		name := info.Name()
		if path != abs {
			if name == ".git" || (!o.hidden && strings.HasPrefix(name, ".")) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !o.noIgnore && ignoredBy(ignores, abs, path, info.IsDir()) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if info.IsDir() {
			if !o.noIgnore {
				for _, f := range []string{".gitignore", ".ignore", ".rgignore"} {
					if data, err := os.ReadFile(filepath.Join(path, f)); err == nil {
						ignores[path] = append(ignores[path], parseIgnore(string(data))...)
					}
				}
			}
			return nil
		}
		if !o.wanted(rel, name) {
			return nil
		}
		display := rel
		if !implicit {
			display = joinFindPath(root, rel)
		}
		files = append(files, display)
		return nil
	})
	sort.Strings(files)
	return files
}

func (o *rgOpts) wanted(rel, name string) bool {
	if len(o.types) > 0 {
		ok := false
		for _, t := range o.types {
			if matchAnyGlob(rgTypes[t], name) {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	for _, t := range o.notTypes {
		if matchAnyGlob(rgTypes[t], name) {
			return false
		}
	}
	included, hasInclude := false, false
	for _, g := range o.globs {
		neg := strings.HasPrefix(g, "!")
		g = strings.TrimPrefix(g, "!")
		target := name
		if strings.Contains(g, "/") {
			target = filepath.ToSlash(rel)
		}
		m := globMatch(strings.TrimPrefix(g, "**/"), target) || globMatch(g, filepath.ToSlash(rel))
		if neg {
			if m {
				return false
			}
			continue
		}
		hasInclude = true
		if m {
			included = true
		}
	}
	return !hasInclude || included
}

type ignoreRule struct {
	pattern  string
	negate   bool
	dirOnly  bool
	anchored bool
}

func parseIgnore(data string) []ignoreRule {
	var rules []ignoreRule
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimRight(line, " \r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r := ignoreRule{}
		if strings.HasPrefix(line, "!") {
			r.negate, line = true, line[1:]
		}
		if strings.HasSuffix(line, "/") {
			r.dirOnly, line = true, strings.TrimSuffix(line, "/")
		}
		if strings.Contains(strings.TrimPrefix(line, "**/"), "/") {
			r.anchored = true
		}
		r.pattern = strings.TrimPrefix(strings.TrimPrefix(line, "/"), "**/")
		rules = append(rules, r)
	}
	return rules
}

// ignoredBy applies the ignore files of every directory from the search
// root down to path; later (deeper) rules win, as in git.
func ignoredBy(ignores map[string][]ignoreRule, root, path string, isDir bool) bool {
	ignored := false
	dir := filepath.Dir(path)
	var chain []string
	for {
		chain = append(chain, dir)
		if dir == root || len(dir) <= len(root) {
			break
		}
		dir = filepath.Dir(dir)
	}
	for k := len(chain) - 1; k >= 0; k-- {
		base := chain[k]
		rel, err := filepath.Rel(base, path)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		for _, r := range ignores[base] {
			if r.dirOnly && !isDir {
				continue
			}
			var m bool
			if r.anchored {
				m = globMatch(r.pattern, rel) || strings.HasPrefix(rel, r.pattern+"/")
			} else {
				m = globMatch(r.pattern, filepath.Base(path))
			}
			if m {
				ignored = !r.negate
			}
		}
	}
	return ignored
}

func (o *rgOpts) parse(args []string) (CmdResult, bool) {
	s := o.s
	fail := func(msg string) (CmdResult, bool) {
		return CmdResult{Stdout: "", Stderr: "rg: " + msg + "\n", ExitCode: 2}, true
	}
	patternSet := false
	args = expandClusters(args, "iSsnNlcwxFvoqHIa", "egtTmABC")
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() (string, bool) {
			if i+1 < len(args) {
				i++
				return args[i], true
			}
			return "", false
		}
		num := func() (int, bool) {
			v, ok := next()
			if !ok {
				return 0, false
			}
			n, err := strconv.Atoi(v)
			return n, err == nil
		}
		if name, val, ok := strings.Cut(a, "="); ok && strings.HasPrefix(a, "--") {
			a = name
			args = append(args[:i+1], append([]string{val}, args[i+1:]...)...)
		}
		switch a {
		case "-i", "--ignore-case":
			o.caseMode = "insensitive"
		case "-S", "--smart-case":
			o.caseMode = "smart"
		case "-s", "--case-sensitive":
			o.caseMode = "sensitive"
		case "-n", "--line-number":
			s.lineNum = true
		case "-N", "--no-line-number":
			s.lineNum = false
		case "-l", "--files-with-matches":
			s.filesWith = true
		case "--files-without-match":
			s.filesWithout = true
		case "-c", "--count":
			s.count = true
		case "-w", "--word-regexp":
			o.word = true
		case "-x", "--line-regexp":
			o.line = true
		case "-F", "--fixed-strings":
			o.fixed = true
		case "-v", "--invert-match":
			s.invert = true
		case "-o", "--only-matching":
			s.only = true
		case "-q", "--quiet":
			s.quiet = true
		case "--hidden", "-.":
			o.hidden = true
		case "--no-ignore", "--no-ignore-vcs":
			o.noIgnore = true
		case "-u":
			o.noIgnore = true
		case "-uu", "-uuu":
			o.noIgnore, o.hidden = true, true
		case "--files":
			o.filesOnly = true
		case "-H", "--with-filename":
			o.forceName = true
		case "-I", "--no-filename":
			o.noName = true
		case "-a", "--text":
			s.binary = "text"
		case "-P", "--pcre2", "--no-heading", "--heading", "--color", "--colors", "-p", "--pretty", "--sort", "--no-messages", "--trim":
			if a == "--color" || a == "--colors" || a == "--sort" {
				next()
			}
		case "-e", "--regexp":
			v, ok := next()
			if !ok {
				return fail("missing pattern after -e")
			}
			o.patterns, patternSet = append(o.patterns, v), true
		case "-g", "--glob", "--iglob":
			v, ok := next()
			if !ok {
				return fail("missing glob")
			}
			o.globs = append(o.globs, v)
		case "-t", "--type", "-T", "--type-not":
			v, ok := next()
			if !ok || rgTypes[v] == nil {
				return fail(fmt.Sprintf("unrecognized file type: %s", v))
			}
			if a == "-t" || a == "--type" {
				o.types = append(o.types, v)
			} else {
				o.notTypes = append(o.notTypes, v)
			}
		case "-m", "--max-count", "-A", "--after-context", "-B", "--before-context", "-C", "--context":
			n, ok := num()
			if !ok {
				return fail("invalid number for " + a)
			}
			switch a {
			case "-m", "--max-count":
				s.maxCount = n
			case "-A", "--after-context":
				s.after = n
			case "-B", "--before-context":
				s.before = n
			default:
				s.before, s.after = n, n
			}
		case "--json", "-U", "--multiline", "--replace", "-r", "--pre", "-z", "--search-zip":
			return CmdResult{Stdout: "", Stderr: "rg: " + a + " is not supported by the in-browser shell\n", ExitCode: ExitCommandNotFound}, true
		default:
			if strings.HasPrefix(a, "-") && len(a) > 1 {
				return fail("unrecognized flag " + a)
			}
			if !patternSet && !o.filesOnly {
				o.patterns, patternSet = append(o.patterns, a), true
			} else {
				o.targets = append(o.targets, a)
			}
		}
	}
	if !patternSet && !o.filesOnly {
		return fail("no pattern given")
	}
	return CmdResult{}, false
}

// expandClusters splits combined short flags (-in, -tgo) into separate
// arguments; a value flag may end a cluster and take the remainder.
func expandClusters(args []string, noArg, withArg string) []string {
	out := make([]string, 0, len(args))
	for i, a := range args {
		if a == "--" {
			return append(out, args[i:]...)
		}
		if len(a) < 3 || a[0] != '-' || a[1] == '-' {
			out = append(out, a)
			continue
		}
		var split []string
		ok := true
		for k := 1; k < len(a); k++ {
			c := a[k]
			if strings.IndexByte(noArg, c) >= 0 {
				split = append(split, "-"+string(c))
				continue
			}
			if strings.IndexByte(withArg, c) >= 0 {
				split = append(split, "-"+string(c))
				if k+1 < len(a) {
					split = append(split, a[k+1:])
				}
				break
			}
			ok = false
			break
		}
		if ok {
			out = append(out, split...)
		} else {
			out = append(out, a)
		}
	}
	return out
}
