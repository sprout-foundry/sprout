package wasmshell

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type grepOpts struct {
	s           *searcher
	patterns    []string
	patternSet  bool
	fixed       bool
	extended    bool
	word        bool
	line        bool
	fold        bool
	recursive   bool
	noMessages  bool
	noName      bool
	forceName   bool
	includes    []string
	excludes    []string
	excludeDirs []string
	targets     []string
}

func (o *grepOpts) parse(args []string) error {
	s := o.s
	intArg := func(v string) (int, error) {
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("invalid number: %s", v)
		}
		return n, nil
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		value := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("option requires an argument -- %s", strings.TrimLeft(a, "-"))
			}
			i++
			return args[i], nil
		}
		if a == "--" {
			for _, rest := range args[i+1:] {
				o.positional(rest)
			}
			break
		}
		if strings.HasPrefix(a, "--") {
			name, val, hasVal := strings.Cut(a[2:], "=")
			switch name {
			case "include", "exclude", "exclude-dir", "regexp", "file", "max-count", "context", "after-context", "before-context":
				if !hasVal {
					v, err := value()
					if err != nil {
						return err
					}
					val = v
				}
			}
			switch name {
			case "include":
				o.includes = append(o.includes, val)
			case "exclude":
				o.excludes = append(o.excludes, val)
			case "exclude-dir":
				o.excludeDirs = append(o.excludeDirs, val)
			case "regexp":
				o.patterns, o.patternSet = append(o.patterns, val), true
			case "file":
				if err := o.patternFile(val); err != nil {
					return err
				}
			case "max-count", "context", "after-context", "before-context":
				n, err := intArg(val)
				if err != nil {
					return err
				}
				switch name {
				case "max-count":
					s.maxCount = n
				case "context":
					s.before, s.after = n, n
				case "after-context":
					s.after = n
				case "before-context":
					s.before = n
				}
			case "ignore-case":
				o.fold = true
			case "invert-match":
				s.invert = true
			case "line-number":
				s.lineNum = true
			case "count":
				s.count = true
			case "files-with-matches":
				s.filesWith = true
			case "files-without-match":
				s.filesWithout = true
			case "only-matching":
				s.only = true
			case "quiet", "silent":
				s.quiet = true
			case "no-messages":
				o.noMessages = true
			case "word-regexp":
				o.word = true
			case "line-regexp":
				o.line = true
			case "fixed-strings":
				o.fixed = true
			case "extended-regexp", "perl-regexp":
				o.extended = true
			case "recursive", "dereference-recursive":
				o.recursive = true
			case "no-filename":
				o.noName = true
			case "with-filename":
				o.forceName = true
			case "text":
				s.binary = "text"
			case "null":
				s.nullAfter = true
			case "color", "colour", "line-buffered", "binary-files":
				if name == "binary-files" && val == "without-match" {
					s.binary = "skip"
				}
			default:
				return fmt.Errorf("unrecognized option '%s'", a)
			}
			continue
		}
		if len(a) > 1 && a[0] == '-' {
			for k := 1; k < len(a); k++ {
				ch := a[k]
				rest := a[k+1:]
				takeVal := func() (string, error) {
					if rest != "" {
						k = len(a)
						return rest, nil
					}
					return value()
				}
				switch ch {
				case 'i', 'y':
					o.fold = true
				case 'v':
					s.invert = true
				case 'n':
					s.lineNum = true
				case 'c':
					s.count = true
				case 'l':
					s.filesWith = true
				case 'L':
					s.filesWithout = true
				case 'o':
					s.only = true
				case 'q':
					s.quiet = true
				case 's':
					o.noMessages = true
				case 'w':
					o.word = true
				case 'x':
					o.line = true
				case 'F':
					o.fixed = true
				case 'E', 'P':
					o.extended = true
				case 'G':
					o.extended = false
				case 'r', 'R':
					o.recursive = true
				case 'h':
					o.noName = true
				case 'H':
					o.forceName = true
				case 'a':
					s.binary = "text"
				case 'I':
					s.binary = "skip"
				case 'Z':
					s.nullAfter = true
				case 'e', 'f', 'm', 'A', 'B', 'C':
					v, err := takeVal()
					if err != nil {
						return err
					}
					switch ch {
					case 'e':
						o.patterns, o.patternSet = append(o.patterns, v), true
					case 'f':
						if err := o.patternFile(v); err != nil {
							return err
						}
					default:
						n, err := intArg(v)
						if err != nil {
							return err
						}
						switch ch {
						case 'm':
							s.maxCount = n
						case 'A':
							s.after = n
						case 'B':
							s.before = n
						case 'C':
							s.before, s.after = n, n
						}
					}
				default:
					if ch >= '0' && ch <= '9' {
						n, _ := strconv.Atoi(a[k:])
						s.before, s.after = n, n
						k = len(a)
						continue
					}
					return fmt.Errorf("invalid option -- '%c'", ch)
				}
			}
			continue
		}
		o.positional(a)
	}
	if !o.patternSet {
		return fmt.Errorf("usage: grep [OPTION]... PATTERNS [FILE]")
	}
	return nil
}

func (o *grepOpts) positional(a string) {
	if !o.patternSet {
		o.patterns = append(o.patterns, strings.Split(a, "\n")...)
		o.patternSet = true
		return
	}
	o.targets = append(o.targets, a)
}

func (o *grepOpts) patternFile(path string) error {
	data, err := os.ReadFile(ResolvePath(path)) //nolint:gosec // G703: shell commands act on the paths the user names
	if err != nil {
		return fmt.Errorf("%s: %s", path, describeErr(err))
	}
	o.patterns = append(o.patterns, strings.Split(strings.TrimRight(string(data), "\n"), "\n")...)
	o.patternSet = true
	return nil
}
