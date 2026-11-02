//go:build !js

package cliui

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/stretchr/testify/require"
)

// renderThroughSubscriber runs publish against a live terminal subscriber,
// then publishes a primary-agent warning as a sentinel and returns
// everything the subscriber wrote up to and including it. Events are
// consumed in order, so once the sentinel has rendered every earlier event
// has been handled.
func renderThroughSubscriber(t *testing.T, publish func(eb *events.EventBus)) string {
	t.Helper()
	oldStdout, oldStderr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	errR, errW, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout, os.Stderr = outW, errW
	t.Cleanup(func() {
		os.Stdout, os.Stderr = oldStdout, oldStderr
		_ = outR.Close()
		_ = errR.Close()
	})

	eb := events.NewEventBus()
	// Tool end lines go out through the indicator's own writer, so it is
	// part of what the user sees.
	indicatorOut := &lockedBuffer{}
	var footerBuf bytes.Buffer
	resetSpawn := StartTerminalToolSubscriber(t.Context(), nil, eb,
		console.NewActivityIndicator(indicatorOut), console.NewStatusFooter(&footerBuf, nil))
	t.Cleanup(resetSpawn)

	publish(eb)
	const sentinel = "sentinel from the primary"
	eb.Publish(events.EventTypeAgentMessage, events.AgentMessageEvent("warning", sentinel, nil))

	// Read stdout as it arrives; the write ends stay open until the
	// sentinel shows up so a late write cannot hit a closed pipe.
	seen := make(chan string, 1)
	go func() {
		var acc strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := outR.Read(buf)
			acc.Write(buf[:n])
			if strings.Contains(acc.String(), sentinel) || err != nil {
				seen <- acc.String()
				return
			}
		}
	}()
	var stdout string
	select {
	case stdout = <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("the sentinel never rendered")
	}
	_ = errW.Close()
	var errOut bytes.Buffer
	_, _ = io.Copy(&errOut, errR)
	return stdout + errOut.String() + indicatorOut.String()
}

// lockedBuffer is a bytes.Buffer safe to write from the subscriber goroutine
// while the test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A /task agent shares the primary's bus. Its events must not reach the
// primary's transcript.
func TestTerminalSubscriber_DropsBackgroundTaskEvents(t *testing.T) {
	out := renderThroughSubscriber(t, func(eb *events.EventBus) {
		eb.Publish(events.EventTypeAgentMessage, events.AgentMessageEvent("warning", "from the task",
			map[string]interface{}{agent.EventMetaBackgroundTaskID: "fix-login", "task_id": "fix-login"}))
	})
	require.NotContains(t, out, "from the task")
}

// Parallel subagents' tool calls feed the fleet status, not the transcript;
// their warnings still print.
func TestTerminalSubscriber_ConcurrentSubagentToolsStayOffTranscript(t *testing.T) {
	out := renderThroughSubscriber(t, func(eb *events.EventBus) {
		meta := map[string]interface{}{
			agent.EventMetaSubagentTaskID:     "auth",
			agent.EventMetaSubagentConcurrent: true,
			"active_persona":                  "coder",
			"subagent_depth":                  1,
		}
		start := map[string]interface{}{"tool_name": "read_file", "tool_call_id": "c1", "arguments": `{"path":"secret_marker.go"}`}
		end := map[string]interface{}{"tool_name": "read_file", "tool_call_id": "c1", "status": "completed"}
		for k, v := range meta {
			start[k], end[k] = v, v
		}
		eb.Publish(events.EventTypeToolStart, start)
		eb.Publish(events.EventTypeToolEnd, end)
		eb.Publish(events.EventTypeAgentMessage, events.AgentMessageEvent("warning", "worker warning", meta))
	})
	require.NotContains(t, out, "secret_marker.go", "a concurrent run's tool line must not print")
	require.Contains(t, out, "worker warning")
}

// A subagent_activity payload names the subagent's run in task_id; that alone
// must not mark it as a background task's.
func TestIsBackgroundTaskEvent_SubagentTaskIDIsNotATask(t *testing.T) {
	require.False(t, isBackgroundTaskEvent(map[string]interface{}{"task_id": "subagent-123"}))
	require.True(t, isBackgroundTaskEvent(map[string]interface{}{agent.EventMetaBackgroundTaskID: "fix-login"}))
	require.False(t, isBackgroundTaskEvent(nil))
}
