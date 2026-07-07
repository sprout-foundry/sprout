//go:build !js

package cmd

// Integration regression tests for cmd/agent_terminal_subscriber.go's
// EventTypeAgentMessage handler. The previous code wrote
// `[⚠️  SECURITY CAUTION] …` directly to os.Stderr via fmt.Fprintf,
// bypassing PrintExternal. When the InputReader was active (between
// turns), the raw bytes landed under the cursor and corrupted the
// in-progress input line; when the SteerInputReader was active (during
// a turn), they corrupted the pinned steer panel.
//
// The contract under test: when the subscriber receives a
// security_caution agent message, it must NOT write raw bytes to
// stderr; it must route through console.PrintExternal so the cursor-
// management path handles them. This is verified by swapping os.Stdout
// and os.Stderr for pipes, publishing a security_caution event, then
// closing the write ends and draining the pipes to inspect what each
// stream received.

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestTerminalSubscriber_AgentMessage_SecurityCaution_NoStderrLeak is
// the regression test for the security-caution input-corruption bug
// the user hit (⚠️ SECURITY CAUTION … landed mid-line and broke the
// input). With the fix the subscriber routes through PrintExternal
// (stdout), so stderr must remain empty and stdout must carry the
// message.
//
// The test starts the subscriber with a real event bus / indicator /
// footer, publishes a security_caution agent message (the same shape
// handleToolError emits when a high-risk command hits the persona
// risk cascade), and asserts the route:
//
//   - stderr is empty (no raw fmt.Fprintf leak)
//   - stdout contains the [⚠️  SECURITY CAUTION] text + the underlying
//     message body
//
// If a future change moves the render back to a direct stderr write,
// stderr will become non-empty and this test will fail loudly.
func TestTerminalSubscriber_AgentMessage_SecurityCaution_NoStderrLeak(t *testing.T) {
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutR, stdoutW, _ := os.Pipe()
	stderrR, stderrW, _ := os.Pipe()
	os.Stdout = stdoutW
	os.Stderr = stderrW
	// Defer restore + close the read ends. Write ends are closed
	// explicitly inside the test body AFTER the subscriber has had a
	// chance to write — closing them is what lets io.Copy terminate
	// with EOF when we drain the pipes below.
	t.Cleanup(func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
		stdoutR.Close()
		stderrR.Close()
	})

	// Stand up the subscriber with minimal plumbing. chatAgent is nil
	// because the EventTypeAgentMessage path doesn't use it. The
	// indicator + footer are backed by a throwaway buffer (they're
	// non-TTY so they no-op, which is fine — we only care about
	// stdout/stderr capture).
	eb := events.NewEventBus()
	var indicatorBuf, footerBuf bytes.Buffer
	indicator := console.NewActivityIndicator(&indicatorBuf)
	footer := console.NewStatusFooter(&footerBuf, nil)

	resetSpawn := startTerminalToolSubscriber(t.Context(), nil, eb, indicator, footer)
	t.Cleanup(resetSpawn)

	// Publish the security_caution event. This is the exact pattern
	// handleToolError emits when a high-risk command hits the persona
	// risk cascade — the message body matches the one the user saw
	// leaking through in the previous run.
	rawMsg := "high-risk operation rejected by persona risk cascade: high (command: 'git checkout HEAD -- pkg/spec/spec_test.go pkg/spec/spec_test.go && wc -l pkg/spec/spec_test.go')"
	eb.Publish(events.EventTypeAgentMessage, events.AgentMessageEvent("security_caution", rawMsg, nil))

	// Give the subscriber goroutine time to consume the event and
	// write. The event channel is unbuffered but the subscriber
	// drains in a tight loop; 100ms is comfortably more than enough.
	time.Sleep(100 * time.Millisecond)

	// Close write ends so io.Copy on the read ends terminates with
	// EOF. Must happen before we drain.
	stdoutW.Close()
	stderrW.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	io.Copy(&stdoutBuf, stdoutR)
	io.Copy(&stderrBuf, stderrR)

	stdoutOutput := stdoutBuf.String()
	stderrOutput := stderrBuf.String()

	// Contract: nothing on stderr. The whole point of the fix is that
	// these messages route through PrintExternal (stdout) so the
	// cursor-management path handles them. A raw fmt.Fprintf to
	// os.Stderr would regress here and trigger the failure.
	if strings.TrimSpace(stderrOutput) != "" {
		t.Errorf("Unexpected stderr output: %q\n— security_caution must route through PrintExternal, not raw fmt.Fprintf to os.Stderr", stderrOutput)
	}

	// Contract: the warning text + underlying message both render on stdout.
	if !strings.Contains(stdoutOutput, "SECURITY CAUTION") {
		t.Errorf("stdout missing SECURITY CAUTION text; got: %q", stdoutOutput)
	}
	if !strings.Contains(stdoutOutput, rawMsg) {
		t.Errorf("stdout missing underlying message body; got: %q", stdoutOutput)
	}
}
