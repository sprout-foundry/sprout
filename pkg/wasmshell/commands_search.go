package wasmshell

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

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

	root := ResolvePath(path)
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n", root)

	counts := []int{0, 0} // [dirs, files]

	err := WalkCompat(root, func(p string, info os.FileInfo, err error) error {
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
		return CmdResult{Stdout: "", Stderr: fmt.Sprintf("tree: %s\n", err.Error()), ExitCode: 1}
	}

	fmt.Fprintf(&out, "\n%d directories, %d files\n", counts[0], counts[1])
	return CmdResult{Stdout: out.String(), Stderr: "", ExitCode: 0}
}
