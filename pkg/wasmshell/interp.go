package wasmshell

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type flowKind int

const (
	flowNone flowKind = iota
	flowBreak
	flowContinue
	flowReturn
	flowExit
)

// maxLoopIterations stops a runaway loop before it freezes the browser tab.
const maxLoopIterations = 1_000_000

const maxFuncDepth = 200

// interp is the state of one shell run. Variables, the working directory
// and functions are shared across runs (the terminal is one session);
// options, positional parameters and control flow belong to the run.
type interp struct {
	name       string
	positional []string
	lastExit   int
	lastSubst  int
	errexit    bool
	nounset    bool
	pipefail   bool
	xtrace     bool
	flow       flowKind
	flowN      int
	flowCode   int
	condDepth  int
	funcDepth  int
	locals     []map[string]*string
	sideErr    strings.Builder
}

var (
	functions    = map[string]command{}
	lastExitCode int
)

// ioIn is a command's standard input; read consumes it line by line.
type ioIn struct {
	data string
	pos  int
}

func (in *ioIn) rest() string {
	if in == nil {
		return ""
	}
	return in.data[in.pos:]
}

// readLine returns the next line without its newline; ok is false when
// the input ended before a newline (the line may still hold text).
func (in *ioIn) readLine(delim byte) (line string, ok bool) {
	if in == nil || in.pos >= len(in.data) {
		return "", false
	}
	rest := in.data[in.pos:]
	i := strings.IndexByte(rest, delim)
	if i < 0 {
		in.pos = len(in.data)
		return rest, false
	}
	in.pos += i + 1
	return rest[:i], true
}

// ParseAndExecute runs a shell command line or script: lists, pipelines,
// redirections, control flow, functions and expansions, over the built-in
// commands.
func ParseAndExecute(input string) CmdResult {
	if strings.TrimSpace(input) == "" {
		return CmdResult{"", "", 0}
	}
	addToHistory(input)
	sh := &interp{name: "sh", lastExit: lastExitCode}
	res := sh.runSource(input, nil)
	lastExitCode = res.ExitCode
	return res
}

func parseFailure(err error) CmdResult {
	var se *shellError
	if errors.As(err, &se) && se.unsupported {
		return CmdResult{"", "sh: " + err.Error() + "\n", ExitCommandNotFound}
	}
	return CmdResult{"", "sh: " + err.Error() + "\n", 2}
}

func (sh *interp) runSource(src string, in *ioIn) CmdResult {
	list, err := parseScript(src)
	if err != nil {
		return parseFailure(err)
	}
	res := sh.execList(list, in)
	if sh.flow == flowExit {
		res.ExitCode = sh.flowCode
	}
	sh.flow = flowNone
	return res
}

func (r *CmdResult) add(o CmdResult) {
	r.Stdout += o.Stdout
	r.Stderr += o.Stderr
	r.ExitCode = o.ExitCode
}

func (sh *interp) execList(l *listNode, in *ioIn) CmdResult {
	var out CmdResult
	for _, ao := range l.items {
		r, finalRan := sh.execAndOr(ao, in)
		out.add(r)
		if sh.flow != flowNone {
			break
		}
		if sh.errexit && r.ExitCode != 0 && sh.condDepth == 0 && finalRan {
			sh.flow, sh.flowCode = flowExit, r.ExitCode
			break
		}
	}
	return out
}

func (sh *interp) execAndOr(ao *andOrNode, in *ioIn) (CmdResult, bool) {
	var out CmdResult
	code, finalRan := 0, false
	for i, p := range ao.pipes {
		if i > 0 {
			op := ao.ops[i-1]
			if (op == "&&" && code != 0) || (op == "||" && code == 0) {
				continue
			}
		}
		last := i == len(ao.pipes)-1
		if !last {
			sh.condDepth++
		}
		r := sh.execPipeline(p, in)
		if !last {
			sh.condDepth--
		}
		out.add(r)
		code, finalRan = r.ExitCode, last
		if sh.flow != flowNone {
			break
		}
	}
	out.ExitCode = code
	return out, finalRan
}

func (sh *interp) execPipeline(p *pipelineNode, in *ioIn) CmdResult {
	if p.negate {
		sh.condDepth++
		defer func() { sh.condDepth-- }()
	}
	var out CmdResult
	stdin := in
	codes := make([]int, 0, len(p.cmds))
	for i, c := range p.cmds {
		if i > 0 {
			stdin = &ioIn{data: out.Stdout}
		}
		r := sh.execCommand(c, stdin)
		out.Stdout = r.Stdout
		out.Stderr += r.Stderr
		codes = append(codes, r.ExitCode)
		// Stages of a real pipeline run in subshells: exit or break there
		// ends only that stage.
		if len(p.cmds) > 1 && sh.flow != flowReturn {
			sh.flow = flowNone
		}
	}
	code := codes[len(codes)-1]
	if sh.pipefail {
		for _, c := range codes {
			if c != 0 {
				code = c
			}
		}
	}
	if p.negate {
		code = int(boolInt(code == 0))
	}
	out.ExitCode = code
	sh.lastExit = code
	return out
}

func (sh *interp) execCommand(c command, in *ioIn) CmdResult {
	switch c := c.(type) {
	case *simpleCmd:
		return sh.execSimple(c, in)
	case *groupCmd:
		return sh.withRedirs(c.redirs, in, func(in *ioIn) CmdResult {
			if !c.subshell {
				return sh.execList(c.body, in)
			}
			child := sh.child()
			return runIsolated(func() CmdResult {
				r := child.execList(c.body, in)
				if child.flow == flowExit {
					r.ExitCode = child.flowCode
				}
				return r
			})
		})
	case *ifCmd:
		return sh.withRedirs(c.redirs, in, func(in *ioIn) CmdResult { return sh.execIf(c, in) })
	case *loopCmd:
		return sh.withRedirs(c.redirs, in, func(in *ioIn) CmdResult { return sh.execWhile(c, in) })
	case *forCmd:
		return sh.withRedirs(c.redirs, in, func(in *ioIn) CmdResult { return sh.execFor(c, in) })
	case *caseCmd:
		return sh.withRedirs(c.redirs, in, func(in *ioIn) CmdResult { return sh.execCase(c, in) })
	case *funcDef:
		functions[c.name] = c.body
		return CmdResult{}
	case *arithCmd:
		return sh.withRedirs(c.redirs, in, func(*ioIn) CmdResult {
			n, err := sh.arith(c.expr)
			if err != nil {
				return errResult(err)
			}
			return CmdResult{ExitCode: int(boolInt(n == 0))}
		})
	case *condCmd:
		return sh.withRedirs(c.redirs, in, func(*ioIn) CmdResult { return sh.evalDoubleBracket(c.words) })
	}
	return CmdResult{"", "sh: unknown command node\n", 2}
}

func errResult(err error) CmdResult {
	var se *shellError
	if errors.As(err, &se) && se.unsupported {
		return CmdResult{"", "sh: " + err.Error() + "\n", ExitCommandNotFound}
	}
	return CmdResult{"", "sh: " + err.Error() + "\n", 1}
}

func (sh *interp) child() *interp {
	return &interp{
		name:       sh.name,
		positional: append([]string(nil), sh.positional...),
		lastExit:   sh.lastExit,
		errexit:    sh.errexit,
		nounset:    sh.nounset,
		pipefail:   sh.pipefail,
	}
}

// runIsolated runs fn as a subshell: directory and variable changes made
// inside don't leak out.
func runIsolated(fn func() CmdResult) CmdResult {
	cwd, _ := os.Getwd()
	saved := ShellEnv.All()
	r := fn()
	if cwd != "" {
		_ = os.Chdir(cwd)
	}
	for k := range ShellEnv.Vars {
		if _, ok := saved[k]; !ok {
			delete(ShellEnv.Vars, k)
			_ = os.Unsetenv(k)
		}
	}
	for k, v := range saved {
		if ShellEnv.Vars[k] != v {
			ShellEnv.Set(k, v)
		}
	}
	return r
}

// substitute runs a command substitution and returns its output without
// trailing newlines; its stderr surfaces with the enclosing command.
func (sh *interp) substitute(src string) string {
	child := sh.child()
	r := runIsolated(func() CmdResult { return child.runSource(src, nil) })
	sh.sideErr.WriteString(r.Stderr)
	sh.lastSubst = r.ExitCode
	return strings.TrimRight(r.Stdout, "\n")
}

func (sh *interp) setVar(name, val string) {
	ShellEnv.Set(name, val)
}

func (sh *interp) unsetVar(name string) {
	delete(ShellEnv.Vars, name)
	_ = os.Unsetenv(name)
}

func (sh *interp) execSimple(c *simpleCmd, in *ioIn) CmdResult {
	sh.lastSubst = 0
	var argv []string
	for _, w := range c.words {
		fs, err := sh.expandFields(w)
		if err != nil {
			return sh.flushSideErr(errResult(err))
		}
		argv = append(argv, fs...)
	}
	type assignment struct{ name, val string }
	var assigns []assignment
	for _, a := range c.assigns {
		name, raw, _ := strings.Cut(a, "=")
		val, err := sh.expandString(raw)
		if err != nil {
			return sh.flushSideErr(errResult(err))
		}
		if strings.HasSuffix(name, "+") {
			name = strings.TrimSuffix(name, "+")
			old, _ := sh.lookup(name)
			val = old + val
		}
		assigns = append(assigns, assignment{name, val})
	}

	if len(argv) == 0 {
		for _, a := range assigns {
			sh.setVar(a.name, a.val)
		}
		code := sh.lastSubst
		r := sh.withRedirs(c.redirs, in, func(*ioIn) CmdResult { return CmdResult{ExitCode: code} })
		return sh.flushSideErr(r)
	}

	type saved struct {
		val string
		ok  bool
	}
	restore := map[string]saved{}
	for _, a := range assigns {
		old, ok := ShellEnv.Vars[a.name]
		restore[a.name] = saved{old, ok}
		sh.setVar(a.name, a.val)
	}
	if sh.xtrace {
		sh.sideErr.WriteString("+ " + strings.Join(argv, " ") + "\n")
	}
	r := sh.withRedirs(c.redirs, in, func(in *ioIn) CmdResult { return sh.run(argv, in) })
	for name, s := range restore {
		if s.ok {
			sh.setVar(name, s.val)
		} else {
			sh.unsetVar(name)
		}
	}
	return sh.flushSideErr(r)
}

func (sh *interp) flushSideErr(r CmdResult) CmdResult {
	if sh.sideErr.Len() > 0 {
		r.Stderr = sh.sideErr.String() + r.Stderr
		sh.sideErr.Reset()
	}
	sh.lastExit = r.ExitCode
	return r
}

// run dispatches one command: shell builtins, functions, commands, then
// shell scripts named by path.
func (sh *interp) run(argv []string, in *ioIn) CmdResult {
	name := argv[0]
	if fn, ok := shellBuiltins[name]; ok {
		return fn(sh, argv[1:], in)
	}
	if body, ok := functions[name]; ok {
		return sh.callFunction(body, argv[1:], in)
	}
	if fn, ok := CmdRegistry[name]; ok {
		return fn(argv[1:], in.rest())
	}
	if strings.Contains(name, "/") {
		return sh.runScriptFile(name, argv[1:], in)
	}
	return CmdResult{"", fmt.Sprintf("command not found: %s\n", name), ExitCommandNotFound}
}

// runArgv runs an already-expanded command line with the given input
// (xargs, find -exec, env, time).
func (sh *interp) runArgv(argv []string, stdin string) CmdResult {
	if len(argv) == 0 {
		return CmdResult{}
	}
	return sh.run(argv, &ioIn{data: stdin})
}

func (sh *interp) callFunction(body command, args []string, in *ioIn) CmdResult {
	if sh.funcDepth >= maxFuncDepth {
		return CmdResult{"", "sh: maximum function nesting level exceeded\n", 1}
	}
	savedPos := sh.positional
	sh.positional = args
	sh.locals = append(sh.locals, map[string]*string{})
	sh.funcDepth++
	r := sh.execCommand(body, in)
	if sh.flow == flowReturn {
		sh.flow = flowNone
		r.ExitCode = sh.flowCode
	}
	frame := sh.locals[len(sh.locals)-1]
	for name, old := range frame {
		if old == nil {
			sh.unsetVar(name)
		} else {
			sh.setVar(name, *old)
		}
	}
	sh.locals = sh.locals[:len(sh.locals)-1]
	sh.funcDepth--
	sh.positional = savedPos
	return r
}
