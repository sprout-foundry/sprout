package wasmshell

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// findEntry is one visited path.
type findEntry struct {
	display string
	rel     string
	abs     string
	info    os.FileInfo
	depth   int
	prune   bool
}

type findExpr func(e *findEntry) bool

type findRun struct {
	sh        *interp
	args      []string
	pos       int
	out       strings.Builder
	errs      strings.Builder
	hasAction bool
	quit      bool
	deletes   []string
	batches   []*findBatch
	code      int
	err       error
}

type findBatch struct {
	argv  []string
	paths []string
}

func biFind(sh *interp, args []string, _ *ioIn) CmdResult {
	var roots []string
	i := 0
	for i < len(args) && !strings.HasPrefix(args[i], "-") && args[i] != "(" && args[i] != "!" {
		roots = append(roots, args[i])
		i++
	}
	if len(roots) == 0 {
		roots = []string{"."}
	}
	f := &findRun{sh: sh, args: args[i:]}
	var maxDepth, minDepth int
	f.args, maxDepth, minDepth = extractDepthOptions(f.args)

	expr := f.or()
	if f.err == nil && f.pos < len(f.args) {
		f.err = fmt.Errorf("unexpected argument: %s", f.args[f.pos])
	}
	if f.err != nil {
		code := 1
		if strings.Contains(f.err.Error(), "not supported") {
			code = ExitCommandNotFound
		}
		return CmdResult{Stdout: "", Stderr: "find: " + f.err.Error() + "\n", ExitCode: code}
	}
	if expr == nil {
		expr = func(*findEntry) bool { return true }
	}

	for _, root := range roots {
		abs := ResolvePath(root)
		if _, err := os.Lstat(abs); err != nil { //nolint:gosec // G703: shell commands act on the paths the user names
			fmt.Fprintf(&f.errs, "find: '%s': %s\n", root, describeErr(err))
			f.code = 1
			continue
		}
		_ = WalkCompat(abs, func(path string, info os.FileInfo, err error) error {
			if f.quit {
				return filepath.SkipAll
			}
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(abs, path)
			e := &findEntry{abs: path, info: info, rel: rel, depth: findDepth(abs, path)}
			e.display = joinFindPath(root, rel)
			if maxDepth >= 0 && e.depth > maxDepth {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if e.depth >= minDepth {
				if expr(e) && !f.hasAction {
					f.out.WriteString(e.display + "\n")
				}
			}
			if f.quit {
				return filepath.SkipAll
			}
			if info.IsDir() && (e.prune || (maxDepth >= 0 && e.depth == maxDepth)) && e.depth > 0 {
				return filepath.SkipDir
			}
			return nil
		})
	}
	for _, b := range f.batches {
		if len(b.paths) == 0 {
			continue
		}
		var argv []string
		for _, a := range b.argv {
			if a == "{}" {
				argv = append(argv, b.paths...)
			} else {
				argv = append(argv, a)
			}
		}
		r := f.sh.runArgv(argv, "")
		f.out.WriteString(r.Stdout)
		f.errs.WriteString(r.Stderr)
		if r.ExitCode != 0 {
			f.code = r.ExitCode
		}
	}
	for k := len(f.deletes) - 1; k >= 0; k-- {
		if err := removePath(f.deletes[k]); err != nil {
			fmt.Fprintf(&f.errs, "find: cannot delete '%s': %s\n", f.deletes[k], describeErr(err))
			f.code = 1
		}
	}
	return CmdResult{Stdout: f.out.String(), Stderr: f.errs.String(), ExitCode: f.code}
}

func joinFindPath(root, rel string) string {
	if rel == "." {
		return root
	}
	sep := string(filepath.Separator)
	if strings.HasSuffix(root, sep) || strings.HasSuffix(root, "/") {
		return root + rel
	}
	return root + sep + rel
}

func findDepth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(rel, string(os.PathSeparator)) + 1
}

// extractDepthOptions pulls the position-independent -maxdepth/-mindepth
// options (and no-op global options) out of the expression.
func extractDepthOptions(args []string) ([]string, int, int) {
	maxDepth, minDepth := -1, 0
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-maxdepth", "-mindepth":
			if i+1 < len(args) {
				n, err := strconv.Atoi(args[i+1])
				if err == nil {
					if args[i] == "-maxdepth" {
						maxDepth = n
					} else {
						minDepth = n
					}
					i++
					continue
				}
			}
			rest = append(rest, args[i])
		case "-depth", "-follow", "-xdev", "-mount", "-noleaf", "-ignore_readdir_race", "-L", "-P", "-H":
		default:
			rest = append(rest, args[i])
		}
	}
	return rest, maxDepth, minDepth
}

// removePath deletes a file or a whole tree, keeping the persistent
// store in step.
func removePath(path string) error {
	info, err := os.Lstat(path) //nolint:gosec // G703: shell commands act on the paths the user names
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return SyncDeleteFile(path)
	}
	var files []string
	_ = WalkCompat(path, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	for _, p := range files {
		storeWriter.DeleteFile(p)
	}
	return os.RemoveAll(path) //nolint:gosec // G703: shell commands act on the paths the user names
}
