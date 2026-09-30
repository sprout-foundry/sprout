package wasmshell

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// biSh runs sh/bash: -c STRING, a script file, or a script on stdin, in a
// subshell of this one.
func biSh(sh *interp, args []string, in *ioIn) CmdResult {
	child := &interp{name: "sh", lastExit: sh.lastExit}
	cmdMode := false
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			i++
			break
		}
		if !strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "+") || a == "-" {
			break
		}
		if strings.HasPrefix(a, "--") {
			continue
		}
		on := a[0] == '-'
		for _, ch := range a[1:] {
			switch ch {
			case 'c':
				cmdMode = true
			case 'o':
				if i+1 < len(args) {
					i++
					child.setOption(args[i], on)
				}
			case 'l', 'i', 's':
			default:
				child.setOption(string(ch), on)
			}
		}
	}
	rest := args[i:]
	var src string
	switch {
	case cmdMode:
		if len(rest) == 0 {
			return usage("sh", "-c: option requires an argument", 2)
		}
		src = rest[0]
		if len(rest) > 1 {
			child.name = rest[1]
			child.positional = rest[2:]
		}
		in = &ioIn{data: in.rest()}
	case len(rest) > 0 && rest[0] != "-":
		data, err := os.ReadFile(ResolvePath(rest[0])) //nolint:gosec // G703: shell commands act on the paths the user names
		if err != nil {
			return usage("sh", rest[0]+": "+describeErr(err), 127)
		}
		src = string(data)
		child.name, child.positional = rest[0], rest[1:]
		in = &ioIn{data: in.rest()}
	default:
		src = in.rest()
		in = &ioIn{}
	}
	return runIsolated(func() CmdResult { return child.runSource(src, in) })
}

var shellInterpreters = words("sh", "bash", "dash", "zsh", "ksh")

// runScriptFile runs ./script.sh: shell scripts run here; anything with
// another interpreter needs a real environment.
func (sh *interp) runScriptFile(path string, args []string, in *ioIn) CmdResult {
	data, err := os.ReadFile(ResolvePath(path)) //nolint:gosec // G703: shell commands act on the paths the user names
	if err != nil {
		if os.IsNotExist(err) {
			return CmdResult{Stdout: "", Stderr: fmt.Sprintf("command not found: %s\n", path), ExitCode: ExitCommandNotFound}
		}
		return usage("sh", path+": "+describeErr(err), 126)
	}
	src := string(data)
	if strings.HasPrefix(src, "#!") {
		line, _, _ := strings.Cut(src[2:], "\n")
		fields := strings.Fields(line)
		interpName := ""
		if len(fields) > 0 {
			interpName = filepath.Base(fields[0])
			if interpName == "env" && len(fields) > 1 {
				interpName = fields[1]
			}
		}
		if !shellInterpreters[interpName] {
			return CmdResult{Stdout: "", Stderr: fmt.Sprintf("%s: %s is not available in the in-browser shell\n", path, interpName), ExitCode: ExitCommandNotFound}
		}
	}
	return biSh(sh, append([]string{path}, args...), in)
}

func (sh *interp) describeCommand(name string) (kind string, ok bool) {
	switch {
	case shellKeywords[name]:
		return "keyword", true
	case functions[name] != nil:
		return "function", true
	case shellBuiltins[name] != nil || CmdRegistry[name] != nil:
		return "builtin", true
	}
	return "", false
}

func biCommand(sh *interp, args []string, in *ioIn) CmdResult {
	if len(args) == 0 {
		return CmdResult{}
	}
	if args[0] == "-v" || args[0] == "-V" {
		var out strings.Builder
		code := 0
		for _, name := range args[1:] {
			kind, ok := sh.describeCommand(name)
			if !ok {
				code = 1
				continue
			}
			if args[0] == "-v" {
				out.WriteString(name + "\n")
			} else {
				fmt.Fprintf(&out, "%s is a shell %s\n", name, kind)
			}
		}
		return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: code}
	}
	if args[0] == "-p" {
		args = args[1:]
	}
	if len(args) == 0 {
		return CmdResult{}
	}
	saved, had := functions[args[0]]
	delete(functions, args[0])
	r := sh.run(args, in)
	if had {
		functions[args[0]] = saved
	}
	return r
}

func biType(sh *interp, args []string, _ *ioIn) CmdResult {
	short := false
	var out, errOut strings.Builder
	code := 0
	for _, name := range args {
		if name == "-t" {
			short = true
			continue
		}
		if strings.HasPrefix(name, "-") {
			continue
		}
		kind, ok := sh.describeCommand(name)
		if !ok {
			fmt.Fprintf(&errOut, "type: %s: not found\n", name)
			code = 1
			continue
		}
		switch {
		case short:
			out.WriteString(kind + "\n")
		case kind == "builtin":
			fmt.Fprintf(&out, "%s is a shell built-in\n", name)
		default:
			fmt.Fprintf(&out, "%s is a shell %s\n", name, kind)
		}
	}
	return CmdResult{Stdout: out.String(), Stderr: errOut.String(), ExitCode: code}
}

func biExec(sh *interp, args []string, in *ioIn) CmdResult {
	if len(args) == 0 {
		return CmdResult{}
	}
	r := sh.run(args, in)
	sh.flow, sh.flowCode = flowExit, r.ExitCode
	return r
}

func biLet(sh *interp, args []string, _ *ioIn) CmdResult {
	if len(args) == 0 {
		return usage("let", "expression expected", 1)
	}
	var v int64
	for _, a := range args {
		n, err := sh.arith(a)
		if err != nil {
			return errResult(err)
		}
		v = n
	}
	return CmdResult{ExitCode: int(boolInt(v == 0))}
}

func biEnv(sh *interp, args []string, in *ioIn) CmdResult {
	i := 0
	var unset []string
	for ; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-i" || a == "-" || a == "--ignore-environment":
		case a == "-u" && i+1 < len(args):
			i++
			unset = append(unset, args[i])
		case strings.HasPrefix(a, "-"):
		default:
			goto done
		}
	}
done:
	var assigns [][2]string
	for ; i < len(args) && strings.Contains(args[i], "=") && assignRe.MatchString(args[i]); i++ {
		name, val, _ := strings.Cut(args[i], "=")
		assigns = append(assigns, [2]string{name, val})
	}
	return runIsolated(func() CmdResult {
		for _, name := range unset {
			sh.unsetVar(name)
		}
		for _, kv := range assigns {
			sh.setVar(kv[0], kv[1])
		}
		if i >= len(args) {
			return cmdEnvCmd(nil, "")
		}
		return sh.run(args[i:], in)
	})
}

func biTime(sh *interp, args []string, in *ioIn) CmdResult {
	start := time.Now()
	var r CmdResult
	if len(args) > 0 {
		r = sh.run(args, in)
	}
	d := time.Since(start)
	r.writeErr(fmt.Sprintf("\nreal\t%dm%.3fs\nuser\t0m0.000s\nsys\t0m0.000s\n", int(d.Minutes()), d.Seconds()-float64(int(d.Minutes())*60)))
	return r
}

// biWrapper runs the wrapped command of timeout/nice/nohup/stdbuf; the
// in-browser shell has no process limits to apply.
func biWrapper(sh *interp, args []string, in *ioIn) CmdResult {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		a := args[i]
		i++
		if (a == "-s" || a == "-k" || a == "-n" || a == "--signal" || a == "--kill-after") && i < len(args) {
			i++
		}
	}
	if len(args) > i && isDuration(args[i]) {
		i++
	}
	if i >= len(args) {
		return CmdResult{}
	}
	return sh.run(args[i:], in)
}

func isDuration(s string) bool {
	s = strings.TrimRight(s, "smhd")
	_, err := strconv.ParseFloat(s, 64)
	return err == nil && s != ""
}

// commandNames lists every command name the shell answers.
func commandNames() []string {
	var names []string
	for n := range CmdRegistry {
		names = append(names, n)
	}
	for n := range shellBuiltins {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
