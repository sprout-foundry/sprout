package wasmshell

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// shellBuiltin is a command that needs the running shell: its variables,
// control flow, input stream, or the ability to run other commands.
type shellBuiltin func(sh *interp, args []string, in *ioIn) CmdResult

var shellBuiltins map[string]shellBuiltin

var shellKeywords = words("if", "then", "elif", "else", "fi", "for", "in", "do", "done", "while", "until", "case", "esac", "function", "{", "}", "!", "[[", "]]")

func init() {
	shellBuiltins = map[string]shellBuiltin{
		"exit":     biExit,
		"return":   biReturn,
		"break":    func(sh *interp, a []string, _ *ioIn) CmdResult { return biLoop(sh, a, flowBreak) },
		"continue": func(sh *interp, a []string, _ *ioIn) CmdResult { return biLoop(sh, a, flowContinue) },
		"shift":    biShift,
		"set":      biSet,
		"local":    biLocal,
		"declare":  biDeclare,
		"typeset":  biDeclare,
		"readonly": biDeclare,
		"unset":    biUnset,
		"eval":     biEval,
		"source":   biSource,
		".":        biSource,
		"read":     biRead,
		"sh":       biSh,
		"bash":     biSh,
		"dash":     biSh,
		"command":  biCommand,
		"type":     biType,
		"exec":     biExec,
		"let":      biLet,
		"env":      biEnv,
		"time":     biTime,
		"timeout":  biWrapper,
		"nice":     biWrapper,
		"nohup":    biWrapper,
		"stdbuf":   biWrapper,
		"xargs":    biXargs,
		"find":     biFind,
	}
	for _, name := range []string{"wait", "trap", "hash", "shopt", "ulimit", "disown"} {
		shellBuiltins[name] = func(*interp, []string, *ioIn) CmdResult { return CmdResult{} }
	}
	shellBuiltins["umask"] = func(*interp, []string, *ioIn) CmdResult { return CmdResult{"0022\n", "", 0} }
}

func usage(name, msg string, code int) CmdResult {
	return CmdResult{"", fmt.Sprintf("%s: %s\n", name, msg), code}
}

func biExit(sh *interp, args []string, _ *ioIn) CmdResult {
	code := sh.lastExit
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return usage("exit", args[0]+": numeric argument required", 2)
		}
		code = n & 0xff
	}
	sh.flow, sh.flowCode = flowExit, code
	return CmdResult{ExitCode: code}
}

func biReturn(sh *interp, args []string, _ *ioIn) CmdResult {
	code := sh.lastExit
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return usage("return", args[0]+": numeric argument required", 2)
		}
		code = n & 0xff
	}
	sh.flow, sh.flowCode = flowReturn, code
	return CmdResult{ExitCode: code}
}

func biLoop(sh *interp, args []string, kind flowKind) CmdResult {
	n := 1
	if len(args) > 0 {
		v, err := strconv.Atoi(args[0])
		if err != nil || v < 1 {
			return usage("break", "loop count out of range", 1)
		}
		n = v
	}
	sh.flow, sh.flowN = kind, n
	return CmdResult{}
}

func biShift(sh *interp, args []string, _ *ioIn) CmdResult {
	n := 1
	if len(args) > 0 {
		v, err := strconv.Atoi(args[0])
		if err != nil || v < 0 {
			return usage("shift", args[0]+": numeric argument required", 1)
		}
		n = v
	}
	if n > len(sh.positional) {
		return CmdResult{ExitCode: 1}
	}
	sh.positional = sh.positional[n:]
	return CmdResult{}
}

// setOption applies one set/sh option letter or -o name.
func (sh *interp) setOption(name string, on bool) bool {
	switch name {
	case "e", "errexit":
		sh.errexit = on
	case "u", "nounset":
		sh.nounset = on
	case "x", "xtrace":
		sh.xtrace = on
	case "pipefail":
		sh.pipefail = on
	case "v", "verbose", "f", "noglob", "h", "B", "C", "noclobber", "H", "m", "monitor", "posix", "a", "allexport":
	default:
		return false
	}
	return true
}

func biSet(sh *interp, args []string, _ *ioIn) CmdResult {
	if len(args) == 0 {
		var b strings.Builder
		for _, k := range sortedKeys(ShellEnv.All()) {
			fmt.Fprintf(&b, "%s=%s\n", k, ShellEnv.Get(k))
		}
		return CmdResult{b.String(), "", 0}
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			sh.positional = append([]string(nil), args[i+1:]...)
			return CmdResult{}
		}
		if len(a) < 2 || (a[0] != '-' && a[0] != '+') {
			sh.positional = append([]string(nil), args[i:]...)
			return CmdResult{}
		}
		on := a[0] == '-'
		for _, ch := range a[1:] {
			if ch == 'o' {
				if i+1 >= len(args) {
					return CmdResult{}
				}
				i++
				if !sh.setOption(args[i], on) {
					return usage("set", args[i]+": invalid option name", 2)
				}
				continue
			}
			if !sh.setOption(string(ch), on) {
				return usage("set", "-"+string(ch)+": invalid option", 2)
			}
		}
	}
	return CmdResult{}
}

func biLocal(sh *interp, args []string, _ *ioIn) CmdResult {
	if sh.funcDepth == 0 {
		return usage("local", "can only be used in a function", 1)
	}
	frame := sh.locals[len(sh.locals)-1]
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		name, val, hasVal := strings.Cut(a, "=")
		if _, recorded := frame[name]; !recorded {
			if old, ok := ShellEnv.Vars[name]; ok {
				frame[name] = &old
			} else {
				frame[name] = nil
			}
		}
		if hasVal {
			sh.setVar(name, val)
		} else if _, ok := ShellEnv.Vars[name]; !ok {
			sh.setVar(name, "")
		}
	}
	return CmdResult{}
}

func biDeclare(sh *interp, args []string, in *ioIn) CmdResult {
	for _, a := range args {
		if strings.HasPrefix(a, "-") && strings.ContainsAny(a, "aA") {
			return CmdResult{"", "declare: arrays are not supported by the in-browser shell\n", ExitCommandNotFound}
		}
	}
	if sh.funcDepth > 0 {
		return biLocal(sh, args, in)
	}
	for _, a := range args {
		if strings.HasPrefix(a, "-") || strings.HasPrefix(a, "+") {
			continue
		}
		if name, val, ok := strings.Cut(a, "="); ok {
			sh.setVar(name, val)
		}
	}
	return CmdResult{}
}

func biUnset(sh *interp, args []string, _ *ioIn) CmdResult {
	funcs := false
	for _, a := range args {
		switch a {
		case "-f":
			funcs = true
		case "-v":
			funcs = false
		default:
			if funcs {
				delete(functions, a)
			} else {
				sh.unsetVar(a)
			}
		}
	}
	return CmdResult{}
}

func biEval(sh *interp, args []string, in *ioIn) CmdResult {
	list, err := parseScript(strings.Join(args, " "))
	if err != nil {
		return parseFailure(err)
	}
	return sh.execList(list, in)
}

func biSource(sh *interp, args []string, in *ioIn) CmdResult {
	if len(args) == 0 {
		return usage("source", "filename argument required", 2)
	}
	data, err := os.ReadFile(ResolvePath(args[0])) //nolint:gosec // G703: shell commands act on the paths the user names
	if err != nil {
		return usage("source", args[0]+": "+describeErr(err), 1)
	}
	list, perr := parseScript(string(data))
	if perr != nil {
		return parseFailure(perr)
	}
	saved := sh.positional
	if len(args) > 1 {
		sh.positional = args[1:]
	}
	r := sh.execList(list, in)
	sh.positional = saved
	if sh.flow == flowReturn {
		sh.flow = flowNone
		r.ExitCode = sh.flowCode
	}
	return r
}

func biRead(sh *interp, args []string, in *ioIn) CmdResult {
	raw := false
	delim := byte('\n')
	var names []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "-r":
			raw = true
		case "-s", "-e":
		case "-p", "-t", "-u", "-n", "-N":
			i++
		case "-d":
			if i+1 < len(args) {
				i++
				if args[i] == "" {
					delim = 0
				} else {
					delim = args[i][0]
				}
			}
		case "-a":
			return CmdResult{"", "read: arrays are not supported by the in-browser shell\n", ExitCommandNotFound}
		default:
			if strings.HasPrefix(a, "-") && len(a) > 1 {
				raw = raw || strings.Contains(a, "r")
				continue
			}
			names = append(names, a)
		}
	}
	line, ok := in.readLine(delim)
	if !raw {
		for ok && strings.HasSuffix(line, "\\") {
			next, more := in.readLine(delim)
			line = strings.TrimSuffix(line, "\\") + next
			ok = more
		}
		line = unescapeBackslashes(line)
	}
	if len(names) == 0 {
		sh.setVar("REPLY", line)
	} else {
		ifs := sh.ifs()
		isIFS := func(r rune) bool { return strings.ContainsRune(ifs, r) }
		rest := strings.TrimLeftFunc(line, isIFS)
		for i, name := range names {
			if i == len(names)-1 {
				sh.setVar(name, strings.TrimRightFunc(rest, isIFS))
				break
			}
			end := strings.IndexFunc(rest, isIFS)
			if end < 0 {
				sh.setVar(name, rest)
				rest = ""
				continue
			}
			sh.setVar(name, rest[:end])
			rest = strings.TrimLeftFunc(rest[end:], isIFS)
		}
	}
	if !ok {
		return CmdResult{ExitCode: 1}
	}
	return CmdResult{}
}

func unescapeBackslashes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
