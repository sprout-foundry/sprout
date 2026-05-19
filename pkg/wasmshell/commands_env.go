package wasmshell

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
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
	"cut": true, "tee": true, "symbols": true,
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
	CmdRegistry["symbols"] = cmdSymbols
}

func cmdHelp(args []string, stdin string) CmdResult {
	var out strings.Builder
	out.WriteString("sprout-wasm shell commands:\n\n")
	out.WriteString("  ls [path]          List directory contents\n")
	out.WriteString("  cd <path>           Change directory\n")
	out.WriteString("  pwd                 Print working directory\n")
	out.WriteString("  cat <file>          Display file contents\n")
	out.WriteString("  mkdir [-p] <path>   Create directory\n")
	out.WriteString("  rm [-rf] <path>     Remove file or directory\n")
	out.WriteString("  rmdir <path>        Remove empty directory\n")
	out.WriteString("  cp [-r] <src> <dst> Copy file/directory\n")
	out.WriteString("  mv <src> <dst>      Move/rename file\n")
	out.WriteString("  touch <file>        Create empty file / update mtime\n")
	out.WriteString("  echo <text>         Print text\n")
	out.WriteString("  head [-n N] <file>  Show first N lines\n")
	out.WriteString("  tail [-n N] <file>  Show last N lines\n")
	out.WriteString("  wc [-lwm] <file>    Count lines/words/chars\n")
	out.WriteString("  grep [-iv] <pat>    Search with regex\n")
	out.WriteString("  sort [-nr]          Sort lines\n")
	out.WriteString("  find <dir> [-name]  Find files\n")
	out.WriteString("  tree [-a] [dir]     Directory tree\n")
	out.WriteString("  clear               Clear the terminal\n")
	out.WriteString("  date                Show current date/time\n")
	out.WriteString("  whoami              Show current user\n")
	out.WriteString("  env                 List environment variables\n")
	out.WriteString("  export K=V          Set environment variable\n")
	out.WriteString("  which <cmd>         Show command location\n")
	out.WriteString("  type <cmd>          Show command type\n")
	out.WriteString("  history             Show command history\n")
	out.WriteString("  tr <set1> <set2>    Translate characters\n")
	out.WriteString("  uniq                Remove duplicate lines\n")
	out.WriteString("  cut -d<delim> -f<n> Cut fields from lines\n")
	out.WriteString("  tee <file>          Write to stdout and file\n")
	out.WriteString("  basename <path>     Print directory name from path\n")
	out.WriteString("  dirname <path>      Print directory name from path\n")
	out.WriteString("  realpath <path>     Print resolved path\n")
	out.WriteString("  symbols <file>      Show code symbols from a source file\n")
	out.WriteString("  symbols -j <file>   Show symbols as JSON\n")
	out.WriteString("  symbols -d <file>   Deep symbol extraction with scope info\n")
	out.WriteString("\nShell features:\n")
	out.WriteString("  |   Pipe commands\n")
	out.WriteString("  >   Redirect stdout to file\n")
	out.WriteString("  >>  Append stdout to file\n")
	out.WriteString("  <   Redirect file to stdin\n")
	out.WriteString("  *   Glob expansion\n")
	out.WriteString("  $VAR  Environment variable expansion\n")
	out.WriteString("  ~    Home directory\n")
	return CmdResult{out.String(), "", 0}
}

func cmdClear(args []string, stdin string) CmdResult {
	return CmdResult{"\x1b[H\x1b[2J", "", 0}
}

func cmdDate(args []string, stdin string) CmdResult {
	format := time.RFC1123
	if len(args) > 0 {
		switch args[0] {
		case "+%s":
			return CmdResult{fmt.Sprintf("%d\n", time.Now().Unix()), "", 0}
		case "+%Y-%m-%d":
			format = "2006-01-02"
		case "+%H:%M:%S":
			format = "15:04:05"
		case "+%Y-%m-%d %H:%M:%S":
			format = "2006-01-02 15:04:05"
		case "+%Y%m%d%H%M%S":
			format = "20060102150405"
		default:
			if strings.HasPrefix(args[0], "+") {
				goFmt := strings.ReplaceAll(args[0][1:], "%Y", "2006")
				goFmt = strings.ReplaceAll(goFmt, "%m", "01")
				goFmt = strings.ReplaceAll(goFmt, "%d", "02")
				goFmt = strings.ReplaceAll(goFmt, "%H", "15")
				goFmt = strings.ReplaceAll(goFmt, "%M", "04")
				goFmt = strings.ReplaceAll(goFmt, "%S", "05")
				goFmt = strings.ReplaceAll(goFmt, "%a", "Mon")
				goFmt = strings.ReplaceAll(goFmt, "%A", "Monday")
				goFmt = strings.ReplaceAll(goFmt, "%b", "Jan")
				goFmt = strings.ReplaceAll(goFmt, "%B", "January")
				goFmt = strings.ReplaceAll(goFmt, "%Z", "MST")
				goFmt = strings.ReplaceAll(goFmt, "%%", "%")
				format = goFmt
			}
		}
	}
	return CmdResult{time.Now().Format(format) + "\n", "", 0}
}

func cmdWhoami(args []string, stdin string) CmdResult {
	return CmdResult{ShellEnv.Get("USER") + "\n", "", 0}
}

func cmdEnvCmd(args []string, stdin string) CmdResult {
	var out strings.Builder
	for _, k := range sortedKeys(ShellEnv.All()) {
		fmt.Fprintf(&out, "%s=%s\n", k, ShellEnv.Get(k))
	}
	return CmdResult{out.String(), "", 0}
}

// cmdEnv is exported as the "env" command.
func cmdEnv(args []string, stdin string) CmdResult {
	return cmdEnvCmd(args, stdin)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func cmdExport(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return cmdEnvCmd(args, stdin)
	}

	for _, arg := range args {
		eqIdx := strings.Index(arg, "=")
		if eqIdx < 0 {
			if val, ok := ShellEnv.Vars[arg]; ok {
				os.Setenv(arg, val)
			}
			continue
		}
		key := arg[:eqIdx]
		value := arg[eqIdx+1:]
		ShellEnv.Set(key, value)
	}

	return CmdResult{"", "", 0}
}

func cmdWhich(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "which: missing argument\n", 1}
	}

	name := args[0]
	if isBuiltin(name) {
		return CmdResult{fmt.Sprintf("%s: sprout-wasm built-in command\n", name), "", 0}
	}

	return CmdResult{"", fmt.Sprintf("which: %s: not found\n", name), 1}
}

func isBuiltin(name string) bool {
	return BuiltinNames[name]
}

func cmdType(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "type: missing argument\n", 1}
	}

	name := args[0]
	if _, ok := CmdRegistry[name]; ok {
		return CmdResult{fmt.Sprintf("%s is a shell built-in\n", name), "", 0}
	}

	return CmdResult{fmt.Sprintf("%s: not found\n", name), "", 1}
}

func cmdHistory(args []string, stdin string) CmdResult {
	var out strings.Builder
	for i, entry := range commandHistory {
		fmt.Fprintf(&out, "%5d  %s\n", i+1, entry)
	}
	return CmdResult{out.String(), "", 0}
}

func cmdPrintln(args []string, stdin string) CmdResult {
	return CmdResult{strings.Join(args, " ") + "\n", "", 0}
}

func cmdBasename(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "basename: missing operand\n", 1}
	}
	return CmdResult{filepath.Base(args[0]) + "\n", "", 0}
}

func cmdDirname(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "dirname: missing operand\n", 1}
	}
	return CmdResult{filepath.Dir(args[0]) + "\n", "", 0}
}

func cmdRealpath(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "realpath: missing operand\n", 1}
	}
	abs, err := filepath.Abs(ResolvePath(args[0]))
	if err != nil {
		return CmdResult{"", fmt.Sprintf("realpath: %s\n", err.Error()), 1}
	}
	return CmdResult{abs + "\n", "", 0}
}
