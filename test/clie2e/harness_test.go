//go:build unix

// Package clie2e drives the real `sprout agent --mock-llm` REPL in a pty and
// asserts what a user would see, through the vtscreen emulator. It covers
// what unit tests over the writers cannot: the whole program's output — the
// footer, the transcript writer, the readers, resizes — arriving at one
// terminal in the order the process actually produces it.
//
// It builds and runs the binary, so it is opt-in: SPROUT_CLI_E2E=1 (or
// `make test-cli-e2e`). SPROUT_E2E_BIN points at a prebuilt binary.
package clie2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/sprout-foundry/sprout/pkg/testutil/vtscreen"
)

var (
	binOnce sync.Once
	binPath string
	binErr  error
)

// sproutBinary returns the binary under test, building it once per run.
func sproutBinary(t *testing.T) string {
	t.Helper()
	if os.Getenv("SPROUT_CLI_E2E") != "1" {
		t.Skip("CLI end-to-end tests are opt-in: SPROUT_CLI_E2E=1 (make test-cli-e2e)")
	}
	if p := os.Getenv("SPROUT_E2E_BIN"); p != "" {
		return p
	}
	binOnce.Do(func() {
		dir, err := os.MkdirTemp("", "sprout-e2e-bin")
		if err != nil {
			binErr = err
			return
		}
		binPath = filepath.Join(dir, "sprout")
		// The same tags as `make build`; grammars must be prepared
		// (make prepare-grammars). The make target passes a built binary
		// instead.
		cmd := exec.Command("go", "build", "-tags", "grammar_blobs_external", "-o", binPath, ".") //nolint:gosec // G204: builds this repository
		cmd.Dir = filepath.Join("..", "..")
		if out, err := cmd.CombinedOutput(); err != nil {
			binErr = fmt.Errorf("go build: %w\n%s", err, out)
		}
	})
	if binErr != nil {
		t.Fatal(binErr)
	}
	return binPath
}

// session is one running REPL.
type session struct {
	t    *testing.T
	ptmx *os.File
	cmd  *exec.Cmd
	scr  *vtscreen.Screen

	// resizeMode is the resize semantics this session's terminal models. The
	// DA2 answer must agree with it: that reply is how the console learns the
	// difference, and a fixture that answers with the wrong flavor tests a
	// code path the terminal it models never reaches.
	resizeMode vtscreen.ResizeMode

	mu   sync.Mutex
	raw  bytes.Buffer // everything the process wrote, for failure messages
	done chan struct{}
}

// queries the program sends a terminal and the harness answers, as a real
// terminal would: cursor position (DSR 6) and device attributes (DA1/DA2).
var terminalQuery = regexp.MustCompile(`\x1b\[(6n|>0?c|0?c)`)

// start launches the REPL at cols×rows with an isolated home and config.
// replies are the mock LLM's answers, one per turn.
func start(t *testing.T, cols, rows int, replies ...string) *session {
	t.Helper()
	return startWithResizeMode(t, cols, rows, vtscreen.ResizeNonReflow, replies...)
}

// startWithResizeMode is start on a terminal that resizes like mode. The flavor
// is fixed before the process starts because the console identifies the
// terminal with a DA2 round trip while its footer first draws; answering
// afterwards would test the wrong path at random.
func startWithResizeMode(t *testing.T, cols, rows int, mode vtscreen.ResizeMode, replies ...string) *session {
	t.Helper()
	bin := sproutBinary(t)
	home := t.TempDir()
	work := t.TempDir()
	cmd := exec.Command(bin, "agent", "--mock-llm", "--no-connection-check", "--no-daemon") //nolint:gosec // G204: the binary under test
	cmd.Dir = work
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		"SPROUT_CONFIG="+filepath.Join(home, ".sprout"),
		"SPROUT_NO_UPDATE_CHECK=1",
		"SPROUT_DAEMON_AGENT=0",
		"TERM=xterm-256color",
		"NO_COLOR=1",
		"SPROUT_MOCK_LLM_QUEUE="+strings.Join(replies, "\n---MOCK-LIGHTWEIGHT-TURN---\n"),
	)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}) //nolint:gosec // G115: test sizes
	if err != nil {
		t.Fatalf("start sprout in a pty: %v", err)
	}
	s := &session{t: t, ptmx: ptmx, cmd: cmd, scr: vtscreen.New(cols, rows),
		resizeMode: mode, done: make(chan struct{})}
	s.scr.TranslateNewlines(false) // the pty already applied ONLCR
	s.scr.SetResizeMode(mode)
	go s.pump()
	t.Cleanup(func() {
		if t.Failed() {
			s.saveRaw()
		}
		s.stop()
	})
	return s
}

// pump copies the program's output into the emulator, answering terminal
// queries on the way.
func (s *session) pump() {
	defer close(s.done)
	buf := make([]byte, 32*1024)
	var tail string
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			s.mu.Lock()
			s.raw.WriteString(chunk)
			s.mu.Unlock()
			// Feed the emulator up to each query so a cursor report
			// describes the screen as it was when the query was sent.
			scan := tail + chunk
			pos := len(tail)
			for _, m := range terminalQuery.FindAllStringIndex(scan, -1) {
				if m[1] <= pos {
					continue
				}
				if m[1] > pos {
					_, _ = s.scr.WriteString(scan[pos:m[1]])
					pos = m[1]
				}
				s.answer(scan[m[0]:m[1]])
			}
			_, _ = s.scr.WriteString(scan[pos:])
			tail = scan[max(len(scan)-8, 0):]
			tail = tail[max(0, len(tail)-(len(scan)-pos)):]
		}
		if err != nil {
			return
		}
	}
}

func (s *session) answer(query string) {
	switch {
	case strings.HasSuffix(query, "6n"):
		row, col := s.scr.Cursor()
		_, _ = fmt.Fprintf(s.ptmx, "\x1b[%d;%dR", row, col)
	case strings.Contains(query, ">"):
		if s.resizeMode == vtscreen.ResizeBottomAnchored {
			// Termux's real reply: the 320 in the version field is what the
			// console fingerprints.
			_, _ = s.ptmx.WriteString("\x1b[>41;320;0c")
			return
		}
		_, _ = s.ptmx.WriteString("\x1b[>41;380;0c")
	default:
		_, _ = s.ptmx.WriteString("\x1b[?62;22c")
	}
}

func (s *session) stop() {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.ptmx.Close()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
	}
	_ = s.cmd.Wait()
}

// typeLine types text, then presses Enter as a separate keystroke: sent in
// one write, the reader would take the burst for a paste and insert the
// newline instead of submitting.
func (s *session) typeLine(text string) {
	s.t.Helper()
	_, err := s.ptmx.WriteString(text)
	if err != nil {
		s.t.Fatalf("type: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if _, err := s.ptmx.WriteString("\r"); err != nil {
		s.t.Fatalf("enter: %v", err)
	}
}

// waitFor polls the screen until cond holds, failing with the screen and the
// tail of the raw output when it does not within d.
func (s *session) waitFor(what string, d time.Duration, cond func(screen string) bool) {
	s.t.Helper()
	deadline := time.Now().Add(d)
	for {
		if cond(s.scr.Text()) {
			return
		}
		if time.Now().After(deadline) {
			s.mu.Lock()
			raw := s.raw.String()
			s.mu.Unlock()
			s.saveRaw()
			s.t.Fatalf("timed out waiting for %s.\nscreen:\n%s\nscrollback:\n%s\nraw tail: %q",
				what, s.scr.Dump(), strings.Join(s.scr.Scrollback(), "\n"), raw[max(0, len(raw)-1500):])
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// saveRaw writes the raw output to $SPROUT_E2E_RAW_DIR/<test>.raw when set,
// for replaying a failure through the emulator.
func (s *session) saveRaw() {
	dir := os.Getenv("SPROUT_E2E_RAW_DIR")
	if dir == "" {
		return
	}
	s.mu.Lock()
	raw := s.raw.Bytes()
	s.mu.Unlock()
	name := strings.NewReplacer("/", "_").Replace(s.t.Name()) + ".raw"
	_ = os.WriteFile(filepath.Join(dir, name), raw, 0o600)
}

func (s *session) waitForText(text string, d time.Duration) {
	s.t.Helper()
	s.waitFor(fmt.Sprintf("%q on screen", text), d, func(screen string) bool { return strings.Contains(screen, text) })
}

// resize changes the pty size (the program gets SIGWINCH) and the emulator's.
func (s *session) resize(cols, rows int) {
	s.t.Helper()
	s.scr.Resize(cols, rows)
	if err := pty.Setsize(s.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil { //nolint:gosec // G115: test sizes
		s.t.Fatalf("resize: %v", err)
	}
}

// allText is the scrollback plus the visible rows: everything the user could
// read by scrolling up. A row missing from THIS is a row that was destroyed.
func (s *session) allText() string { return s.scr.AllText() }
