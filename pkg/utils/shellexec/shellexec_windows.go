//go:build windows

package shellexec

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Path returns the POSIX shell used to run commands, or "" when none is
// installed and commands fall back to cmd.exe.
//
// $SHELL is only honored when it names a real Windows executable: Git Bash
// exports SHELL=/usr/bin/bash, an MSYS path CreateProcess cannot resolve.
// %SystemRoot%\System32\bash.exe is the WSL launcher, which runs commands
// inside a Linux VM with a different filesystem, so it is never chosen.
func Path() string {
	if s := os.Getenv("SHELL"); s != "" {
		if isFile(s) && !isWSLLauncher(s) {
			return s
		}
	}
	for _, c := range gitBashCandidates() {
		if isFile(c) {
			return c
		}
	}
	for _, name := range []string{"bash", "sh"} {
		if p, err := exec.LookPath(name); err == nil && !isWSLLauncher(p) {
			return p
		}
	}
	return ""
}

// gitBashCandidates lists where Git for Windows keeps bash.exe. bin\bash.exe
// is preferred over usr\bin\bash.exe: the former is a launcher that puts
// the MSYS and mingw tool directories on PATH before starting bash.
func gitBashCandidates() []string {
	var roots []string
	if git, err := exec.LookPath("git"); err == nil {
		dir := filepath.Dir(git) // <root>\cmd, <root>\bin or <root>\mingw64\bin
		roots = append(roots, filepath.Dir(dir), filepath.Dir(filepath.Dir(dir)))
	}
	for _, env := range []string{"ProgramW6432", "ProgramFiles", "ProgramFiles(x86)"} {
		if v := os.Getenv(env); v != "" {
			roots = append(roots, filepath.Join(v, "Git"))
		}
	}
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		roots = append(roots, filepath.Join(v, "Programs", "Git"))
	}
	candidates := make([]string, 0, 2*len(roots))
	for _, r := range roots {
		candidates = append(candidates,
			filepath.Join(r, "bin", "bash.exe"),
			filepath.Join(r, "usr", "bin", "bash.exe"))
	}
	return candidates
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func isWSLLauncher(p string) bool {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(abs), strings.ToLower(root)+`\`)
}

func comspec() string {
	if c := os.Getenv("ComSpec"); c != "" {
		return c
	}
	return "cmd.exe"
}

func argv(command string) (string, []string) {
	if sh := Path(); sh != "" {
		return sh, []string{"-c", command}
	}
	return comspec(), nil
}

func prepare(cmd *exec.Cmd, command string) {
	if cmd.Cancel != nil {
		cmd.Cancel = func() error { return killTree(cmd.Process) }
	}
	if len(cmd.Args) == 1 {
		passVerbatimToCmd(cmd, command)
	}
}

// killTree ends the shell and everything it started. Killing only the
// direct child is not enough: Git for Windows' bin\bash.exe is a launcher
// whose real bash (and that bash's children) would keep running and
// holding the output pipes, so a timed-out command never returned.
func killTree(p *os.Process) error {
	taskkill := filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe")
	if err := exec.Command(taskkill, "/T", "/F", "/PID", strconv.Itoa(p.Pid)).Run(); err == nil {
		return nil
	}
	return p.Kill()
}

// passVerbatimToCmd hands cmd.exe the command line verbatim. Go's argv
// quoting follows the MSVC runtime rules, which cmd.exe does not parse, so
// a quoted argument would arrive with stray backslashes. /s strips exactly
// the outer quote pair, leaving the user's command untouched.
func passVerbatimToCmd(cmd *exec.Cmd, command string) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CmdLine = `"` + cmd.Path + `" /d /s /c "` + command + `"`
}
