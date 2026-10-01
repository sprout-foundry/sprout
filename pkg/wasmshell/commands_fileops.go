package wasmshell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// splitFlags separates short-flag letters from operands; "--" ends flags.
func splitFlags(args []string) (flags string, operands []string) {
	for i, a := range args {
		if a == "--" {
			return flags, append(operands, args[i+1:]...)
		}
		if len(a) > 1 && a[0] == '-' && a[1] != '-' {
			flags += a[1:]
			continue
		}
		if strings.HasPrefix(a, "--") {
			continue
		}
		operands = append(operands, a)
	}
	return flags, operands
}

func cmdCd(args []string, stdin string) CmdResult {
	_, ops := splitFlags(args)
	target := ShellEnv.Get("HOME")
	printDir := false
	if len(ops) > 0 {
		target = ops[0]
	}
	if target == "-" {
		target = ShellEnv.Get("OLDPWD")
		if target == "" {
			return CmdResult{Stdout: "", Stderr: "cd: OLDPWD not set\n", ExitCode: 1}
		}
		printDir = true
	}
	path := ResolvePath(target)
	info, err := os.Stat(path) //nolint:gosec // G703: shell commands act on the paths the user names
	if err != nil {
		return CmdResult{Stdout: "", Stderr: fmt.Sprintf("cd: %s: %s\n", target, describeErr(err)), ExitCode: 1}
	}
	if !info.IsDir() {
		return CmdResult{Stdout: "", Stderr: fmt.Sprintf("cd: %s: Not a directory\n", target), ExitCode: 1}
	}
	old, _ := os.Getwd()
	if err := os.Chdir(path); err != nil {
		return CmdResult{Stdout: "", Stderr: fmt.Sprintf("cd: %s: %s\n", target, describeErr(err)), ExitCode: 1}
	}
	abs, _ := filepath.Abs(path)
	ShellEnv.Set("OLDPWD", old)
	ShellEnv.Set("PWD", abs)
	if printDir {
		return CmdResult{Stdout: abs + "\n", Stderr: "", ExitCode: 0}
	}
	return CmdResult{}
}

func cmdPwd(args []string, stdin string) CmdResult {
	cwd, err := os.Getwd()
	if err != nil {
		return CmdResult{Stdout: "", Stderr: "pwd: error getting working directory\n", ExitCode: 1}
	}
	return CmdResult{Stdout: cwd + "\n", Stderr: "", ExitCode: 0}
}

func cmdCat(args []string, stdin string) CmdResult {
	flags, files := splitFlags(args)
	for _, a := range args {
		switch a {
		case "--number":
			flags += "n"
		case "--number-nonblank":
			flags += "b"
		case "--squeeze-blank":
			flags += "s"
		case "--show-ends":
			flags += "E"
		}
	}
	if len(files) == 0 {
		files = []string{"-"}
	}
	var raw strings.Builder
	for _, f := range files {
		if f == "-" {
			raw.WriteString(stdin)
			continue
		}
		data, err := readFileArg(f)
		if err != nil {
			return CmdResult{Stdout: raw.String(), Stderr: fmt.Sprintf("cat: %s: %s\n", f, describeErrText(err)), ExitCode: 1}
		}
		raw.WriteString(data)
	}
	text := raw.String()
	if !strings.ContainsAny(flags, "nbsEA") || text == "" {
		return CmdResult{Stdout: text, Stderr: "", ExitCode: 0}
	}
	trailing := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	var out strings.Builder
	n := 0
	prevBlank := false
	for k, l := range lines {
		blank := l == ""
		if strings.Contains(flags, "s") && blank && prevBlank {
			continue
		}
		prevBlank = blank
		switch {
		case strings.Contains(flags, "b"):
			if !blank {
				n++
				fmt.Fprintf(&out, "%6d\t", n)
			}
		case strings.Contains(flags, "n"):
			n++
			fmt.Fprintf(&out, "%6d  ", n)
		}
		out.WriteString(l)
		if strings.ContainsAny(flags, "EA") {
			out.WriteString("$")
		}
		if k < len(lines)-1 || trailing {
			out.WriteString("\n")
		}
	}
	return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: 0}
}

func describeErrText(err error) string {
	if errors.Is(err, errIsDirectory) {
		return "Is a directory"
	}
	return describeErr(err)
}

func cmdMkdir(args []string, stdin string) CmdResult {
	flags, dirs := splitFlags(args)
	parents := strings.Contains(flags, "p") || containsArg(args, "--parents")
	if len(dirs) == 0 {
		return CmdResult{Stdout: "", Stderr: "mkdir: missing operand\n", ExitCode: 1}
	}
	var errs strings.Builder
	for _, d := range dirs {
		path := ResolvePath(d)
		var err error
		if parents {
			err = os.MkdirAll(path, 0o755) //nolint:gosec // G703: shell commands act on the paths the user names
		} else {
			err = os.Mkdir(path, 0o755) //nolint:gosec // G703: shell commands act on the paths the user names
		}
		if err != nil {
			fmt.Fprintf(&errs, "mkdir: cannot create directory '%s': %s\n", d, describeErr(err))
		}
	}
	if errs.Len() > 0 {
		return CmdResult{Stdout: "", Stderr: errs.String(), ExitCode: 1}
	}
	return CmdResult{}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func cmdRm(args []string, stdin string) CmdResult {
	flags, targets := splitFlags(args)
	recursive := strings.ContainsAny(flags, "rR") || containsArg(args, "--recursive")
	force := strings.Contains(flags, "f") || containsArg(args, "--force")
	emptyDirs := strings.Contains(flags, "d")
	if len(targets) == 0 {
		if force {
			return CmdResult{}
		}
		return CmdResult{Stdout: "", Stderr: "rm: missing operand\n", ExitCode: 1}
	}
	var errs strings.Builder
	for _, t := range targets {
		path := ResolvePath(t)
		if path == "/" {
			fmt.Fprintf(&errs, "rm: it is dangerous to operate recursively on '/'\n")
			continue
		}
		info, err := os.Lstat(path) //nolint:gosec // G703: shell commands act on the paths the user names
		if err != nil {
			if !force {
				fmt.Fprintf(&errs, "rm: cannot remove '%s': %s\n", t, describeErr(err))
			}
			continue
		}
		if info.IsDir() && !recursive {
			entries, _ := ReadDirCompat(path)
			if !emptyDirs || len(entries) > 0 {
				fmt.Fprintf(&errs, "rm: cannot remove '%s': Is a directory\n", t)
				continue
			}
		}
		if err := removePath(path); err != nil {
			fmt.Fprintf(&errs, "rm: cannot remove '%s': %s\n", t, describeErr(err))
		}
	}
	if errs.Len() > 0 {
		return CmdResult{Stdout: "", Stderr: errs.String(), ExitCode: 1}
	}
	return CmdResult{}
}

func cmdRmdir(args []string, stdin string) CmdResult {
	flags, dirs := splitFlags(args)
	if len(dirs) == 0 {
		return CmdResult{Stdout: "", Stderr: "rmdir: missing operand\n", ExitCode: 1}
	}
	for _, d := range dirs {
		path := ResolvePath(d)
		for {
			if err := os.Remove(path); err != nil { //nolint:gosec // G703: shell commands act on the paths the user names
				return CmdResult{Stdout: "", Stderr: fmt.Sprintf("rmdir: failed to remove '%s': %s\n", d, describeErr(err)), ExitCode: 1}
			}
			if !strings.Contains(flags, "p") {
				break
			}
			d = filepath.Dir(d)
			if d == "." || d == "/" {
				break
			}
			path = ResolvePath(d)
		}
	}
	return CmdResult{}
}

// destination resolves where src lands: inside dst when dst is a
// directory, else at dst itself.
func destination(src, dst string, forceDir bool) (string, error) {
	if info, err := os.Stat(dst); err == nil && info.IsDir() { //nolint:gosec // G703: shell commands act on the paths the user names
		return filepath.Join(dst, filepath.Base(src)), nil
	}
	if forceDir {
		return "", fmt.Errorf("target '%s' is not a directory", dst)
	}
	return dst, nil
}

func cmdCp(args []string, stdin string) CmdResult {
	flags, ops := splitFlags(args)
	recursive := strings.ContainsAny(flags, "rRa") || containsArg(args, "--recursive")
	noClobber := strings.Contains(flags, "n")
	verbose := strings.Contains(flags, "v")
	if len(ops) < 2 {
		return CmdResult{Stdout: "", Stderr: "cp: missing destination file operand\n", ExitCode: 1}
	}
	dst := ResolvePath(ops[len(ops)-1])
	srcs := ops[:len(ops)-1]
	var out, errs strings.Builder
	for _, s := range srcs {
		src := ResolvePath(s)
		info, err := os.Stat(src) //nolint:gosec // G703: shell commands act on the paths the user names
		if err != nil {
			fmt.Fprintf(&errs, "cp: cannot stat '%s': %s\n", s, describeErr(err))
			continue
		}
		if info.IsDir() && !recursive {
			fmt.Fprintf(&errs, "cp: -r not specified; omitting directory '%s'\n", s)
			continue
		}
		target, err := destination(src, dst, len(srcs) > 1)
		if err != nil {
			fmt.Fprintf(&errs, "cp: %s\n", err)
			break
		}
		if strings.HasPrefix(target+string(filepath.Separator), src+string(filepath.Separator)) && info.IsDir() {
			fmt.Fprintf(&errs, "cp: cannot copy a directory, '%s', into itself\n", s)
			continue
		}
		if _, err := os.Stat(target); err == nil && noClobber {
			continue
		}
		if err := copyPath(src, target); err != nil {
			fmt.Fprintf(&errs, "cp: %s\n", describeErr(err))
			continue
		}
		if verbose {
			fmt.Fprintf(&out, "'%s' -> '%s'\n", s, target)
		}
	}
	if errs.Len() > 0 {
		return CmdResult{Stdout: out.String(), Stderr: errs.String(), ExitCode: 1}
	}
	return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: 0}
}

func copyPath(src, dst string) error {
	info, err := os.Stat(src) //nolint:gosec // G703: shell commands act on the paths the user names
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst)
	}
	return WalkCompat(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src) //nolint:gosec // G703: shell commands act on the paths the user names
	if err != nil {
		return err
	}
	return SyncWriteFile(dst, string(data))
}

func cmdMv(args []string, stdin string) CmdResult {
	flags, ops := splitFlags(args)
	noClobber := strings.Contains(flags, "n")
	verbose := strings.Contains(flags, "v")
	if len(ops) < 2 {
		return CmdResult{Stdout: "", Stderr: "mv: missing destination file operand\n", ExitCode: 1}
	}
	dst := ResolvePath(ops[len(ops)-1])
	srcs := ops[:len(ops)-1]
	var out, errs strings.Builder
	for _, s := range srcs {
		src := ResolvePath(s)
		if _, err := os.Lstat(src); err != nil { //nolint:gosec // G703: shell commands act on the paths the user names
			fmt.Fprintf(&errs, "mv: cannot stat '%s': %s\n", s, describeErr(err))
			continue
		}
		target, err := destination(src, dst, len(srcs) > 1)
		if err != nil {
			fmt.Fprintf(&errs, "mv: %s\n", err)
			break
		}
		if target == src {
			continue
		}
		if _, err := os.Stat(target); err == nil && noClobber {
			continue
		}
		if err := copyPath(src, target); err != nil {
			fmt.Fprintf(&errs, "mv: %s\n", describeErr(err))
			continue
		}
		if err := removePath(src); err != nil {
			fmt.Fprintf(&errs, "mv: cannot remove '%s': %s\n", s, describeErr(err))
			continue
		}
		if verbose {
			fmt.Fprintf(&out, "renamed '%s' -> '%s'\n", s, target)
		}
	}
	if errs.Len() > 0 {
		return CmdResult{Stdout: out.String(), Stderr: errs.String(), ExitCode: 1}
	}
	return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: 0}
}

func cmdTouch(args []string, stdin string) CmdResult {
	noCreate := false
	var files []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-c" || a == "--no-create":
			noCreate = true
		case a == "-d" || a == "-t" || a == "-r":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			files = append(files, a)
		}
	}
	if len(files) == 0 {
		return CmdResult{Stdout: "", Stderr: "touch: missing file operand\n", ExitCode: 1}
	}
	for _, f := range files {
		path := ResolvePath(f)
		if _, err := os.Stat(path); err == nil { //nolint:gosec // G703: shell commands act on the paths the user names
			now := time.Now()
			_ = os.Chtimes(path, now, now)
			continue
		}
		if noCreate {
			continue
		}
		if _, err := os.Stat(filepath.Dir(path)); err != nil { //nolint:gosec // G703: shell commands act on the paths the user names
			return CmdResult{Stdout: "", Stderr: fmt.Sprintf("touch: cannot touch '%s': No such file or directory\n", f), ExitCode: 1}
		}
		if err := SyncWriteFile(path, ""); err != nil {
			return CmdResult{Stdout: "", Stderr: fmt.Sprintf("touch: cannot touch '%s': %s\n", f, describeErr(err)), ExitCode: 1}
		}
	}
	return CmdResult{}
}
