package wasmshell

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ResolvePath expands ~ and makes relative paths absolute against cwd.
func ResolvePath(p string) string {
	if p == "~" {
		return ShellEnv.Get("HOME")
	}
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(ShellEnv.Get("HOME"), p[2:])
	}

	if !filepath.IsAbs(p) {
		cwd, err := os.Getwd()
		if err == nil {
			p = filepath.Join(cwd, p)
		}
	}

	return filepath.Clean(p)
}

// ExpandGlobs expands glob patterns in arguments.
func ExpandGlobs(args []string) []string {
	var result []string
	for _, arg := range args {
		// Expand variables first
		expanded := os.ExpandEnv(arg)

		// Handle tilde
		if strings.HasPrefix(expanded, "~") {
			expanded = ShellEnv.Get("HOME") + expanded[1:]
		}

		if strings.ContainsAny(expanded, "*?[") {
			matches, err := filepath.Glob(ResolvePath(expanded))
			if err != nil || len(matches) == 0 {
				result = append(result, arg)
			} else {
				result = append(result, matches...)
			}
		} else {
			result = append(result, arg)
		}
	}
	return result
}

// GlobCompletion returns files matching a glob prefix for tab completion.
func GlobCompletion(prefix string) []string {
	dir := filepath.Dir(prefix)
	base := filepath.Base(prefix)

	if dir == "" {
		dir = "."
	}
	dir = ResolvePath(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var matches []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), base) {
			fullPath := filepath.Join(dir, e.Name())
			if e.IsDir() {
				fullPath += "/"
			}
			// Make relative to cwd
			cwd, _ := os.Getwd()
			rel, _ := filepath.Rel(cwd, fullPath)
			matches = append(matches, rel)
		}
	}
	return matches
}

// pipeStdin reads from a reader and returns string.
func pipeStdin(r io.Reader) string {
	data, _ := io.ReadAll(r)
	return string(data)
}

// stringReader converts a string to a reader for piping.
func stringReader(s string) io.Reader {
	return strings.NewReader(s)
}

// pipeCommands runs multiple commands connected by pipes.
func pipeCommands(commands [][]string) CmdResult {
	var stdin string
	for i, cmdArgs := range commands {
		if len(cmdArgs) == 0 {
			continue
		}
		name := cmdArgs[0]
		args := cmdArgs[1:]

		// Expand globs in args
		args = ExpandGlobs(args)

		if i > 0 {
			if fn, ok := CmdRegistry[name]; ok {
				result := fn(args, stdin)
				if result.ExitCode != 0 && len(commands) > 1 {
					return result
				}
				stdin = result.Stdout
			} else {
				return CmdResult{stdin, fmt.Sprintf("command not found: %s\n", name), 127}
			}
		} else {
			if fn, ok := CmdRegistry[name]; ok {
				result := fn(args, stdin)
				if result.ExitCode != 0 {
					return result
				}
				stdin = result.Stdout
			} else {
				return CmdResult{"", fmt.Sprintf("command not found: %s\n", name), 127}
			}
		}
	}

	return CmdResult{stdin, "", 0}
}

// RunCommandWithRedirects runs a command with optional redirect support.
func RunCommandWithRedirects(name string, args []string, stdin string, stdoutRedirect *string, stderrRedirect *string, appendStdout bool) CmdResult {
	args = ExpandGlobs(args)

	fn, ok := CmdRegistry[name]
	if !ok {
		return CmdResult{"", fmt.Sprintf("command not found: %s\n", name), 127}
	}

	result := fn(args, stdin)

	// Handle stdout redirect
	if stdoutRedirect != nil && *stdoutRedirect != "" {
		redirectPath := ResolvePath(*stdoutRedirect)
		if appendStdout {
			existing := ""
			if data, err := os.ReadFile(redirectPath); err == nil {
				existing = string(data)
			}
			SyncWriteFile(redirectPath, existing+result.Stdout)
		} else {
			SyncWriteFile(redirectPath, result.Stdout)
		}
		result.Stdout = ""
	}

	// Handle stderr redirect
	if stderrRedirect != nil && *stderrRedirect != "" {
		redirectPath := ResolvePath(*stderrRedirect)
		SyncWriteFile(redirectPath, result.Stderr)
		result.Stderr = ""
	}

	return result
}
