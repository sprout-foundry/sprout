package console

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"

	"golang.org/x/term"
)

// StatusFooter renders a single pinned line at the bottom of the terminal
// showing live session state: model, context-window usage, cumulative cost,
// and working directory.
//
// Mechanism: when started, the footer sets a terminal scroll region of
// rows 1..(N-1) where N is the terminal height. Subsequent output scrolls
// within that region; row N stays put for the footer. On Stop (and on
// signal-driven shutdown) the scroll region is reset so the user's
// terminal isn't left in a broken state.
//
// Suppressed entirely on non-TTY writers — Render is a no-op, scroll
// region is never touched.
type StatusFooter struct {
	mu     sync.Mutex
	w      io.Writer
	isTTY  bool
	fd     int
	active bool
	source ContentSource

	winchStop chan struct{}
	winchDone chan struct{}

	// Cost-warn thresholds (USD). Costs above warn render yellow; above
	// alert render red. Sane defaults; future config wiring possible.
	WarnCost  float64
	AlertCost float64
}

// ContentSource supplies the current values rendered in the footer. The
// footer reads from it on every Refresh; the source must be safe for
// concurrent calls.
type ContentSource interface {
	Model() string
	ContextTokens() (used, limit int)
	TotalCost() float64
	WorkingDir() string
}

// NewStatusFooter constructs a footer that writes to w. If w is nil
// os.Stderr is used (the same channel the spinner uses). Non-TTY writers
// produce a no-op footer.
func NewStatusFooter(w io.Writer, source ContentSource) *StatusFooter {
	if w == nil {
		w = os.Stderr
	}
	isTTY := false
	fd := -1
	if f, ok := w.(*os.File); ok {
		fd = int(f.Fd())
		isTTY = term.IsTerminal(fd)
	}
	return &StatusFooter{
		w:         w,
		isTTY:     isTTY,
		fd:        fd,
		source:    source,
		WarnCost:  1.0,
		AlertCost: 5.0,
	}
}

// Start declares the scroll region, spawns a SIGWINCH watcher, and renders
// the initial footer line. Safe to call multiple times; redundant calls
// just re-render (idempotent on the watcher).
func (f *StatusFooter) Start() {
	if f == nil || !f.isTTY || f.source == nil {
		return
	}
	f.mu.Lock()
	wasActive := f.active
	f.active = true
	if !wasActive {
		f.winchStop = make(chan struct{})
		f.winchDone = make(chan struct{})
	}
	stopCh := f.winchStop
	doneCh := f.winchDone
	f.mu.Unlock()

	f.applyScrollRegion()
	f.draw()

	if !wasActive {
		go f.watchResize(stopCh, doneCh)
	}
}

// Refresh re-reads the source and redraws the footer. Idempotent and
// cheap; safe to call from event subscribers on each ToolEnd.
func (f *StatusFooter) Refresh() {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	active := f.active
	f.mu.Unlock()
	if !active {
		return
	}
	f.draw()
}

// Resize handles a terminal-size change (SIGWINCH). The scroll region is
// re-applied for the new height and the footer is redrawn.
func (f *StatusFooter) Resize() {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	active := f.active
	f.mu.Unlock()
	if !active {
		return
	}
	f.applyScrollRegion()
	f.draw()
}

// Stop resets the scroll region to full-screen, clears the footer row, and
// halts the SIGWINCH watcher. MUST be called on every exit path (including
// signal-driven shutdown) or the user's terminal is left with a broken
// scroll region. Idempotent — safe to call when already stopped.
func (f *StatusFooter) Stop() {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	if !f.active {
		f.mu.Unlock()
		return
	}
	f.active = false
	stopCh := f.winchStop
	doneCh := f.winchDone
	f.winchStop = nil
	f.winchDone = nil
	f.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
		<-doneCh
	}

	_, rows := f.terminalSize()
	if rows > 0 {
		// Move to footer row and clear it before resetting the region,
		// so we don't leave the footer text behind in the scrollback.
		fmt.Fprintf(f.w, "\033[%d;1H\033[K", rows)
	}
	// Reset scroll region to full screen.
	fmt.Fprint(f.w, "\033[r")
	// Restore cursor to a sensible position (one row above the footer).
	if rows > 1 {
		fmt.Fprintf(f.w, "\033[%d;1H", rows-1)
	}
}

// watchResize listens for SIGWINCH (or the platform equivalent) and
// re-applies the scroll region + redraws the footer. Exits when stopCh
// is closed. On platforms without SIGWINCH (Windows, js/wasm) the goroutine
// just waits for stopCh and never fires Resize.
func (f *StatusFooter) watchResize(stopCh, doneCh chan struct{}) {
	defer close(doneCh)
	sig := resizeSignal()
	if sig == nil {
		<-stopCh
		return
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sig)
	defer signal.Stop(ch)
	for {
		select {
		case <-stopCh:
			return
		case <-ch:
			f.Resize()
		}
	}
}

// applyScrollRegion sets the scroll region to rows 1..(rows-1) so the
// last row is reserved for the footer. No-op when terminal size is
// unreadable.
func (f *StatusFooter) applyScrollRegion() {
	_, rows := f.terminalSize()
	if rows < 2 {
		return
	}
	// DECSTBM: set scroll region. After setting, cursor moves to the
	// home position of the new region (row 1, col 1) per VT100 spec.
	// We then move it back to a useful spot just above the footer so
	// subsequent prints land where the user expects.
	fmt.Fprintf(f.w, "\033[1;%dr", rows-1)
	fmt.Fprintf(f.w, "\033[%d;1H", rows-1)
}

// draw renders the current footer line. Uses save/restore cursor (DEC
// private mode \0337/\0338) so any in-flight prompt rendering above the
// footer row is not perturbed.
func (f *StatusFooter) draw() {
	cols, rows := f.terminalSize()
	if rows < 2 {
		return
	}
	line := f.composeLine(cols)

	// \0337 save cursor; jump to footer row; clear line; write; restore.
	fmt.Fprint(f.w, "\0337")
	fmt.Fprintf(f.w, "\033[%d;1H\033[K%s\0338", rows, line)
}

// composeLine builds the single-row footer string, padded/truncated to
// cols width. Cost is colored against WarnCost/AlertCost.
func (f *StatusFooter) composeLine(cols int) string {
	if f.source == nil {
		return ""
	}
	model := truncTo(f.source.Model(), 30)
	used, limit := f.source.ContextTokens()
	cost := f.source.TotalCost()
	cwd := shortPath(f.source.WorkingDir())
	branch := gitBranchOf(cwd)

	ctxStr := formatCtx(used, limit)
	costStr := formatCost(cost)
	costStyled := f.styleCost(cost, costStr)

	parts := []string{model, ctxStr, costStyled, cwdSegment(cwd, branch)}
	// Render with " · " separators; pad with a thin line on either end so
	// the bar visually rests at the bottom of the screen.
	body := " ─ " + strings.Join(parts, " · ") + " "
	// Truncate or pad to terminal width. ANSI codes are not visible chars;
	// rather than measure visible width here we accept slight over/underfill
	// when cost styling is on — terminals tolerate it.
	if visibleLen(body) >= cols {
		return truncWithEllipsis(body, cols)
	}
	return body + strings.Repeat("─", cols-visibleLen(body))
}

func (f *StatusFooter) styleCost(cost float64, text string) string {
	if cost >= f.AlertCost {
		return "\033[31m" + text + "\033[0m" // red
	}
	if cost >= f.WarnCost {
		return "\033[33m" + text + "\033[0m" // yellow
	}
	return text
}

// Global registration so signal handlers (which don't have a footer
// reference) can stop the footer before force-quitting via os.Exit, which
// otherwise skips deferred cleanup and leaves the terminal with a broken
// scroll region.
var (
	globalFooter   *StatusFooter
	globalFooterMu sync.RWMutex
)

// RegisterGlobalStatusFooter installs f as the process-wide footer that
// StopGlobalStatusFooter targets. Pass nil to clear. Safe to call
// multiple times. Mirrors RegisterGlobalIndicator.
func RegisterGlobalStatusFooter(f *StatusFooter) {
	globalFooterMu.Lock()
	defer globalFooterMu.Unlock()
	globalFooter = f
}

// StopGlobalStatusFooter resets the registered global footer's scroll
// region and clears its row. Safe to call when no footer is registered or
// when it's already stopped (no-op). Use from signal handlers immediately
// before os.Exit so the user's terminal isn't left in a weird state.
func StopGlobalStatusFooter() {
	globalFooterMu.RLock()
	f := globalFooter
	globalFooterMu.RUnlock()
	f.Stop()
}

func (f *StatusFooter) terminalSize() (cols, rows int) {
	if f.fd < 0 {
		return 0, 0
	}
	c, r, err := term.GetSize(f.fd)
	if err != nil {
		return 0, 0
	}
	return c, r
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func formatCtx(used, limit int) string {
	if limit <= 0 {
		return formatTokens(used) + " ctx"
	}
	return fmt.Sprintf("%s/%s ctx", formatTokens(used), formatTokens(limit))
}

func formatTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
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
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func truncWithEllipsis(s string, n int) string {
	if n <= 1 {
		return strings.Repeat(" ", n)
	}
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// visibleLen counts non-ANSI runes. Cheap implementation that handles
// only the styling sequences we emit (\033[31m, \033[33m, \033[0m).
func visibleLen(s string) int {
	count := 0
	in := false
	for _, r := range s {
		if in {
			if r == 'm' {
				in = false
			}
			continue
		}
		if r == '\033' {
			in = true
			continue
		}
		count++
	}
	return count
}
