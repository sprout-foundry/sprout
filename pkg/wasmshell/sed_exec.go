package wasmshell

import (
	"strconv"
	"strings"
)

// run executes the program over the input, returning output and the exit
// code a q/Q command set.
func (p *sedProg) run(input string, quiet bool) (string, int) {
	lines := strings.Split(input, "\n")
	trailingNewline := strings.HasSuffix(input, "\n")
	if trailingNewline || input == "" {
		lines = lines[:len(lines)-1]
	}
	st := &sedState{prog: p, lines: lines, quiet: quiet}
	for _, c := range allSedCmds(p.cmds) {
		c.inRange = false
	}
	st.exec()
	out := st.out.String()
	if !trailingNewline && strings.HasSuffix(out, "\n") && input != "" {
		out = strings.TrimSuffix(out, "\n")
	}
	return out, st.exitCode
}

func allSedCmds(cmds []*sedCmd) []*sedCmd {
	var all []*sedCmd
	for _, c := range cmds {
		all = append(all, c)
		all = append(all, allSedCmds(c.block)...)
	}
	return all
}

type sedState struct {
	prog     *sedProg
	lines    []string
	idx      int
	lineNo   int
	pattern  string
	hold     string
	quiet    bool
	out      strings.Builder
	appendQ  []string
	subbed   bool
	quit     bool
	exitCode int
}

func (st *sedState) nextLine() (string, bool) {
	if st.idx >= len(st.lines) {
		return "", false
	}
	l := st.lines[st.idx]
	st.idx++
	st.lineNo++
	return l, true
}

func (st *sedState) isLast() bool { return st.idx >= len(st.lines) }

func (st *sedState) flushAppend() {
	for _, a := range st.appendQ {
		st.out.WriteString(a + "\n")
	}
	st.appendQ = nil
}

func (st *sedState) exec() {
	for !st.quit {
		line, ok := st.nextLine()
		if !ok {
			return
		}
		st.pattern = line
		st.subbed = false
		deleted := st.runCmds(st.prog.cmds)
		if !deleted && !st.quiet {
			st.out.WriteString(st.pattern + "\n")
		}
		st.flushAppend()
	}
}

// runCmds runs a command list for the current cycle; it reports whether
// the pattern space was deleted (no auto-print).
func (st *sedState) runCmds(cmds []*sedCmd) bool {
	labels := map[string]int{}
	for k, c := range cmds {
		if c.name == ':' {
			labels[c.label] = k
		}
	}
	for pc := 0; pc < len(cmds); pc++ {
		c := cmds[pc]
		if !st.matches(c) {
			continue
		}
		switch c.name {
		case '{':
			if st.runCmds(c.block) {
				return true
			}
			if st.quit {
				return false
			}
		case 's':
			st.substitute(c)
		case 'd':
			return true
		case 'D':
			if i := strings.IndexByte(st.pattern, '\n'); i >= 0 {
				st.pattern = st.pattern[i+1:]
				st.flushAppend()
				pc = -1
				continue
			}
			return true
		case 'p':
			st.out.WriteString(st.pattern + "\n")
		case 'P':
			first, _, _ := strings.Cut(st.pattern, "\n")
			st.out.WriteString(first + "\n")
		case 'n':
			if !st.quiet {
				st.out.WriteString(st.pattern + "\n")
			}
			st.flushAppend()
			line, ok := st.nextLine()
			if !ok {
				st.quit = true
				return true
			}
			st.pattern = line
		case 'N':
			line, ok := st.nextLine()
			if !ok {
				st.quit = true
				return false
			}
			st.pattern += "\n" + line
		case 'q':
			st.quit, st.exitCode = true, c.exitCode
			return false
		case 'Q':
			st.quit, st.exitCode = true, c.exitCode
			return true
		case 'a':
			st.appendQ = append(st.appendQ, c.text)
		case 'i':
			st.out.WriteString(c.text + "\n")
		case 'c':
			if c.a2 == nil || !c.inRange {
				st.out.WriteString(c.text + "\n")
			}
			return true
		case '=':
			st.out.WriteString(strconv.Itoa(st.lineNo) + "\n")
		case 'y':
			rs := []rune(st.pattern)
			for k, r := range rs {
				for j, f := range c.from {
					if r == f {
						rs[k] = c.to[j]
						break
					}
				}
			}
			st.pattern = string(rs)
		case 'h':
			st.hold = st.pattern
		case 'H':
			st.hold += "\n" + st.pattern
		case 'g':
			st.pattern = st.hold
		case 'G':
			st.pattern += "\n" + st.hold
		case 'x':
			st.pattern, st.hold = st.hold, st.pattern
		case 'z':
			st.pattern = ""
		case 'l':
			st.out.WriteString(strconv.Quote(st.pattern) + "$\n")
		case 'b', 't', 'T':
			jump := c.name == 'b' || (c.name == 't' && st.subbed) || (c.name == 'T' && !st.subbed)
			if c.name != 'b' {
				st.subbed = false
			}
			if !jump {
				continue
			}
			if c.label == "" {
				return false
			}
			target, ok := labels[c.label]
			if !ok {
				return false
			}
			pc = target
		}
	}
	return false
}

func (st *sedState) addrMatch(a *sedAddr) bool {
	switch a.kind {
	case 'n':
		return st.lineNo == a.line
	case '$':
		return st.isLast()
	case '~':
		if a.step <= 0 {
			return st.lineNo == a.line
		}
		return st.lineNo >= a.line && (st.lineNo-a.line)%a.step == 0
	case '/':
		return a.re.MatchString(st.pattern)
	}
	return false
}

func (st *sedState) matches(c *sedCmd) bool {
	var m bool
	switch {
	case c.a1 == nil:
		m = true
	case c.a2 == nil:
		m = st.addrMatch(c.a1)
	case c.inRange:
		m = true
		switch {
		case c.a2.plusN > 0 || (c.a2.kind == 'n' && c.a2.plusN == 0 && c.rangeEnd > 0):
			if st.lineNo >= c.rangeEnd {
				c.inRange = false
			}
		case c.a2.kind == 'n':
			if st.lineNo >= c.a2.line {
				c.inRange = false
			}
		default:
			if st.addrMatch(c.a2) {
				c.inRange = false
			}
		}
	default:
		if st.addrMatch(c.a1) {
			m = true
			c.inRange = true
			switch {
			case c.a2.plusN > 0:
				c.rangeEnd = st.lineNo + c.a2.plusN
			case c.a2.kind == 'n':
				c.rangeEnd = 0
				if c.a2.line <= st.lineNo {
					c.inRange = false
				}
			case c.a2.kind == '$':
				if st.isLast() {
					c.inRange = false
				}
			}
		}
	}
	return m != c.negate
}

func (st *sedState) substitute(c *sedCmd) {
	replaceIt := func(count int) bool {
		switch {
		case c.nth == 0:
			return c.global || count == 1
		case c.global:
			return count >= c.nth
		}
		return count == c.nth
	}
	var b strings.Builder
	last := 0
	replaced := false
	for count, loc := range c.re.FindAllStringSubmatchIndex(st.pattern, -1) {
		if !replaceIt(count + 1) {
			continue
		}
		groups := make([]string, len(loc)/2)
		for g := range groups {
			if loc[2*g] >= 0 {
				groups[g] = st.pattern[loc[2*g]:loc[2*g+1]]
			}
		}
		b.WriteString(st.pattern[last:loc[0]])
		b.WriteString(expandSedReplacement(groups, c.repl))
		last = loc[1]
		replaced = true
	}
	if !replaced {
		return
	}
	b.WriteString(st.pattern[last:])
	st.pattern = b.String()
	st.subbed = true
	if c.print {
		st.out.WriteString(st.pattern + "\n")
	}
}

// expandSedReplacement renders a replacement: & and \1-\9 insert matches,
// \n and \t are control characters, \U \L \u \l \E change case.
func expandSedReplacement(groups []string, repl string) string {
	match := groups[0]
	var b strings.Builder
	caseMode := byte(0)
	oneShot := byte(0)
	write := func(s string) {
		for _, r := range s {
			str := string(r)
			switch {
			case oneShot == 'u':
				str, oneShot = strings.ToUpper(str), 0
			case oneShot == 'l':
				str, oneShot = strings.ToLower(str), 0
			case caseMode == 'U':
				str = strings.ToUpper(str)
			case caseMode == 'L':
				str = strings.ToLower(str)
			}
			b.WriteString(str)
		}
	}
	for i := 0; i < len(repl); i++ {
		c := repl[i]
		if c == '&' {
			write(match)
			continue
		}
		if c != '\\' || i+1 >= len(repl) {
			write(string(c))
			continue
		}
		i++
		n := repl[i]
		switch {
		case n >= '0' && n <= '9':
			if k := int(n - '0'); k < len(groups) {
				write(groups[k])
			}
		case n == 'n':
			write("\n")
		case n == 't':
			write("\t")
		case n == 'U' || n == 'L':
			caseMode = n
		case n == 'E':
			caseMode = 0
		case n == 'u' || n == 'l':
			oneShot = n
		default:
			write(string(n))
		}
	}
	return b.String()
}
