package wasmshell

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// lsContext carries the ls flag state and the shared output builders.
type lsContext struct {
	showAll, showLong, humanSize, sortTime, recursive, dirOnly bool
	paths                                                      []string
	out, errOut                                                strings.Builder
}

// parseLsFlags parses ls flags (including clustered forms like -la) and
// returns the flag state plus the positional path arguments.
func parseLsFlags(args []string) (*lsContext, []string) {
	ctx := &lsContext{}
	// Parse flags, including clusters (-la, -lat, -laR, -ld…): every letter
	// must be a known ls flag letter.
	for _, a := range args {
		if !strings.HasPrefix(a, "-") || a == "-" {
			continue
		}
		if strings.HasPrefix(a, "--") {
			switch a {
			case "--all":
				ctx.showAll = true
			case "--long":
				ctx.showLong = true
			case "--human-readable":
				ctx.humanSize = true
			case "--time":
				ctx.sortTime = true
			case "--recursive":
				ctx.recursive = true
			case "--directory":
				ctx.dirOnly = true
			}
			continue
		}
		for _, ch := range strings.TrimPrefix(a, "-") {
			switch ch {
			case 'a':
				ctx.showAll = true
			case 'l':
				ctx.showLong = true
			case 'h':
				ctx.humanSize = true
			case 't':
				ctx.sortTime = true
			case 'R':
				ctx.recursive = true
			case 'd':
				ctx.dirOnly = true
			}
		}
	}

	// Find non-flag arguments
	paths := []string{}
	for _, a := range args {
		if strings.HasPrefix(a, "-") && a != "-" {
			continue
		}
		paths = append(paths, a)
	}
	return ctx, paths
}

// listDir lists one directory, appending to ctx.out/ctx.errOut.
func (ctx *lsContext) listDir(p string, header bool) {
	target := ResolvePath(p)
	entries, err := ReadDirCompat(target)
	if err != nil {
		fmt.Fprintf(&ctx.errOut, "ls: cannot access '%s': %s\n", p, err.Error())
		return
	}

	if header {
		fmt.Fprintf(&ctx.out, "%s:\n", p)
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

	if ctx.showAll {
		items = append(items, entry{name: ".", isDir: true})
		items = append(items, entry{name: "..", isDir: true})
	}

	for _, e := range entries {
		if !ctx.showAll && strings.HasPrefix(e.Name(), ".") {
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
		if ctx.sortTime {
			if items[i].modTime.Equal(items[j].modTime) {
				return items[i].name < items[j].name
			}
			return items[i].modTime.After(items[j].modTime)
		}
		if items[i].isDir != items[j].isDir {
			return items[i].isDir
		}
		return items[i].name < items[j].name
	})

	for _, item := range items {
		if ctx.showLong {
			dirChar := "-"
			if item.isDir {
				dirChar = "d"
			}
			size := item.size
			if ctx.humanSize {
				ctx.out.WriteString(fmt.Sprintf("%srwxr-xr-x 1 user user %8s %s %s\n",
					dirChar, humanizeSize(size), item.modTime.Format("Jan 02 15:04"), item.name))
			} else {
				ctx.out.WriteString(fmt.Sprintf("%srwxr-xr-x 1 user user %8d %s %s\n",
					dirChar, size, item.modTime.Format("Jan 02 15:04"), item.name))
			}
		} else {
			ctx.out.WriteString(item.name)
			if item.isDir {
				ctx.out.WriteString("/")
			}
			ctx.out.WriteString("\n")
		}
	}
}

// listOne lists a single path argument (directory, file, or -d entry),
// recursing into subdirectories when ctx.recursive is set.
func (ctx *lsContext) listOne(p string) {
	target := ResolvePath(p)

	if ctx.dirOnly {
		// ls -d: the directory entry itself, not its contents.
		info, err := os.Lstat(target)
		if err != nil {
			fmt.Fprintf(&ctx.errOut, "ls: cannot access '%s': %s\n", p, err.Error())
			return
		}
		if ctx.showLong {
			dirChar := "-"
			if info.IsDir() {
				dirChar = "d"
			}
			size := info.Size()
			if ctx.humanSize {
				fmt.Fprintf(&ctx.out, "%srwxr-xr-x 1 user user %8s %s %s\n",
					dirChar, humanizeSize(size), info.ModTime().Format("Jan 02 15:04"), p)
			} else {
				fmt.Fprintf(&ctx.out, "%srwxr-xr-x 1 user user %8d %s %s\n",
					dirChar, size, info.ModTime().Format("Jan 02 15:04"), p)
			}
		} else {
			ctx.out.WriteString(p)
			if info.IsDir() {
				ctx.out.WriteString("/")
			}
			ctx.out.WriteString("\n")
		}
		return
	}

	info, err := os.Stat(target)
	if err == nil && !info.IsDir() {
		// A plain file argument: the entry itself, in long or bare form.
		if ctx.showLong {
			fmt.Fprintf(&ctx.out, "-rwxr-xr-x 1 user user %8d %s %s\n",
				info.Size(), info.ModTime().Format("Jan 02 15:04"), p)
		} else {
			fmt.Fprintf(&ctx.out, "%s\n", p)
		}
		return
	}

	ctx.listDir(p, len(ctx.paths) > 1 || ctx.recursive)

	// ls -R: recurse into subdirectories after listing each level.
	if ctx.recursive {
		entries, readErr := ReadDirCompat(target)
		if readErr != nil {
			return
		}
		for _, e := range entries {
			if !ctx.showAll && strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if e.IsDir() {
				ctx.out.WriteString("\n")
				ctx.listOne(filepath.Join(p, e.Name()))
			}
		}
	}
}
