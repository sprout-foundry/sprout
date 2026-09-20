package console

import (
	"bytes"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// answerDA2 replies to a DA2 request observed on master with a Termux
// fingerprint reply. Returns immediately; the probe polls the fd.
func answerDA2(master *os.File) {
	go func() {
		buf := make([]byte, 32)
		for {
			n, err := master.Read(buf)
			if err != nil || n == 0 {
				return
			}
			if strings.Contains(string(buf[:n]), "\033[>c") {
				_, _ = master.Write([]byte("\033[>41;320;0c"))
				return
			}
		}
	}()
}

// TestFooterStartPrimesFlavorCache is the regression pin for the
// keyboard-dismiss history-eating bug: the DA2 probe used to run lazily
// on the FIRST Resize(), which on Termux is the first soft-keyboard
// toggle — after the input reader had already parked on stdin. The two
// readers raced for the reply; when the input reader won, the probe
// timed out and cached false forever, so every subsequent resize took
// the xterm union-clear path and wiped freshly pulled-down conversation
// lines (visible history) on each keyboard dismiss.
//
// Start() must prime the cache itself, before any reader exists. After
// a Start() against a Termux-answering pty, a Resize on a grow must
// clear ONLY the new-geometry window (bottom-anchored semantics) — not
// the union that eats the old-top rows.
func TestFooterStartPrimesFlavorCache(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("pty.Open failed: %v", err)
	}
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	raw, err := term.MakeRaw(int(slave.Fd()))
	if err != nil {
		t.Fatalf("MakeRaw on pty slave: %v", err)
	}
	t.Cleanup(func() { _ = term.Restore(int(slave.Fd()), raw) })
	answerDA2(master)

	f := &StatusFooter{
		w:            slave,
		isTTY:        true,
		active:       false,
		steerCursor:  -1,
		fd:           -1,
		sizeOverride: &terminalSizeOverride{cols: 80, rows: 24},
	}
	f.source = &stubSource{model: "test"}
	// The hint flag must be on BEFORE Start so the startup draw records
	// lastHintRows=1 — matching the production REPL (hint enabled),
	// where reserved=3 and the old footer top on a 24-row terminal is
	// row 22.
	f.mu.Lock()
	f.showKeymapHint = true
	f.mu.Unlock()

	// The probe round-trips through os.Stdin/os.Stdout (hardcoded in
	// bottomAnchoredResize, as in production). Point them at the pty
	// slave for the duration so the DA2 query/reply flows through the
	// pty; answerDA2 answers from the master end.
	origStdin, origStdout := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = slave, slave
	defer func() { os.Stdin, os.Stdout = origStdin, origStdout }()

	probeOverride = nil
	resetFlavorCache()
	defer resetFlavorCache()

	f.Start()

	if !bottomAnchoredResize() {
		t.Fatalf("Start() did not prime the flavor cache; bottomAnchoredResize()=false after Start against a Termux-answering pty")
	}

	// Simulate the keyboard-dismiss grow with the cached flavor: the
	// clear window must start at the NEW footer top (38), not the old
	// top (22) — the old-top rows hold pulled-down history under
	// bottom-anchored reflow. reserved=3 (rule+content+hint).
	f.mu.Lock()
	f.sizeOverride = &terminalSizeOverride{cols: 80, rows: 40}
	f.mu.Unlock()

	var buf bytes.Buffer
	out := captureResizeOutput(f, &buf)

	re := regexp.MustCompile(`\x1b\[(\d+);1H\x1b\[J`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("Resize emitted no clear-to-end window; output=%q", out)
	}
	clearTop, _ := strconv.Atoi(m[1])
	if clearTop != 38 {
		t.Fatalf("clear window starts at row %d; want 38 (new-geometry top only — union clear would eat history); output=%q", clearTop, out)
	}
}

// captureResizeOutput runs f.Resize() with f.w pointed at buf and
// returns what was written. The footer under test holds the pty (needed
// for the probe), so Resize's writes are re-routed to buf for capture.
func captureResizeOutput(f *StatusFooter, buf *bytes.Buffer) string {
	orig := f.w
	f.w = buf
	f.Resize()
	f.w = orig
	return buf.String()
}

// TestFooterStartProbeFailureLeavesTopAnchoredDefault pins the failure
// path: a silent terminal (no DA2 reply) still primes the cache (to
// false) at Start, and Resize keeps the historical union behavior —
// the conservative default for xterm-family terminals.
func TestFooterStartProbeFailureLeavesTopAnchoredDefault(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("pty.Open failed: %v", err)
	}
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	raw, err := term.MakeRaw(int(slave.Fd()))
	if err != nil {
		t.Fatalf("MakeRaw on pty slave: %v", err)
	}
	t.Cleanup(func() { _ = term.Restore(int(slave.Fd()), raw) })

	f := &StatusFooter{
		w:            slave,
		isTTY:        true,
		active:       false,
		steerCursor:  -1,
		fd:           -1,
		sizeOverride: &terminalSizeOverride{cols: 80, rows: 24},
	}
	f.source = &stubSource{model: "test"}
	f.mu.Lock()
	f.showKeymapHint = true
	f.mu.Unlock()

	// Silent pty: point os.Stdin/os.Stdout at the slave (probe path)
	// but never answer the DA2 query — the deadline path must still
	// prime the cache (to false) and bound Start's blocking.
	origStdin, origStdout := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = slave, slave
	defer func() { os.Stdin, os.Stdout = origStdin, origStdout }()

	probeOverride = nil
	resetFlavorCache()
	defer resetFlavorCache()

	start := time.Now()
	f.Start()
	elapsed := time.Since(start)

	if bottomAnchoredResize() {
		t.Fatalf("bottomAnchoredResize()=true against a silent pty; want false")
	}
	// The probe must not hang Start: bounded by probeDeadline plus
	// scheduling slack.
	if elapsed > 3*probeDeadline {
		t.Fatalf("Start blocked %v on the probe; want bounded by ~%v", elapsed, probeDeadline)
	}

	// Union behavior retained on grow: clear starts at the OLD top (22).
	// reserved=3 (rule+content+hint).
	f.mu.Lock()
	f.sizeOverride = &terminalSizeOverride{cols: 80, rows: 40}
	f.mu.Unlock()

	var buf bytes.Buffer
	out := captureResizeOutput(f, &buf)

	re := regexp.MustCompile(`\x1b\[(\d+);1H\x1b\[J`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("Resize emitted no clear-to-end window; output=%q", out)
	}
	clearTop, _ := strconv.Atoi(m[1])
	if clearTop != 22 {
		t.Fatalf("clear window starts at row %d; want 22 (old footer top, union semantics); output=%q", clearTop, out)
	}
}
