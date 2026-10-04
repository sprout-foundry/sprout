package console

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
)

// ---------------------------------------------------------------------------
// formatting helpers
// ---------------------------------------------------------------------------

func formatCtx(used, limit int) string {
	if limit <= 0 {
		return formatTokens(used)
	}
	if used <= 0 {
		return formatTokens(limit)
	}
	return fmt.Sprintf("%s/%s", formatTokens(used), formatTokens(limit))
}

func formatTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%dM", n/1_000_000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

func formatCost(c float64) string {
	switch {
	case c < 0.01:
		return fmt.Sprintf("$%.4f", c)
	case c < 1.0:
		return fmt.Sprintf("$%.3f", c)
	default:
		return fmt.Sprintf("$%.2f", c)
	}
}

func shortPath(p string) string {
	if p == "" {
		return ""
	}
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func cwdSegment(cwd, branch string) string {
	if branch == "" {
		return cwd
	}
	return cwd + " (" + branch + ")"
}

// fitCwdSegment renders cwdSegment within width columns by dropping leading
// path components ("…/sprout/worktrees/x (main)"), then the branch, before
// finally truncating the directory name itself.
func fitCwdSegment(cwd, branch string, width int) string {
	if full := cwdSegment(cwd, branch); width <= 0 || displayWidth(full) <= width {
		return full
	}
	parts := strings.Split(cwd, "/")
	for keep := len(parts) - 1; keep >= 1; keep-- {
		if s := cwdSegment("…/"+strings.Join(parts[len(parts)-keep:], "/"), branch); displayWidth(s) <= width {
			return s
		}
	}
	base := parts[len(parts)-1]
	if branch != "" && displayWidth(base) <= width {
		return base
	}
	return truncateToWidth(base, width, "…")
}

// branchCacheTTL bounds how stale the footer's branch can be. The footer
// redraws on every keystroke while the slash dropdown is open; without the
// cache each redraw would spawn git.
const branchCacheTTL = 2 * time.Second

var branchCache struct {
	sync.Mutex
	dir, branch string
	at          time.Time
}

func cachedGitBranchOf(dir string) string {
	branchCache.Lock()
	defer branchCache.Unlock()
	if dir == branchCache.dir && time.Since(branchCache.at) < branchCacheTTL {
		return branchCache.branch
	}
	branchCache.dir, branchCache.branch, branchCache.at = dir, gitBranchOf(dir), time.Now()
	return branchCache.branch
}

// gitBranchOf returns the current git branch for the directory, or empty
// string if not a git repo or git is unavailable. Fast-fails when no
// .git is present; only shells out to git when one exists.
func gitBranchOf(dir string) string {
	if dir == "" {
		return ""
	}
	// Walk up looking for .git so subdirectories of a repo report the
	// repo's branch. Bail at filesystem root.
	probe := dir
	for {
		if _, err := os.Stat(probe + "/.git"); err == nil {
			break
		}
		parent := stripTail(probe)
		if parent == probe || parent == "" {
			return "" // not in a git repo
		}
		probe = parent
	}
	cmd := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func stripTail(p string) string {
	i := strings.LastIndexByte(p, '/')
	if i < 0 {
		return ""
	}
	if i == 0 {
		return "/"
	}
	return p[:i]
}

func truncTo(s string, n int) string {
	if displayWidth(s) <= n {
		return s
	}
	if n <= 1 {
		return truncateToWidth(s, n, "")
	}
	return truncateToWidth(s, n, "…")
}

// truncWithEllipsis clamps s to at most n display columns, preserving ANSI
// styling escapes (they don't count toward the budget) and cutting only on rune
// boundaries so wide/CJK content is never split. Appends "…" when it cuts.
func truncWithEllipsis(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if n == 1 {
		return " "
	}
	if visibleLen(s) <= n {
		return s
	}
	budget := n - 1 // reserve a column for the ellipsis
	var b strings.Builder
	w := 0
	inEsc := false
	for _, r := range s {
		if inEsc {
			b.WriteRune(r)
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if r == '\033' {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		rw := runewidth.RuneWidth(r)
		if w+rw > budget {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

// visibleLen returns the display-column width of s, ignoring ANSI escapes
// (wide/CJK runes count as 2, combining as 0).
func visibleLen(s string) int {
	return displayWidth(s)
}
