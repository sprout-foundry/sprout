package wasmshell

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

func init() {
	CmdRegistry["test"] = cmdTest
	CmdRegistry["["] = cmdBracket
}

func cmdTest(args []string, _ string) CmdResult {
	return runTest(&testParser{toks: args})
}

func cmdBracket(args []string, _ string) CmdResult {
	if len(args) == 0 || args[len(args)-1] != "]" {
		return CmdResult{Stdout: "", Stderr: "[: missing ']'\n", ExitCode: 2}
	}
	return runTest(&testParser{toks: args[:len(args)-1]})
}

func (sh *interp) evalDoubleBracket(raw []string) CmdResult {
	return runTest(&testParser{toks: raw, sh: sh})
}

func runTest(p *testParser) CmdResult {
	if len(p.toks) == 0 {
		return CmdResult{ExitCode: 1}
	}
	ok := p.or()
	if p.err == nil && p.pos < len(p.toks) {
		p.err = fmt.Errorf("unexpected argument %q", p.toks[p.pos])
	}
	if p.err != nil {
		return CmdResult{Stdout: "", Stderr: "test: " + p.err.Error() + "\n", ExitCode: 2}
	}
	return CmdResult{ExitCode: int(boolInt(!ok))}
}

// testParser evaluates test / [ arguments, or the raw words of [[ ]] when
// sh is set (operands are expanded then, and == matches glob patterns).
type testParser struct {
	toks []string
	pos  int
	sh   *interp
	err  error
}

func (p *testParser) orOp() string {
	if p.sh != nil {
		return "||"
	}
	return "-o"
}

func (p *testParser) andOp() string {
	if p.sh != nil {
		return "&&"
	}
	return "-a"
}

func (p *testParser) or() bool {
	v := p.and()
	for p.pos < len(p.toks) && p.toks[p.pos] == p.orOp() {
		p.pos++
		r := p.and()
		v = v || r
	}
	return v
}

func (p *testParser) and() bool {
	v := p.not()
	for p.pos < len(p.toks) && p.toks[p.pos] == p.andOp() {
		p.pos++
		r := p.not()
		v = v && r
	}
	return v
}

func (p *testParser) not() bool {
	if p.pos < len(p.toks)-1 && p.toks[p.pos] == "!" {
		p.pos++
		return !p.not()
	}
	return p.primary()
}

func (p *testParser) value(raw string) string {
	if p.sh == nil {
		return raw
	}
	v, err := p.sh.expandString(raw)
	if err != nil && p.err == nil {
		p.err = err
	}
	return v
}

var testBinary = words("=", "==", "!=", "<", ">", "=~", "-eq", "-ne", "-lt", "-le", "-gt", "-ge", "-nt", "-ot", "-ef")

func (p *testParser) primary() bool {
	if p.pos >= len(p.toks) {
		p.err = fmt.Errorf("argument expected")
		return false
	}
	t := p.toks[p.pos]
	if t == "(" && p.pos+1 < len(p.toks) {
		p.pos++
		v := p.or()
		if p.pos >= len(p.toks) || p.toks[p.pos] != ")" {
			p.err = fmt.Errorf("missing ')'")
			return false
		}
		p.pos++
		return v
	}
	if p.pos+2 < len(p.toks) && testBinary[p.toks[p.pos+1]] {
		left, op, right := p.value(t), p.toks[p.pos+1], p.toks[p.pos+2]
		p.pos += 3
		return p.binary(left, op, right)
	}
	if len(t) == 2 && t[0] == '-' && p.pos+1 < len(p.toks) && strings.ContainsRune("efdsrwxLhznbcpSgukOGNt", rune(t[1])) {
		arg := p.value(p.toks[p.pos+1])
		p.pos += 2
		return unaryTest(t, arg)
	}
	p.pos++
	return p.value(t) != ""
}

func (p *testParser) binary(left, op, rightRaw string) bool {
	switch op {
	case "=", "==", "!=":
		if p.sh != nil {
			re, err := p.sh.expandPattern(rightRaw)
			if err != nil {
				p.err = err
				return false
			}
			return re.MatchString(left) == (op != "!=")
		}
		return (left == rightRaw) == (op != "!=")
	case "=~":
		pat := p.value(rightRaw)
		re, err := regexp.Compile(pat)
		if err != nil {
			p.err = fmt.Errorf("invalid regex %q", pat)
			return false
		}
		m := re.FindString(left)
		if p.sh != nil {
			p.sh.setVar("BASH_REMATCH", m)
		}
		return re.MatchString(left)
	}
	right := p.value(rightRaw)
	switch op {
	case "<":
		return left < right
	case ">":
		return left > right
	case "-nt", "-ot":
		a, errA := os.Stat(ResolvePath(left))
		b, errB := os.Stat(ResolvePath(right))
		if op == "-nt" {
			return errA == nil && (errB != nil || a.ModTime().After(b.ModTime()))
		}
		return errB == nil && (errA != nil || a.ModTime().Before(b.ModTime()))
	case "-ef":
		return ResolvePath(left) == ResolvePath(right)
	}
	x, errX := strconv.ParseInt(strings.TrimSpace(left), 10, 64)
	y, errY := strconv.ParseInt(strings.TrimSpace(right), 10, 64)
	if errX != nil || errY != nil {
		bad := left
		if errX == nil {
			bad = right
		}
		p.err = fmt.Errorf("%s: integer expression expected", bad)
		return false
	}
	switch op {
	case "-eq":
		return x == y
	case "-ne":
		return x != y
	case "-lt":
		return x < y
	case "-le":
		return x <= y
	case "-gt":
		return x > y
	case "-ge":
		return x >= y
	}
	return false
}

func unaryTest(op, arg string) bool {
	switch op {
	case "-z":
		return arg == ""
	case "-n":
		return arg != ""
	case "-t":
		return false
	}
	path := ResolvePath(arg)
	if arg == "" {
		return false
	}
	if op == "-L" || op == "-h" {
		info, err := os.Lstat(path)
		return err == nil && info.Mode()&os.ModeSymlink != 0
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	switch op {
	case "-e", "-r", "-w", "-O", "-G", "-N":
		return true
	case "-f":
		return info.Mode().IsRegular()
	case "-d":
		return info.IsDir()
	case "-s":
		return info.Size() > 0
	case "-x":
		return info.IsDir() || info.Mode()&0o111 != 0
	case "-p":
		return info.Mode()&os.ModeNamedPipe != 0
	case "-S":
		return info.Mode()&os.ModeSocket != 0
	case "-b", "-c":
		return info.Mode()&os.ModeDevice != 0
	case "-g":
		return info.Mode()&os.ModeSetgid != 0
	case "-u":
		return info.Mode()&os.ModeSetuid != 0
	case "-k":
		return info.Mode()&os.ModeSticky != 0
	}
	return false
}
