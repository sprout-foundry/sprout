//go:build js && wasm

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Env holds the shell environment variables.
type Env struct {
	vars map[string]string
}

var shellEnv *Env

func newEnv() *Env {
	home := "/home/user"
	e := &Env{vars: map[string]string{
		"HOME":     home,
		"PWD":      home,
		"PATH":     "/usr/local/bin:/usr/bin:/bin",
		"TERM":     "xterm-256color",
		"LANG":     "en_US.UTF-8",
		"SHELL":    "/bin/ledit-sh",
		"USER":     "user",
		"HOSTNAME": "ledit-wasm",
		"EDITOR":   "ledit",
	}}
	return e
}

func (e *Env) Get(key string) string {
	if v, ok := e.vars[key]; ok {
		return v
	}
	return os.Getenv(key)
}

func (e *Env) Set(key, value string) {
	e.vars[key] = value
	os.Setenv(key, value)
}

func (e *Env) All() map[string]string {
	result := make(map[string]string)
	for k, v := range e.vars {
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

// commandFunc is the type for all command implementations.
type commandFunc func(args []string, stdin string) CmdResult

// cmdRegistry maps command names to their implementations.
var cmdRegistry = map[string]commandFunc{}

// builtinNames lists all built-in command names (avoids init cycle).
var builtinNames = map[string]bool{
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
	cmdRegistry["ls"] = cmdLs
	cmdRegistry["cd"] = cmdCd
	cmdRegistry["pwd"] = cmdPwd
	cmdRegistry["cat"] = cmdCat
	cmdRegistry["mkdir"] = cmdMkdir
	cmdRegistry["rm"] = cmdRm
	cmdRegistry["rmdir"] = cmdRmdir
	cmdRegistry["cp"] = cmdCp
	cmdRegistry["mv"] = cmdMv
	cmdRegistry["touch"] = cmdTouch
	cmdRegistry["echo"] = cmdEcho
	cmdRegistry["head"] = cmdHead
	cmdRegistry["tail"] = cmdTail
	cmdRegistry["wc"] = cmdWc
	cmdRegistry["grep"] = cmdGrep
	cmdRegistry["sort"] = cmdSort
	cmdRegistry["find"] = cmdFind
	cmdRegistry["tree"] = cmdTree
	cmdRegistry["clear"] = cmdClear
	cmdRegistry["help"] = cmdHelp
	cmdRegistry["date"] = cmdDate
	cmdRegistry["whoami"] = cmdWhoami
	cmdRegistry["env"] = cmdEnv
	cmdRegistry["export"] = cmdExport
	cmdRegistry["which"] = cmdWhich
	cmdRegistry["type"] = cmdType
	cmdRegistry["history"] = cmdHistory
	cmdRegistry["println"] = cmdPrintln
	cmdRegistry["basename"] = cmdBasename
	cmdRegistry["dirname"] = cmdDirname
	cmdRegistry["realpath"] = cmdRealpath
	cmdRegistry["tr"] = cmdTr
	cmdRegistry["uniq"] = cmdUniq
	cmdRegistry["cut"] = cmdCut
	cmdRegistry["tee"] = cmdTee
}

// ─── Command Implementations ───────────────────────────────────────────

func cmdLs(args []string, stdin string) CmdResult {
	showAll := false
	showLong := false
	humanSize := false

	// Parse flags
	i := 0
	for i < len(args) {
		switch args[i] {
		case "-a", "--all":
			showAll = true
		case "-l", "--long":
			showLong = true
		case "-h", "--human-readable":
			humanSize = true
		case "-la", "-al":
			showAll = true
			showLong = true
		default:
			break
		}
		i++
	}

	// Find non-flag arguments
	paths := []string{}
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		paths = append(paths, a)
	}

	if len(paths) == 0 {
		paths = []string{"."}
	}

	var out strings.Builder

	for _, p := range paths {
		target := resolvePath(p)
		entries, err := os.ReadDir(target)
		if err != nil {
			if len(paths) == 1 {
				return CmdResult{"", fmt.Sprintf("ls: cannot access '%s': %s\n", p, err.Error()), 1}
			}
			fmt.Fprintf(&out, "ls: cannot access '%s': %s\n", p, err.Error())
			continue
		}

		if len(paths) > 1 {
			fmt.Fprintf(&out, "%s:\n", p)
		}

		// Collect and sort entries
		type entry struct {
			name    string
			isDir   bool
			size    int64
			mode    uint32
			modTime time.Time
		}
		var items []entry

		if showAll {
			items = append(items, entry{name: ".", isDir: true})
			items = append(items, entry{name: "..", isDir: true})
		}

		for _, e := range entries {
			if !showAll && strings.HasPrefix(e.Name(), ".") {
				continue
			}
			info, err := e.Info()
			var sz int64
			var mod time.Time
			var mode uint32
			if err == nil {
				sz = info.Size()
				mod = info.ModTime()
				mode = uint32(info.Mode())
			}
			items = append(items, entry{
				name:    e.Name(),
				isDir:   e.IsDir(),
				size:    sz,
				mode:    mode,
				modTime: mod,
			})
		}

		sort.Slice(items, func(i, j int) bool {
			// Dirs first, then files
			if items[i].isDir != items[j].isDir {
				return items[i].isDir
			}
			return items[i].name < items[j].name
		})

		for _, item := range items {
			if showLong {
				dirChar := "-"
				if item.isDir {
					dirChar = "d"
				}
				size := item.size
				if humanSize {
					out.WriteString(fmt.Sprintf("%srwxr-xr-x 1 user user %8s %s %s\n",
						dirChar, humanizeSize(size), item.modTime.Format("Jan 02 15:04"), item.name))
				} else {
					out.WriteString(fmt.Sprintf("%srwxr-xr-x 1 user user %8d %s %s\n",
						dirChar, size, item.modTime.Format("Jan 02 15:04"), item.name))
				}
			} else {
				out.WriteString(item.name)
				if item.isDir {
					out.WriteString("/")
				}
				out.WriteString("\n")
			}
		}
	}

	return CmdResult{out.String(), "", 0}
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
		target = shellEnv.Get("HOME")
	}

	if target == "~" {
		target = shellEnv.Get("HOME")
	} else if strings.HasPrefix(target, "~/") {
		target = filepath.Join(shellEnv.Get("HOME"), target[2:])
	}

	target = resolvePath(target)

	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return CmdResult{"", fmt.Sprintf("cd: %s: No such directory\n", target), 1}
	}

	if err := os.Chdir(target); err != nil {
		return CmdResult{"", fmt.Sprintf("cd: %s: %s\n", target, err.Error()), 1}
	}

	abs, _ := filepath.Abs(target)
	shellEnv.Set("PWD", abs)
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
	if len(args) == 0 {
		return CmdResult{stdin, "", 0}
	}

	var out strings.Builder
	for _, arg := range args {
		path := resolvePath(arg)
		data, err := os.ReadFile(path)
		if err != nil {
			return CmdResult{"", fmt.Sprintf("cat: %s: %s\n", arg, err.Error()), 1}
		}
		out.Write(data)
		if !bytes.HasSuffix(data, []byte("\n")) {
			out.WriteByte('\n')
		}
	}
	return CmdResult{out.String(), "", 0}
}

func cmdMkdir(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "mkdir: missing operand\n", 1}
	}

	parents := false
	envArgs := []string{}
	for _, a := range args {
		if a == "-p" || a == "--parents" {
			parents = true
		} else {
			envArgs = append(envArgs, a)
		}
	}

	for _, arg := range envArgs {
		path := resolvePath(arg)
		if parents {
			if err := os.MkdirAll(path, 0755); err != nil {
				return CmdResult{"", fmt.Sprintf("mkdir: %s: %s\n", arg, err.Error()), 1}
			}
		} else {
			if err := os.Mkdir(path, 0755); err != nil {
				return CmdResult{"", fmt.Sprintf("mkdir: %s: %s\n", arg, err.Error()), 1}
			}
		}
	}
	return CmdResult{"", "", 0}
}

func cmdRm(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "rm: missing operand\n", 1}
	}

	recursive := false
	force := false
	targets := []string{}

	for _, a := range args {
		switch a {
		case "-r", "-R", "-rf", "-fr", "-rF", "-Fr":
			recursive = true
			force = true
		case "-f", "--force":
			force = true
		default:
			if strings.HasPrefix(a, "-") {
				if strings.Contains(a, "r") || strings.Contains(a, "R") {
					recursive = true
				}
				if strings.Contains(a, "f") {
					force = true
				}
			} else {
				targets = append(targets, a)
			}
		}
	}

	for _, arg := range targets {
		path := resolvePath(arg)
		info, err := os.Stat(path)
		if err != nil {
			if !force {
				return CmdResult{"", fmt.Sprintf("rm: %s: %s\n", arg, err.Error()), 1}
			}
			continue
		}

		if info.IsDir() && !recursive {
			return CmdResult{"", fmt.Sprintf("rm: %s: is a directory (use -r)\n", arg), 1}
		}

		var rmErr error
		if info.IsDir() {
			rmErr = os.RemoveAll(path)
			if rmErr == nil {
				// Sync remaining files after recursive delete
				if dir := filepath.Dir(path); dir != "" && dir != "." {
					RecursiveSync(dir)
				}
			}
		} else {
			rmErr = SyncDeleteFile(path)
		}

		if rmErr != nil && !force {
			return CmdResult{"", fmt.Sprintf("rm: %s: %s\n", arg, rmErr.Error()), 1}
		}
	}
	return CmdResult{"", "", 0}
}

func cmdRmdir(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "rmdir: missing operand\n", 1}
	}
	for _, arg := range args {
		path := resolvePath(arg)
		if err := os.Remove(path); err != nil {
			return CmdResult{"", fmt.Sprintf("rmdir: %s: %s\n", arg, err.Error()), 1}
		}
	}
	return CmdResult{"", "", 0}
}

func cmdCp(args []string, stdin string) CmdResult {
	if len(args) < 2 {
		return CmdResult{"", "cp: missing operand\n", 1}
	}

	recursive := false
	targets := []string{}
	for _, a := range args {
		if a == "-r" || a == "-R" || a == "-a" {
			recursive = true
		} else {
			targets = append(targets, a)
		}
	}

	if len(targets) < 2 {
		return CmdResult{"", "cp: missing destination\n", 1}
	}

	src := resolvePath(targets[0])
	dst := resolvePath(targets[1])

	srcInfo, err := os.Stat(src)
	if err != nil {
		return CmdResult{"", fmt.Sprintf("cp: %s: %s\n", targets[0], err.Error()), 1}
	}

	if srcInfo.IsDir() && !recursive {
		return CmdResult{"", fmt.Sprintf("cp: %s: is a directory (use -r)\n", targets[0]), 1}
	}

	if err := copyPath(src, dst, recursive); err != nil {
		return CmdResult{"", fmt.Sprintf("cp: %s\n", err.Error()), 1}
	}

	return CmdResult{"", "", 0}
}

func copyPath(src, dst string, recursive bool) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if srcInfo.IsDir() {
		return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(src, path)
			destPath := filepath.Join(dst, rel)

			if info.IsDir() {
				return os.MkdirAll(destPath, info.Mode())
			}
			return copyFile(path, destPath)
		})
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return SyncWriteFile(dst, string(data))
}

func cmdMv(args []string, stdin string) CmdResult {
	if len(args) < 2 {
		return CmdResult{"", "mv: missing operand\n", 1}
	}

	src := resolvePath(args[0])
	dst := resolvePath(args[1])

	data, err := os.ReadFile(src)
	if err != nil {
		return CmdResult{"", fmt.Sprintf("mv: %s: %s\n", args[0], err.Error()), 1}
	}

	if err := SyncWriteFile(dst, string(data)); err != nil {
		return CmdResult{"", fmt.Sprintf("mv: cannot write to '%s': %s\n", args[1], err.Error()), 1}
	}

	if err := os.Remove(src); err != nil {
		return CmdResult{"", fmt.Sprintf("mv: cannot remove '%s': %s\n", args[0], err.Error()), 1}
	}

	store.DeleteFile(src)
	return CmdResult{"", "", 0}
}

func cmdTouch(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "touch: missing operand\n", 1}
	}

	for _, arg := range args {
		path := resolvePath(arg)
		dir := filepath.Dir(path)
		if dir != "" && dir != "." {
			os.MkdirAll(dir, 0755)
		}
		// If file exists, update mtime; if not, create empty file.
		if _, err := os.Stat(path); err != nil {
			if err := SyncWriteFile(path, ""); err != nil {
				return CmdResult{"", fmt.Sprintf("touch: %s: %s\n", arg, err.Error()), 1}
			}
		} else {
			now := time.Now()
			os.Chtimes(path, now, now)
		}
	}
	return CmdResult{"", "", 0}
}

func cmdEcho(args []string, stdin string) CmdResult {
	noNewline := false
	writeArgs := []string{}
	for _, a := range args {
		if a == "-n" {
			noNewline = true
		} else if a == "-e" {
			// Support escape sequences
			writeArgs = append(writeArgs, a)
		} else {
			writeArgs = append(writeArgs, a)
		}
	}

	line := strings.Join(writeArgs, " ")
	// Expand environment variables
	line = os.ExpandEnv(line)

	if noNewline {
		return CmdResult{line, "", 0}
	}
	return CmdResult{line + "\n", "", 0}
}

func cmdHead(args []string, stdin string) CmdResult {
	n := int64(10)
	targets := []string{}

	for i, a := range args {
		if strings.HasPrefix(a, "-n") || strings.HasPrefix(a, "-") {
			if strings.HasPrefix(a, "-n") {
				val := strings.TrimPrefix(a, "-n")
				if val == "" && i+1 < len(args) {
					val = args[i+1]
					args = append(args[:i], args[i+2:]...)
				}
				if parsed, err := strconv.ParseInt(val, 10, 64); err == nil {
					n = parsed
					continue
				}
			} else if parsed, err := strconv.ParseInt(a[1:], 10, 64); err == nil {
				n = parsed
				continue
			}
		}
		targets = append(targets, a)
	}

	var input string
	if len(targets) > 0 {
		data, err := os.ReadFile(resolvePath(targets[0]))
		if err != nil {
			return CmdResult{"", fmt.Sprintf("head: %s: %s\n", targets[0], err.Error()), 1}
		}
		input = string(data)
	} else {
		input = stdin
	}

	lines := strings.Split(input, "\n")
	if n < int64(len(lines)) {
		lines = lines[:n]
	}
	return CmdResult{strings.Join(lines, "\n") + "\n", "", 0}
}

func cmdTail(args []string, stdin string) CmdResult {
	n := int64(10)
	targets := []string{}

	for i, a := range args {
		if strings.HasPrefix(a, "-n") {
			val := strings.TrimPrefix(a, "-n")
			if val == "" && i+1 < len(args) {
				val = args[i+1]
				args = append(args[:i], args[i+2:]...)
			}
			if parsed, err := strconv.ParseInt(val, 10, 64); err == nil {
				n = parsed
				continue
			}
		} else if strings.HasPrefix(a, "-") {
			if parsed, err := strconv.ParseInt(a[1:], 10, 64); err == nil {
				n = parsed
				continue
			}
		}
		targets = append(targets, a)
	}

	var input string
	if len(targets) > 0 {
		data, err := os.ReadFile(resolvePath(targets[0]))
		if err != nil {
			return CmdResult{"", fmt.Sprintf("tail: %s: %s\n", targets[0], err.Error()), 1}
		}
		input = string(data)
	} else {
		input = stdin
	}

	lines := strings.Split(input, "\n")
	if int64(len(lines)) > n {
		lines = lines[len(lines)-int(n):]
	}
	return CmdResult{strings.Join(lines, "\n") + "\n", "", 0}
}

func cmdWc(args []string, stdin string) CmdResult {
	linesOnly := false
	charsOnly := false
	wordsOnly := false
	targets := []string{}

	for _, a := range args {
		switch a {
		case "-l":
			linesOnly = true
		case "-c", "-m":
			charsOnly = true
		case "-w":
			wordsOnly = true
		default:
			targets = append(targets, a)
		}
	}

	var input string
	if len(targets) > 0 {
		data, err := os.ReadFile(resolvePath(targets[0]))
		if err != nil {
			return CmdResult{"", fmt.Sprintf("wc: %s: %s\n", targets[0], err.Error()), 1}
		}
		input = string(data)
	} else {
		input = stdin
	}

	lineCount := int64(strings.Count(input, "\n"))
	wordCount := int64(len(strings.Fields(input)))
	charCount := int64(utf8.RuneCountInString(input))

	if linesOnly {
		return CmdResult{fmt.Sprintf("%d\n", lineCount), "", 0}
	}
	if wordsOnly {
		return CmdResult{fmt.Sprintf("%d\n", wordCount), "", 0}
	}
	if charsOnly {
		return CmdResult{fmt.Sprintf("%d\n", charCount), "", 0}
	}

	return CmdResult{fmt.Sprintf("%8d %8d %8d\n", lineCount, wordCount, charCount), "", 0}
}

func cmdGrep(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "grep: missing pattern\n", 1}
	}

	caseInsensitive := false
	invert := false
	lineNum := false
	count := false
	pattern := ""

	targets := []string{}
	for i, a := range args {
		if a == "-i" {
			caseInsensitive = true
		} else if a == "-v" {
			invert = true
		} else if a == "-n" {
			lineNum = true
		} else if a == "-c" {
			count = true
		} else if a == "-e" && i+1 < len(args) {
			pattern = args[i+1]
			i++
		} else if pattern == "" && !strings.HasPrefix(a, "-") {
			pattern = a
		} else if !strings.HasPrefix(a, "-") {
			targets = append(targets, a)
		}
	}

	if pattern == "" {
		return CmdResult{"", "grep: missing pattern\n", 1}
	}

	var input string
	if len(targets) > 0 {
		data, err := os.ReadFile(resolvePath(targets[0]))
		if err != nil {
			return CmdResult{"", fmt.Sprintf("grep: %s: %s\n", targets[0], err.Error()), 1}
		}
		input = string(data)
	} else {
		input = stdin
	}

	flags := ""
	if caseInsensitive {
		flags = "(?i)"
	}
	re, err := regexp.Compile(flags + pattern)
	if err != nil {
		return CmdResult{"", fmt.Sprintf("grep: invalid pattern: %s\n", err.Error()), 2}
	}

	lines := strings.Split(input, "\n")
	var out strings.Builder
	matchedCount := 0

	for i, line := range lines {
		matched := re.MatchString(line)
		if invert {
			matched = !matched
		}
		if matched {
			matchedCount++
			if count {
				continue
			}
			if lineNum {
				fmt.Fprintf(&out, "%d:", i+1)
			}
			out.WriteString(line)
			out.WriteString("\n")
		}
	}

	if count {
		return CmdResult{fmt.Sprintf("%d\n", matchedCount), "", 0}
	}

	return CmdResult{out.String(), "", 0}
}

func cmdSort(args []string, stdin string) CmdResult {
	numeric := false
	reverse := false
	unique := false
	paths := []string{}

	for _, a := range args {
		switch a {
		case "-n", "--numeric-sort":
			numeric = true
		case "-r", "--reverse":
			reverse = true
		case "-u", "--unique":
			unique = true
		default:
			paths = append(paths, a)
		}
	}

	var input string
	if len(paths) > 0 {
		data, err := os.ReadFile(resolvePath(paths[0]))
		if err != nil {
			return CmdResult{"", fmt.Sprintf("sort: %s: %s\n", paths[0], err.Error()), 1}
		}
		input = string(data)
	} else {
		input = stdin
	}

	lines := strings.Split(strings.TrimSpace(input), "\n")

	if numeric {
		sort.Slice(lines, func(i, j int) bool {
			a, _ := strconv.ParseFloat(strings.TrimSpace(lines[i]), 64)
			b, _ := strconv.ParseFloat(strings.TrimSpace(lines[j]), 64)
			if reverse {
				return a >= b
			}
			return a <= b
		})
	} else {
		if reverse {
			sort.Sort(sort.Reverse(sort.StringSlice(lines)))
		} else {
			sort.Strings(lines)
		}
	}

	if unique {
		seen := map[string]bool{}
		filtered := []string{}
		for _, l := range lines {
			key := l
			if numeric {
				key = strings.TrimSpace(l)
			}
			if !seen[key] {
				seen[key] = true
				filtered = append(filtered, l)
			}
		}
		lines = filtered
	}

	return CmdResult{strings.Join(lines, "\n") + "\n", "", 0}
}

func cmdFind(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		args = []string{"."}
	}

	startDir := resolvePath(args[0])
	namePattern := ""
	filterType := ""

	for i := 1; i < len(args); i++ {
		if args[i] == "-name" && i+1 < len(args) {
			namePattern = args[i+1]
			i++
		} else if args[i] == "-type" && i+1 < len(args) {
			filterType = args[i+1]
			i++
		}
	}

	var out strings.Builder
	err := filepath.Walk(startDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if namePattern != "" {
			matched, err := filepath.Match(namePattern, info.Name())
			if err != nil || !matched {
				return nil
			}
		}

		if filterType != "" {
			if filterType == "f" && info.IsDir() {
				return nil
			}
			if filterType == "d" && !info.IsDir() {
				return nil
			}
		}

		out.WriteString(path)
		out.WriteString("\n")
		return nil
	})

	if err != nil {
		return CmdResult{"", fmt.Sprintf("find: %s\n", err.Error()), 1}
	}

	return CmdResult{out.String(), "", 0}
}

func cmdTree(args []string, stdin string) CmdResult {
	showHidden := false
	maxDepth := -1
	path := "."
	targets := []string{}

	for i, a := range args {
		if a == "-a" {
			showHidden = true
		} else if strings.HasPrefix(a, "-L") {
			val := strings.TrimPrefix(a, "-L")
			if val == "" && i+1 < len(args) {
				val = args[i+1]
			}
			if parsed, err := strconv.Atoi(val); err == nil {
				maxDepth = parsed
			}
		} else if !strings.HasPrefix(a, "-") {
			targets = append(targets, a)
		}
	}

	if len(targets) > 0 {
		path = targets[0]
	}

	root := resolvePath(path)
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n", root)

	counts := []int{0, 0} // [dirs, files]

	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}

		if !showHidden && strings.HasPrefix(filepath.Base(p), ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if maxDepth > 0 {
			depth := strings.Count(rel, string(os.PathSeparator))
			if depth > maxDepth {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		depth := strings.Count(rel, string(os.PathSeparator))
		prefix := ""
		for j := 0; j < depth; j++ {
			prefix += "│   "
		}

		branch := "├── "
		if info.IsDir() {
			branch = "├── "
			counts[0]++
		} else {
			counts[1]++
		}

		fmt.Fprintf(&out, "%s%s%s\n", prefix, branch, info.Name())
		return nil
	})

	if err != nil {
		return CmdResult{"", fmt.Sprintf("tree: %s\n", err.Error()), 1}
	}

	fmt.Fprintf(&out, "\n%d directories, %d files\n", counts[0], counts[1])
	return CmdResult{out.String(), "", 0}
}

func cmdClear(args []string, stdin string) CmdResult {
	return CmdResult{"\x1b[H\x1b[2J", "", 0}
}

func cmdHelp(args []string, stdin string) CmdResult {
	var out strings.Builder
	out.WriteString("ledit-wasm shell commands:\n\n")
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
			// Try to parse as format
			if strings.HasPrefix(args[0], "+") {
				// Convert unix date format to Go format
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
	return CmdResult{shellEnv.Get("USER") + "\n", "", 0}
}

func cmdEnv(args []string, stdin string) CmdResult {
	var out strings.Builder
	for _, k := range sortedKeys(shellEnv.All()) {
		fmt.Fprintf(&out, "%s=%s\n", k, shellEnv.Get(k))
	}
	return CmdResult{out.String(), "", 0}
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
		return cmdEnv(args, stdin)
	}

	for _, arg := range args {
		eqIdx := strings.Index(arg, "=")
		if eqIdx < 0 {
			// export VAR (just mark for export, set from env)
			if val, ok := shellEnv.vars[arg]; ok {
				os.Setenv(arg, val)
			}
			continue
		}
		key := arg[:eqIdx]
		value := arg[eqIdx+1:]
		shellEnv.Set(key, value)
	}

	return CmdResult{"", "", 0}
}

func cmdWhich(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "which: missing argument\n", 1}
	}

	name := args[0]
	if isBuiltin(name) {
		return CmdResult{fmt.Sprintf("%s: ledit-wasm built-in command\n", name), "", 0}
	}

	return CmdResult{"", fmt.Sprintf("which: %s: not found\n", name), 1}
}

func isBuiltin(name string) bool {
	return builtinNames[name]
}

func cmdType(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "type: missing argument\n", 1}
	}

	name := args[0]
	if _, ok := cmdRegistry[name]; ok {
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
	abs, err := filepath.Abs(resolvePath(args[0]))
	if err != nil {
		return CmdResult{"", fmt.Sprintf("realpath: %s\n", err.Error()), 1}
	}
	return CmdResult{abs + "\n", "", 0}
}

func cmdTr(args []string, stdin string) CmdResult {
	if len(args) < 2 {
		return CmdResult{"", "tr: missing operand\n", 1}
	}

	from := args[0]
	to := args[1]
	deleteSet := false
	squeeze := false

	for _, a := range args[2:] {
		if a == "-d" {
			deleteSet = true
		} else if a == "-s" {
			squeeze = true
		}
	}

	result := stdin

	if deleteSet {
		for _, c := range from {
			result = strings.ReplaceAll(result, string(c), "")
		}
	} else {
		runes := []rune(result)
		for i, r := range runes {
			for j, f := range from {
				if r == f && j < len([]rune(to)) {
					runes[i] = []rune(to)[j]
					break
				}
			}
		}
		result = string(runes)
	}

	if squeeze && len(from) > 0 {
		r := []rune(from)[0]
		for strings.Contains(result, string(r)+string(r)) {
			result = strings.ReplaceAll(result, string(r)+string(r), string(r))
		}
	}

	if result != "" && !strings.HasSuffix(result, "\n") && strings.HasSuffix(stdin, "\n") {
		result += "\n"
	}

	return CmdResult{result, "", 0}
}

func cmdUniq(args []string, stdin string) CmdResult {
	lines := strings.Split(strings.TrimRight(stdin, "\n"), "\n")
	var out strings.Builder
	prev := ""
	countOnly := false

	for _, a := range args {
		if a == "-c" {
			countOnly = true
		}
	}

	if countOnly {
		counts := map[string]int{}
		for _, l := range lines {
			counts[l]++
		}
		for _, l := range lines {
			if counts[l] > 0 {
				fmt.Fprintf(&out, "%7d %s\n", counts[l], l)
				delete(counts, l)
			}
		}
	} else {
		for _, l := range lines {
			if l != prev {
				out.WriteString(l)
				out.WriteString("\n")
				prev = l
			}
		}
	}

	return CmdResult{out.String(), "", 0}
}

func cmdCut(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "cut: missing operand\n", 1}
	}

	delimiter := "\t"
	fieldStr := ""

	for i, a := range args {
		if strings.HasPrefix(a, "-d") {
			delimiter = strings.TrimPrefix(a, "-d")
			if delimiter == "" && i+1 < len(args) {
				delimiter = args[i+1]
			}
		} else if strings.HasPrefix(a, "-f") {
			fieldStr = strings.TrimPrefix(a, "-f")
			if fieldStr == "" && i+1 < len(args) {
				fieldStr = args[i+1]
			}
		}
	}

	if fieldStr == "" {
		return CmdResult{"", "cut: you must specify a list of fields\n", 1}
	}

	fields := []int{}
	for _, part := range strings.Split(fieldStr, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && n > 0 {
			fields = append(fields, n-1)
		}
	}

	lines := strings.Split(strings.TrimRight(stdin, "\n"), "\n")
	var out strings.Builder

	for _, line := range lines {
		parts := strings.Split(line, delimiter)
		var selected []string
		for _, f := range fields {
			if f < len(parts) {
				selected = append(selected, parts[f])
			}
		}
		out.WriteString(strings.Join(selected, delimiter))
		out.WriteString("\n")
	}

	return CmdResult{out.String(), "", 0}
}

func cmdTee(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{stdin, "", 0}
	}

	appendMode := false
	targets := []string{}

	for _, a := range args {
		if a == "-a" {
			appendMode = true
		} else {
			targets = append(targets, a)
		}
	}

	for _, t := range targets {
		path := resolvePath(t)
		if appendMode {
			existing := ""
			if data, err := os.ReadFile(path); err == nil {
				existing = string(data)
			}
			SyncWriteFile(path, existing+stdin)
		} else {
			SyncWriteFile(path, stdin)
		}
	}

	return CmdResult{stdin, "", 0}
}

// ─── Utility functions ──────────────────────────────────────────────────

// resolvePath expands ~ and makes relative paths absolute against cwd.
func resolvePath(p string) string {
	if p == "~" {
		return shellEnv.Get("HOME")
	}
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(shellEnv.Get("HOME"), p[2:])
	}

	if !filepath.IsAbs(p) {
		cwd, err := os.Getwd()
		if err == nil {
			p = filepath.Join(cwd, p)
		}
	}

	return filepath.Clean(p)
}

// expandGlobs expands glob patterns in arguments.
func expandGlobs(args []string) []string {
	var result []string
	for _, arg := range args {
		// Expand variables first
		expanded := os.ExpandEnv(arg)

		// Handle tilde
		if strings.HasPrefix(expanded, "~") {
			expanded = shellEnv.Get("HOME") + expanded[1:]
		}

		if strings.ContainsAny(expanded, "*?[") {
			matches, err := filepath.Glob(resolvePath(expanded))
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
	dir = resolvePath(dir)

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

// ListDirEntryJSON returns the JSON representation of directory entries.
func ListDirEntryJSON(path string) (string, error) {
	target := resolvePath(path)
	entries, err := os.ReadDir(target)
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
	data, err := os.ReadFile(resolvePath(path))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteFileContent writes content to a file.
func WriteFileContent(path, content string) error {
	return SyncWriteFile(resolvePath(path), content)
}

// DeleteFilePath deletes a file.
func DeleteFilePath(path string) error {
	return SyncDeleteFile(resolvePath(path))
}

// pipeStdin reads from a reader and returns string.
func pipeStdin(r io.Reader) string {
	data, _ := io.ReadAll(r)
	return string(data)
}

// byteReader converts a string to a reader for piping.
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
		args = expandGlobs(args)

		if i > 0 {
			// Redirect previous stdout to this stdin
			if fn, ok := cmdRegistry[name]; ok {
				result := fn(args, stdin)
				if result.ExitCode != 0 && len(commands) > 1 {
					return result
				}
				stdin = result.Stdout
			} else {
				return CmdResult{stdin, fmt.Sprintf("command not found: %s\n", name), 127}
			}
		} else {
			if fn, ok := cmdRegistry[name]; ok {
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

// WriteBuf is a convenience type — we already use bytes.Buffer via strings.Builder.
// This is for the case where we need to pass stdin through a pipe with redirects.
func runCommandWithRedirects(name string, args []string, stdin string, stdoutRedirect *string, stderrRedirect *string, appendStdout bool) CmdResult {
	args = expandGlobs(args)

	fn, ok := cmdRegistry[name]
	if !ok {
		return CmdResult{"", fmt.Sprintf("command not found: %s\n", name), 127}
	}

	result := fn(args, stdin)

	// Handle stdout redirect
	if stdoutRedirect != nil && *stdoutRedirect != "" {
		redirectPath := resolvePath(*stdoutRedirect)
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
		redirectPath := resolvePath(*stderrRedirect)
		SyncWriteFile(redirectPath, result.Stderr)
		result.Stderr = ""
	}

	return result
}

// Copying bufio into scope since it's used by some utilities.
var _ = bufio.NewReader
var _ = (*bytes.Buffer)(nil)
