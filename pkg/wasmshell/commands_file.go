package wasmshell

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// cmdLs implements the ls builtin. Flag parsing and the listDir/listOne
// machinery live in commands_file_ls.go (lsContext + parseLsFlags).
func cmdLs(args []string, stdin string) CmdResult {
	ctx, paths := parseLsFlags(args)
	if len(paths) == 0 {
		paths = []string{"."}
	}
	ctx.paths = paths

	for _, p := range paths {
		ctx.listOne(p)
	}

	exit := 0
	if ctx.errOut.Len() > 0 {
		exit = 1
	}
	return CmdResult{ctx.out.String(), ctx.errOut.String(), exit}
}

func humanizeSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1fG", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1fM", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1fK", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

func cmdCd(args []string, stdin string) CmdResult {
	var target string
	if len(args) > 0 {
		target = args[0]
	} else {
		target = ShellEnv.Get("HOME")
	}

	if target == "~" {
		target = ShellEnv.Get("HOME")
	} else if strings.HasPrefix(target, "~/") {
		target = filepath.Join(ShellEnv.Get("HOME"), target[2:])
	}

	target = ResolvePath(target)

	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return CmdResult{"", fmt.Sprintf("cd: %s: No such directory\n", target), 1}
	}

	if err := os.Chdir(target); err != nil {
		return CmdResult{"", fmt.Sprintf("cd: %s: %s\n", target, err.Error()), 1}
	}

	abs, _ := filepath.Abs(target)
	ShellEnv.Set("PWD", abs)
	return CmdResult{"", "", 0}
}

func cmdPwd(args []string, stdin string) CmdResult {
	cwd, err := os.Getwd()
	if err != nil {
		return CmdResult{"", "pwd: error getting working directory\n", 1}
	}
	return CmdResult{cwd + "\n", "", 0}
}

func cmdCat(args []string, stdin string) CmdResult {
	numberLines := false
	targets := []string{}

	for _, a := range args {
		if a == "-n" || a == "--number" {
			numberLines = true
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			continue
		}
		targets = append(targets, a)
	}

	if len(targets) == 0 {
		if numberLines {
			return CmdResult{numberLinesText(stdin), "", 0}
		}
		return CmdResult{stdin, "", 0}
	}

	var out strings.Builder
	for _, arg := range targets {
		path := ResolvePath(arg)
		data, err := os.ReadFile(path)
		if err != nil {
			return CmdResult{"", fmt.Sprintf("cat: %s: %s\n", arg, err.Error()), 1}
		}
		if numberLines {
			out.WriteString(numberLinesText(string(data)))
			continue
		}
		out.Write(data)
		if !bytes.HasSuffix(data, []byte("\n")) {
			out.WriteByte('\n')
		}
	}
	return CmdResult{out.String(), "", 0}
}

// numberLinesText prefixes each line with its 6-wide line number the way
// cat -n does.
func numberLinesText(s string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	var b strings.Builder
	for i, l := range lines {
		fmt.Fprintf(&b, "%6d  %s\n", i+1, l)
	}
	return b.String()
}
