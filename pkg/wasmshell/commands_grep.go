package wasmshell

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func init() {
	CmdRegistry["grep"] = cmdGrep
	CmdRegistry["egrep"] = func(a []string, s string) CmdResult { return cmdGrep(append([]string{"-E"}, a...), s) }
	CmdRegistry["fgrep"] = func(a []string, s string) CmdResult { return cmdGrep(append([]string{"-F"}, a...), s) }
}

// searcher matches lines and formats hits the way grep and rg print them.
type searcher struct {
	re           *regexp.Regexp
	invert       bool
	count        bool
	filesWith    bool
	filesWithout bool
	only         bool
	quiet        bool
	lineNum      bool
	withName     bool
	before       int
	after        int
	maxCount     int
	binary       string // "report", "text", "skip"
	nullAfter    bool
	out          strings.Builder
	matched      bool
}

func hasBinary(s string) bool {
	n := len(s)
	if n > 8000 {
		n = 8000
	}
	return strings.IndexByte(s[:n], 0) >= 0
}

// search scans one input; label names it in the output.
func (s *searcher) search(label, content string) bool {
	if hasBinary(content) {
		switch s.binary {
		case "skip":
			return false
		case "report":
			if s.re.MatchString(content) != s.invert {
				s.matched = true
				if !s.quiet && !s.count && !s.filesWith && !s.filesWithout {
					fmt.Fprintf(&s.out, "Binary file %s matches\n", label)
				}
				s.emitSummary(label, 1)
				return true
			}
			s.emitSummary(label, 0)
			return false
		}
	}
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	hits := 0
	lastPrinted := -1
	pendingAfter := 0
	prefix := func(i int, sep string) string {
		var b strings.Builder
		if s.withName {
			b.WriteString(label)
			b.WriteString(sep)
		}
		if s.lineNum {
			b.WriteString(strconv.Itoa(i + 1))
			b.WriteString(sep)
		}
		return b.String()
	}
	printing := !s.count && !s.filesWith && !s.filesWithout && !s.quiet
	for i, line := range lines {
		isMatch := s.re.MatchString(line) != s.invert
		if !isMatch {
			if printing && pendingAfter > 0 && i > lastPrinted {
				s.out.WriteString(prefix(i, "-") + line + "\n")
				lastPrinted = i
				pendingAfter--
			}
			continue
		}
		if s.maxCount > 0 && hits >= s.maxCount {
			break
		}
		hits++
		if !printing {
			if s.quiet || s.filesWith {
				break
			}
			continue
		}
		start := i - s.before
		if start <= lastPrinted {
			start = lastPrinted + 1
		}
		if start < 0 {
			start = 0
		}
		if (s.before > 0 || s.after > 0) && lastPrinted >= 0 && start > lastPrinted+1 {
			s.out.WriteString("--\n")
		}
		for j := start; j < i; j++ {
			s.out.WriteString(prefix(j, "-") + lines[j] + "\n")
		}
		if s.only && !s.invert {
			for _, m := range s.re.FindAllString(line, -1) {
				if m != "" {
					s.out.WriteString(prefix(i, ":") + m + "\n")
				}
			}
		} else {
			s.out.WriteString(prefix(i, ":") + line + "\n")
		}
		lastPrinted = i
		pendingAfter = s.after
	}
	if hits > 0 {
		s.matched = true
	}
	s.emitSummary(label, hits)
	return hits > 0
}

func (s *searcher) emitSummary(label string, hits int) {
	end := "\n"
	if s.nullAfter {
		end = "\x00"
	}
	switch {
	case s.quiet:
	case s.filesWith:
		if hits > 0 {
			s.out.WriteString(label + end)
		}
	case s.filesWithout:
		if hits == 0 {
			s.out.WriteString(label + end)
		}
	case s.count:
		if s.withName {
			s.out.WriteString(label + ":")
		}
		s.out.WriteString(strconv.Itoa(hits) + "\n")
	}
}

// breToERE converts a POSIX basic regular expression to the extended
// syntax Go understands: in BRE, \| \+ \? \( \) \{ \} are operators and
// the bare characters are literals.
func breToERE(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '\\' && i+1 < len(p) {
			n := p[i+1]
			if strings.IndexByte("|+?(){}", n) >= 0 {
				b.WriteByte(n)
			} else {
				b.WriteByte('\\')
				b.WriteByte(n)
			}
			i++
			continue
		}
		if c == '[' {
			j := i + 1
			if j < len(p) && p[j] == '^' {
				j++
			}
			if j < len(p) && p[j] == ']' {
				j++
			}
			for j < len(p) && p[j] != ']' {
				if p[j] == '[' && j+1 < len(p) && p[j+1] == ':' {
					if k := strings.Index(p[j:], ":]"); k > 0 {
						j += k + 1
					}
				}
				j++
			}
			if j < len(p) {
				b.WriteString(p[i : j+1])
				i = j
				continue
			}
		}
		if strings.IndexByte("|+?(){}", c) >= 0 {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}

// buildMatcher combines patterns into one regexp.
func buildMatcher(patterns []string, fixed, extended, word, line, fold bool) (*regexp.Regexp, error) {
	parts := make([]string, 0, len(patterns))
	for _, p := range patterns {
		switch {
		case fixed:
			p = regexp.QuoteMeta(p)
		case !extended:
			p = breToERE(p)
		}
		parts = append(parts, "(?:"+p+")")
	}
	expr := strings.Join(parts, "|")
	if word {
		expr = `\b(?:` + expr + `)\b`
	}
	if line {
		expr = "^(?:" + expr + ")$"
	}
	if fold {
		expr = "(?i)" + expr
	}
	return regexp.Compile(expr)
}

func cmdGrep(args []string, stdin string) CmdResult {
	o := &grepOpts{s: &searcher{binary: "report"}}
	if err := o.parse(args); err != nil {
		return CmdResult{Stdout: "", Stderr: "grep: " + err.Error() + "\n", ExitCode: 2}
	}
	re, err := buildMatcher(o.patterns, o.fixed, o.extended, o.word, o.line, o.fold)
	if err != nil {
		return CmdResult{Stdout: "", Stderr: "grep: invalid pattern: " + err.Error() + "\n", ExitCode: 2}
	}
	s := o.s
	s.re = re
	var errs strings.Builder
	hadErr := false
	targets := o.targets
	if len(targets) == 0 {
		if o.recursive {
			s.withName = !o.noName
			o.walk("", ResolvePath("."))
			return grepResult(s, "", false)
		} else {
			s.withName = o.forceName
			s.search("(standard input)", stdin)
			return grepResult(s, "", false)
		}
	}
	s.withName = (len(targets) > 1 || o.recursive) && !o.noName || o.forceName
	for _, t := range targets {
		if t == "-" {
			s.search("(standard input)", stdin)
			continue
		}
		abs := ResolvePath(t)
		info, statErr := os.Stat(abs)
		if statErr != nil {
			hadErr = true
			if !o.noMessages {
				fmt.Fprintf(&errs, "grep: %s: %s\n", t, describeErr(statErr))
			}
			continue
		}
		if info.IsDir() {
			if !o.recursive {
				hadErr = true
				if !o.noMessages {
					fmt.Fprintf(&errs, "grep: %s: Is a directory\n", t)
				}
				continue
			}
			o.walk(t, abs)
			continue
		}
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			hadErr = true
			if !o.noMessages {
				fmt.Fprintf(&errs, "grep: %s: %s\n", t, describeErr(readErr))
			}
			continue
		}
		s.search(t, string(data))
		if s.quiet && s.matched {
			break
		}
	}
	return grepResult(s, errs.String(), hadErr)
}

func grepResult(s *searcher, errs string, hadErr bool) CmdResult {
	code := 1
	if s.matched {
		code = 0
	}
	if hadErr && (!s.quiet || !s.matched) {
		code = 2
	}
	return CmdResult{Stdout: s.out.String(), Stderr: errs, ExitCode: code}
}

func (o *grepOpts) walk(root, abs string) {
	_ = WalkCompat(abs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		name := info.Name()
		if info.IsDir() {
			if path != abs && matchAnyGlob(o.excludeDirs, name) {
				return filepath.SkipDir
			}
			return nil
		}
		if len(o.includes) > 0 && !matchAnyGlob(o.includes, name) {
			return nil
		}
		if matchAnyGlob(o.excludes, name) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		rel, _ := filepath.Rel(abs, path)
		label := rel
		if root != "" {
			label = joinFindPath(root, rel)
		}
		o.s.search(label, string(data))
		if o.s.quiet && o.s.matched {
			return filepath.SkipAll
		}
		return nil
	})
}

func matchAnyGlob(globs []string, name string) bool {
	for _, g := range globs {
		if globMatch(g, name) {
			return true
		}
	}
	return false
}
