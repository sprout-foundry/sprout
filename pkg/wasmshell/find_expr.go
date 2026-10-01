package wasmshell

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func (f *findRun) peek() string {
	if f.pos < len(f.args) {
		return f.args[f.pos]
	}
	return ""
}

func (f *findRun) or() findExpr {
	left := f.and()
	for f.err == nil && (f.peek() == "-o" || f.peek() == "-or") {
		f.pos++
		right := f.and()
		l, r := left, right
		left = func(e *findEntry) bool { return l(e) || r(e) }
	}
	return left
}

func (f *findRun) and() findExpr {
	left := f.not()
	for f.err == nil {
		p := f.peek()
		switch p {
		case "-a", "-and":
			f.pos++
		case "", ")", "-o", "-or":
			return left
		}
		right := f.not()
		if right == nil {
			return left
		}
		l, r := left, right
		left = func(e *findEntry) bool { return l(e) && r(e) }
	}
	return left
}

func (f *findRun) not() findExpr {
	if p := f.peek(); p == "!" || p == "-not" {
		f.pos++
		inner := f.not()
		if inner == nil {
			return nil
		}
		return func(e *findEntry) bool { return !inner(e) }
	}
	return f.primary()
}

func (f *findRun) arg(name string) string {
	if f.pos >= len(f.args) {
		f.err = fmt.Errorf("missing argument to `%s'", name)
		return ""
	}
	f.pos++
	return f.args[f.pos-1]
}

func (f *findRun) primary() findExpr {
	if f.pos >= len(f.args) {
		return nil
	}
	tok := f.args[f.pos]
	f.pos++
	switch tok {
	case "(":
		e := f.or()
		if f.peek() != ")" {
			f.err = fmt.Errorf("missing ')'")
			return nil
		}
		f.pos++
		return e
	case "-name", "-iname":
		pat := f.arg(tok)
		fold := tok == "-iname"
		return func(e *findEntry) bool { return matchFold(pat, filepath.Base(e.abs), fold) }
	case "-path", "-wholename", "-ipath", "-iwholename":
		pat := filepath.ToSlash(f.arg(tok))
		fold := strings.HasPrefix(tok, "-i")
		return func(e *findEntry) bool { return matchFold(pat, filepath.ToSlash(e.display), fold) }
	case "-regex", "-iregex":
		src := f.arg(tok)
		if tok == "-iregex" {
			src = "(?i)" + src
		}
		re, err := regexp.Compile("^(?:" + src + ")$")
		if err != nil {
			f.err = fmt.Errorf("invalid regex: %s", src)
			return nil
		}
		return func(e *findEntry) bool { return re.MatchString(filepath.ToSlash(e.display)) }
	case "-type":
		types := strings.Split(f.arg(tok), ",")
		return func(e *findEntry) bool {
			for _, t := range types {
				switch {
				case t == "f" && e.info.Mode().IsRegular(),
					t == "d" && e.info.IsDir(),
					t == "l" && e.info.Mode()&os.ModeSymlink != 0:
					return true
				}
			}
			return false
		}
	case "-size":
		return f.sizeTest(f.arg(tok))
	case "-empty":
		return func(e *findEntry) bool {
			if e.info.IsDir() {
				entries, err := ReadDirCompat(e.abs)
				return err == nil && len(entries) == 0
			}
			return e.info.Size() == 0
		}
	case "-newer":
		ref, err := os.Stat(ResolvePath(f.arg(tok)))
		if err != nil {
			f.err = fmt.Errorf("%s", describeErr(err))
			return nil
		}
		return func(e *findEntry) bool { return e.info.ModTime().After(ref.ModTime()) }
	case "-mtime", "-mmin":
		unit := 24 * time.Hour
		if tok == "-mmin" {
			unit = time.Minute
		}
		cmp, n, ok := parseFindNumber(f.arg(tok))
		if !ok {
			f.err = fmt.Errorf("invalid argument to %s", tok)
			return nil
		}
		return func(e *findEntry) bool {
			age := math.Floor(float64(time.Since(e.info.ModTime())) / float64(unit))
			return compareFind(cmp, int64(age), n)
		}
	case "-true", "-readable", "-writable":
		return func(*findEntry) bool { return true }
	case "-false":
		return func(*findEntry) bool { return false }
	case "-executable":
		return func(e *findEntry) bool { return e.info.IsDir() || e.info.Mode()&0o111 != 0 }
	case "-prune":
		return func(e *findEntry) bool { e.prune = true; return true }
	case "-quit":
		return func(*findEntry) bool { f.quit = true; return true }
	case "-print", "-print0":
		f.hasAction = true
		end := "\n"
		if tok == "-print0" {
			end = "\x00"
		}
		return func(e *findEntry) bool { f.out.WriteString(e.display + end); return true }
	case "-printf":
		f.hasAction = true
		format := f.arg(tok)
		return func(e *findEntry) bool { f.out.WriteString(findPrintf(format, e)); return true }
	case "-delete":
		f.hasAction = true
		return func(e *findEntry) bool {
			if e.depth > 0 || e.display != "." {
				f.deletes = append(f.deletes, e.abs)
			}
			return true
		}
	case "-exec":
		return f.execAction()
	}
	f.err = fmt.Errorf("predicate %s is not supported by the in-browser shell", tok)
	return nil
}

func matchFold(pat, s string, fold bool) bool {
	if fold {
		pat, s = strings.ToLower(pat), strings.ToLower(s)
	}
	return globMatch(pat, s)
}

func (f *findRun) execAction() findExpr {
	f.hasAction = true
	var argv []string
	for f.pos < len(f.args) {
		a := f.args[f.pos]
		f.pos++
		if a == ";" {
			return func(e *findEntry) bool {
				cmd := make([]string, len(argv))
				for k, x := range argv {
					cmd[k] = strings.ReplaceAll(x, "{}", e.display)
				}
				r := f.sh.runArgv(cmd, "")
				f.out.WriteString(r.Stdout)
				f.errs.WriteString(r.Stderr)
				if r.ExitCode == ExitCommandNotFound {
					f.code = ExitCommandNotFound
				}
				return r.ExitCode == 0
			}
		}
		if a == "+" && len(argv) > 0 && argv[len(argv)-1] == "{}" {
			b := &findBatch{argv: argv}
			f.batches = append(f.batches, b)
			return func(e *findEntry) bool { b.paths = append(b.paths, e.display); return true }
		}
		argv = append(argv, a)
	}
	f.err = fmt.Errorf("missing argument to `-exec'")
	return nil
}

func parseFindNumber(s string) (byte, int64, bool) {
	cmp := byte('=')
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") {
		cmp, s = s[0], s[1:]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return cmp, n, err == nil
}

func compareFind(cmp byte, v, n int64) bool {
	switch cmp {
	case '+':
		return v > n
	case '-':
		return v < n
	}
	return v == n
}

func (f *findRun) sizeTest(spec string) findExpr {
	unit := int64(512)
	if spec != "" {
		switch spec[len(spec)-1] {
		case 'c':
			unit = 1
		case 'w':
			unit = 2
		case 'k':
			unit = 1024
		case 'M':
			unit = 1024 * 1024
		case 'G':
			unit = 1024 * 1024 * 1024
		case 'b':
			unit = 512
		}
		if !isDigit(spec[len(spec)-1]) {
			spec = spec[:len(spec)-1]
		}
	}
	cmp, n, ok := parseFindNumber(spec)
	if !ok {
		f.err = fmt.Errorf("invalid -size argument")
		return nil
	}
	return func(e *findEntry) bool {
		size := (e.info.Size() + unit - 1) / unit
		return compareFind(cmp, size, n)
	}
}

func findPrintf(format string, e *findEntry) string {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c == '\\' && i+1 < len(format) {
			b.WriteString(unescapeC(format[i:i+2], false))
			i++
			continue
		}
		if c != '%' || i+1 >= len(format) {
			b.WriteByte(c)
			continue
		}
		i++
		switch format[i] {
		case 'p':
			b.WriteString(e.display)
		case 'f':
			b.WriteString(filepath.Base(e.abs))
		case 'h':
			b.WriteString(filepath.Dir(e.display))
		case 'P':
			if e.rel != "." {
				b.WriteString(e.rel)
			}
		case 's':
			b.WriteString(strconv.FormatInt(e.info.Size(), 10))
		case 'd':
			b.WriteString(strconv.Itoa(e.depth))
		case 'm':
			fmt.Fprintf(&b, "%o", e.info.Mode().Perm())
		case 'y':
			b.WriteString(findTypeLetter(e.info))
		case 't':
			b.WriteString(e.info.ModTime().Format("Mon Jan _2 15:04:05 2006"))
		case 'T':
			if i+1 < len(format) && format[i+1] == '@' {
				i++
				b.WriteString(strconv.FormatInt(e.info.ModTime().Unix(), 10))
			}
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(format[i])
		}
	}
	return b.String()
}

func findTypeLetter(info os.FileInfo) string {
	switch {
	case info.IsDir():
		return "d"
	case info.Mode()&os.ModeSymlink != 0:
		return "l"
	}
	return "f"
}
