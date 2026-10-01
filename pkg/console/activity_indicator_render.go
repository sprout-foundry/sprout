package console

// activity_indicator_render.go — the rendering layer of the activity
// indicator: the spinner goroutine (run), the line renderer (render),
// terminal width probing, and line sanitization. Split out of
// activity_indicator.go.
import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

func (a *ActivityIndicator) run() {
	// Capture channels at spawn time. If Stop times out and Start creates new
	// channels before this goroutine exits, accessing a.stopCh/a.doneCh via
	// the struct would race with the new goroutine and double-close doneCh.
	stopCh := a.stopCh
	doneCh := a.doneCh

	ticker := time.NewTicker(spinnerCadence)
	defer ticker.Stop()
	defer close(doneCh)

	// Render the first frame immediately so the user sees something within
	// 0ms rather than waiting for the first tick.
	a.render(0)
	frame := 1

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			a.render(frame)
			frame = (frame + 1) % len(spinnerFrames)
		}
	}
}

func (a *ActivityIndicator) render(frame int) {
	a.mu.Lock()
	if !a.active {
		a.mu.Unlock()
		return
	}
	msg := a.msg
	elapsed := time.Since(a.startedAt)
	a.mu.Unlock()
	// Serialize against InputReader render, status footer draw, and other
	// console chrome. Use TryLock instead of blocking Lock to avoid the
	// cascade deadlock: if another goroutine is holding outputMu during a
	// blocked I/O write (PTY buffer full, NFS hang), blocking here would
	// trap the render goroutine so Stop()'s doneCh never fires, which in
	// turn freezes the event subscriber (which calls Stop from ToolEnd).
	// Skipping a single frame is harmless; the spinner resumes next tick.
	if !TryLockOutput() {
		return
	}
	// Truncate msg so the full rendered line (spinner + space + msg + elapsed
	// suffix) fits within one terminal row. Without this, a long message wraps
	// to a second physical line; on the next tick \r only returns the cursor to
	// column 0 of the BOTTOM (wrapped) line, leaving stale frames frozen above.
	width := a.terminalWidth()
	suffix := fmt.Sprintf(" (%.1fs)", elapsed.Seconds())
	// Fixed overhead: spinner frame (1 col) + space (1 col).
	const overhead = 2
	// Progressive degradation so the rendered line never exceeds width:
	//   width >= overhead + suffix + 1  → spinner + msg + elapsed
	//   width >= overhead + 1           → spinner + msg (drop elapsed)
	//   width >= 1                      → spinner only
	// Each branch emits exactly one row so \r on the next tick clears it.
	switch {
	case width >= overhead+displayWidth(suffix)+1:
		msgBudget := width - overhead - displayWidth(suffix)
		msg = truncateLinePreservingANSI(msg, msgBudget)
		fmt.Fprintf(a.w, "\r\033[K%s %s%s", spinnerFrames[frame], msg, suffix)
	case width >= overhead+1:
		msgBudget := width - overhead
		msg = truncateLinePreservingANSI(msg, msgBudget)
		fmt.Fprintf(a.w, "\r\033[K%s %s", spinnerFrames[frame], msg)
	default:
		fmt.Fprintf(a.w, "\r\033[K%s", spinnerFrames[frame])
	}
	UnlockOutput()
}

// terminalWidth returns the column count to budget the rendered line against.
// It uses widthOverride when set (mainly for tests), otherwise queries the
// underlying *os.File's fd via term.GetSize, falling back to 80 when the
// writer isn't a *os.File or the size can't be determined.
func (a *ActivityIndicator) terminalWidth() int {
	if a.widthOverride > 0 {
		return a.widthOverride
	}
	if f, ok := a.w.(*os.File); ok {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
			return w
		}
	}
	return 80
}

// sanitizeLine strips newlines and carriage returns so the spinner always
// renders on a single row.
func sanitizeLine(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\n' || r == '\r' {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
