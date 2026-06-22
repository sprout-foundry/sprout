package wasmshell

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func cmdLs(args []string, stdin string) CmdResult {
	showAll := false
	showLong := false
	humanSize := false

	// Parse flags
	for _, a := range args {
		switch a {
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
		}
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
		target := ResolvePath(p)
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
	if len(args) == 0 {
		return CmdResult{stdin, "", 0}
	}

	var out strings.Builder
	for _, arg := range args {
		path := ResolvePath(arg)
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
	mkdirArgs := []string{}
	for _, a := range args {
		if a == "-p" || a == "--parents" {
			parents = true
		} else {
			mkdirArgs = append(mkdirArgs, a)
		}
	}

	for _, arg := range mkdirArgs {
		path := ResolvePath(arg)
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
		path := ResolvePath(arg)
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
		path := ResolvePath(arg)
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

	src := ResolvePath(targets[0])
	dst := ResolvePath(targets[1])

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

	src := ResolvePath(args[0])
	dst := ResolvePath(args[1])

	srcInfo, err := os.Stat(src)
	if err != nil {
		return CmdResult{"", fmt.Sprintf("mv: %s: %s\n", args[0], err.Error()), 1}
	}

	// If source is a directory, copy recursively then remove source
	if srcInfo.IsDir() {
		if err := copyPath(src, dst, true); err != nil {
			return CmdResult{"", fmt.Sprintf("mv: %s\n", err.Error()), 1}
		}
		if rmErr := os.RemoveAll(src); rmErr != nil {
			return CmdResult{"", fmt.Sprintf("mv: cannot remove '%s': %s\n", args[0], rmErr.Error()), 1}
		}
		storeWriter.DeleteFile(src)
		return CmdResult{"", "", 0}
	}

	// Single file move
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

	storeWriter.DeleteFile(src)
	return CmdResult{"", "", 0}
}

func cmdTouch(args []string, stdin string) CmdResult {
	if len(args) == 0 {
		return CmdResult{"", "touch: missing operand\n", 1}
	}

	for _, arg := range args {
		path := ResolvePath(arg)
		dir := filepath.Dir(path)
		if dir != "" && dir != "." {
			os.MkdirAll(dir, 0755)
		}
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
