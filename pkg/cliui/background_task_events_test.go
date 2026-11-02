//go:build !js

package cliui

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/stretchr/testify/require"
)

// A /task agent shares the primary's bus. Its events must not reach the
// primary's transcript; the primary's own events still do. The primary's
// message is published last, so once it has rendered the task's (earlier)
// event has certainly been consumed.
func TestTerminalSubscriber_DropsBackgroundTaskEvents(t *testing.T) {
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
	var indicatorBuf, footerBuf bytes.Buffer
	resetSpawn := StartTerminalToolSubscriber(t.Context(), nil, eb,
		console.NewActivityIndicator(&indicatorBuf), console.NewStatusFooter(&footerBuf, nil))
	t.Cleanup(resetSpawn)

	eb.Publish(events.EventTypeAgentMessage, events.AgentMessageEvent("warning", "from the task",
		map[string]interface{}{agent.EventMetaBackgroundTaskID: "fix-login", "task_id": "fix-login"}))
	eb.Publish(events.EventTypeAgentMessage, events.AgentMessageEvent("warning", "from the primary", nil))

	// Read stdout as it arrives until the primary's message shows up; the
	// write ends stay open until then so a late write cannot hit a closed
	// pipe.
	seen := make(chan string, 1)
	go func() {
		var acc strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := outR.Read(buf)
			acc.Write(buf[:n])
			if strings.Contains(acc.String(), "from the primary") || err != nil {
				seen <- acc.String()
				return
			}
		}
	}()
	var stdout string
	select {
	case stdout = <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("the primary's message never rendered")
	}
	_ = errW.Close()
	var errOut bytes.Buffer
	_, _ = io.Copy(&errOut, errR)

	all := stdout + errOut.String()
	require.Contains(t, all, "from the primary")
	require.NotContains(t, all, "from the task")
}

// A subagent_activity payload names the subagent's run in task_id; that alone
// must not mark it as a background task's.
func TestIsBackgroundTaskEvent_SubagentTaskIDIsNotATask(t *testing.T) {
	require.False(t, isBackgroundTaskEvent(map[string]interface{}{"task_id": "subagent-123"}))
	require.True(t, isBackgroundTaskEvent(map[string]interface{}{agent.EventMetaBackgroundTaskID: "fix-login"}))
	require.False(t, isBackgroundTaskEvent(nil))
}
