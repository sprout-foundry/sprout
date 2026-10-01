package wasmshell

import (
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
	sortTime := false
	recursive := false
	dirOnly := false
	classify := false

	// Parse flags, including clusters (-la, -lat, -laR, -ld…): every letter
	// must be a known ls flag letter.
	for _, a := range args {
		if !strings.HasPrefix(a, "-") || a == "-" {
			continue
		}
		if strings.HasPrefix(a, "--") {
			switch a {
			case "--all":
				showAll = true
			case "--long":
				showLong = true
			case "--human-readable":
				humanSize = true
			case "--time":
				sortTime = true
			case "--recursive":
				recursive = true
			case "--directory":
				dirOnly = true
			}
			continue
		}
		for _, ch := range strings.TrimPrefix(a, "-") {
			switch ch {
			case 'a':
				showAll = true
			case 'l':
				showLong = true
			case 'h':
				humanSize = true
			case 't':
				sortTime = true
			case 'R':
				recursive = true
			case 'd':
				dirOnly = true
			case 'F', 'p':
				classify = true
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

	if len(paths) == 0 {
		paths = []string{"."}
	}

	var out strings.Builder
	var errOut strings.Builder

	listDir := func(p string, header bool) {
		target := ResolvePath(p)
		entries, err := ReadDirCompat(target)
		if err != nil {
			fmt.Fprintf(&errOut, "ls: cannot access '%s': %s\n", p, describeErr(err))
			return
		}

		if header {
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
			if sortTime {
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
				if item.isDir && classify {
					out.WriteString("/")
				}
				out.WriteString("\n")
			}
		}
	}

	var listOne func(string)
	listOne = func(p string) {
		target := ResolvePath(p)

		if dirOnly {
			// ls -d: the directory entry itself, not its contents.
			info, err := os.Lstat(target)
			if err != nil {
				fmt.Fprintf(&errOut, "ls: cannot access '%s': %s\n", p, describeErr(err))
				return
			}
			if showLong {
				dirChar := "-"
				if info.IsDir() {
					dirChar = "d"
				}
				size := info.Size()
				if humanSize {
					fmt.Fprintf(&out, "%srwxr-xr-x 1 user user %8s %s %s\n",
						dirChar, humanizeSize(size), info.ModTime().Format("Jan 02 15:04"), p)
				} else {
					fmt.Fprintf(&out, "%srwxr-xr-x 1 user user %8d %s %s\n",
						dirChar, size, info.ModTime().Format("Jan 02 15:04"), p)
				}
			} else {
				out.WriteString(p)
				if info.IsDir() && classify {
					out.WriteString("/")
				}
				out.WriteString("\n")
			}
			return
		}

		info, err := os.Stat(target)
		if err == nil && !info.IsDir() {
			// A plain file argument: the entry itself, in long or bare form.
			if showLong {
				fmt.Fprintf(&out, "-rwxr-xr-x 1 user user %8d %s %s\n",
					info.Size(), info.ModTime().Format("Jan 02 15:04"), p)
			} else {
				fmt.Fprintf(&out, "%s\n", p)
			}
			return
		}

		listDir(p, len(paths) > 1 || recursive)

		// ls -R: recurse into subdirectories after listing each level.
		if recursive {
			entries, readErr := ReadDirCompat(target)
			if readErr != nil {
				return
			}
			for _, e := range entries {
				if !showAll && strings.HasPrefix(e.Name(), ".") {
					continue
				}
				if e.IsDir() {
					out.WriteString("\n")
					listOne(filepath.Join(p, e.Name()))
				}
			}
		}
	}

	for _, p := range paths {
		listOne(p)
	}

	exit := 0
	if errOut.Len() > 0 {
		exit = 2
	}
	return CmdResult{Stdout: out.String(), Stderr: errOut.String(), ExitCode: exit}
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
