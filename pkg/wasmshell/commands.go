package wasmshell

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Env holds the shell environment variables.
type Env struct {
	Vars map[string]string
}

// ShellEnv holds the global shell environment.
var ShellEnv *Env

// NewEnv creates a new Env with default shell variables.
func NewEnv() *Env {
	home := "/home/user"
	e := &Env{Vars: map[string]string{
		"HOME":     home,
		"PWD":      home,
		"PATH":     "/usr/local/bin:/usr/bin:/bin",
		"TERM":     "xterm-256color",
		"LANG":     "en_US.UTF-8",
		"SHELL":    "/bin/sprout-sh",
		"USER":     "user",
		"HOSTNAME": "sprout-wasm",
		"EDITOR":   "sprout",
	}}
	return e
}

// SetShellEnv replaces the global shell environment (useful for testing).
func SetShellEnv(e *Env) {
	ShellEnv = e
	// Sync all env vars to os so that os.ExpandEnv works.
	for k, v := range e.Vars {
		os.Setenv(k, v)
	}
}

// Get returns the value of an environment variable, falling back to the OS environment.
func (e *Env) Get(key string) string {
	if v, ok := e.Vars[key]; ok {
		return v
	}
	return os.Getenv(key)
}

// Set sets an environment variable in both the Env map and the OS environment.
func (e *Env) Set(key, value string) {
	e.Vars[key] = value
	os.Setenv(key, value)
}

// All returns a copy of all environment variables.
func (e *Env) All() map[string]string {
	result := make(map[string]string)
	for k, v := range e.Vars {
		result[k] = v
	}
	return result
}

// CmdResult holds the result of a command execution.
type CmdResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

// DirEntry represents a directory listing entry.
type DirEntry struct {
	Name string `json:"name"`
	Type string `json:"type"` // "file" or "dir"
	Size int64  `json:"size"`
	Mode uint32 `json:"mode"`
}

// CommandFunc is the type for all command implementations.
type CommandFunc func(args []string, stdin string) CmdResult

// CmdRegistry maps command names to their implementations.
var CmdRegistry = map[string]CommandFunc{}

// BuiltinNames lists all built-in command names.
var BuiltinNames = map[string]bool{
	"ls": true, "cd": true, "pwd": true, "cat": true, "mkdir": true,
	"rm": true, "rmdir": true, "cp": true, "mv": true, "touch": true,
	"echo": true, "head": true, "tail": true, "wc": true, "grep": true,
	"sort": true, "find": true, "tree": true, "clear": true, "help": true,
	"date": true, "whoami": true, "env": true, "export": true, "which": true,
	"type": true, "history": true, "println": true, "basename": true,
	"dirname": true, "realpath": true, "tr": true, "uniq": true,
	"cut": true, "tee": true,
}

func init() {
	CmdRegistry["ls"] = cmdLs
	CmdRegistry["cd"] = cmdCd
	CmdRegistry["pwd"] = cmdPwd
	CmdRegistry["cat"] = cmdCat
	CmdRegistry["mkdir"] = cmdMkdir
	CmdRegistry["rm"] = cmdRm
	CmdRegistry["rmdir"] = cmdRmdir
	CmdRegistry["cp"] = cmdCp
	CmdRegistry["mv"] = cmdMv
	CmdRegistry["touch"] = cmdTouch
	CmdRegistry["echo"] = cmdEcho
	CmdRegistry["head"] = cmdHead
	CmdRegistry["tail"] = cmdTail
	CmdRegistry["wc"] = cmdWc
	CmdRegistry["grep"] = cmdGrep
	CmdRegistry["sort"] = cmdSort
	CmdRegistry["find"] = cmdFind
	CmdRegistry["tree"] = cmdTree
	CmdRegistry["clear"] = cmdClear
	CmdRegistry["help"] = cmdHelp
	CmdRegistry["date"] = cmdDate
	CmdRegistry["whoami"] = cmdWhoami
	CmdRegistry["env"] = cmdEnv
	CmdRegistry["export"] = cmdExport
	CmdRegistry["which"] = cmdWhich
	CmdRegistry["type"] = cmdType
	CmdRegistry["history"] = cmdHistory
	CmdRegistry["println"] = cmdPrintln
	CmdRegistry["basename"] = cmdBasename
	CmdRegistry["dirname"] = cmdDirname
	CmdRegistry["realpath"] = cmdRealpath
	CmdRegistry["tr"] = cmdTr
	CmdRegistry["uniq"] = cmdUniq
	CmdRegistry["cut"] = cmdCut
	CmdRegistry["tee"] = cmdTee
}

// ─── Utility functions ──────────────────────────────────────────────────

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

	entries, err := ReadDirCompat(dir)
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

// ListDirEntryJSON returns the JSON representation of directory entries.
func ListDirEntryJSON(path string) (string, error) {
	target := ResolvePath(path)
	entries, err := ReadDirCompat(target)
	if err != nil {
		return "", err
	}

	var result []DirEntry
	for _, e := range entries {
		info, err := e.Info()
		var sz int64
		var mode uint32
		if err == nil {
			sz = info.Size()
			mode = uint32(info.Mode())
		}
		typ := "file"
		if e.IsDir() {
			typ = "dir"
		}
		result = append(result, DirEntry{
			Name: e.Name(),
			Type: typ,
			Size: sz,
			Mode: mode,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ReadFileContent reads a file and returns its content as a string.
func ReadFileContent(path string) (string, error) {
	data, err := os.ReadFile(ResolvePath(path))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteFileContent writes content to a file.
func WriteFileContent(path, content string) error {
	return SyncWriteFile(ResolvePath(path), content)
}

// DeleteFilePath deletes a file.
func DeleteFilePath(path string) error {
	return SyncDeleteFile(ResolvePath(path))
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

// Copying bufio into scope since it's used by some utilities.
var _ = bufio.NewReader
var _ = (*bytes.Buffer)(nil)
