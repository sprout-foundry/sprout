package wasmshell

// commands_file_mutation.go — the filesystem mutation commands of the
// shell: mkdir, rm, rmdir, cp, mv, touch, and their copy helpers.
// Split out of commands_file.go.
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
		return WalkCompat(src, func(path string, info os.FileInfo, err error) error {
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
