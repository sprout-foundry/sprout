//go:build !js

package cmd

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/stretchr/testify/require"
)

// --- Bus-level unit tests for the progress emitter ---

func TestProgressEmitter_QueryStarted(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer
	target := &buf

	pe := startProgressEmitterDirect(context.Background(), bus, target)
	defer pe.stop()

	bus.Publish(events.EventTypeQueryStarted, events.QueryStartedEvent("hello", "test", "test:test"))
	time.Sleep(200 * time.Millisecond)

	lines := readLines(&buf)
	require.Equal(t, []string{"progress: agent run started"}, lines)
}

func TestProgressEmitter_QueryStarted_DuplicatesSuppressed(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer

	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	// The agent publishes query_started from multiple code paths (CLI
	// ProcessQuery, seed prepare phase, seed rich event publisher).
	for i := 0; i < 3; i++ {
		bus.Publish(events.EventTypeQueryStarted, events.QueryStartedEvent("hello", "test", "test:test"))
	}
	time.Sleep(200 * time.Millisecond)

	lines := readLines(&buf)
	require.Equal(t, []string{"progress: agent run started"}, lines,
		"only one 'started' milestone expected despite duplicate events")
}

func TestProgressEmitter_DedupResetsAfterCompletion(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer

	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	bus.Publish(events.EventTypeQueryStarted, events.QueryStartedEvent("q1", "test", "test:test"))
	bus.Publish(events.EventTypeQueryCompleted, events.QueryCompletedEvent("q1", "done", 10, 0, 100))
	// A subsequent query in the same session must emit its own milestone.
	bus.Publish(events.EventTypeQueryStarted, events.QueryStartedEvent("q2", "test", "test:test"))
	time.Sleep(300 * time.Millisecond)

	lines := readLines(&buf)
	require.Equal(t, []string{
		"progress: agent run started",
		"progress: agent run completed",
		"progress: agent run started",
	}, lines)
}

func TestProgressEmitter_MilestoneAlwaysStartsOwnLine(t *testing.T) {
	// Simulates the container's merged log stream: the assistant's streamed
	// prose (stdout) ends without a trailing newline, then the milestone
	// (stderr) arrives. The milestone must not be glued onto the prose.
	bus := events.NewEventBus()
	var buf bytes.Buffer

	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	// Unterminated prose on the shared stream.
	buf.WriteString("This repository handles pull requests")

	bus.Publish(events.EventTypeQueryCompleted, events.QueryCompletedEvent("q", "done", 10, 0, 100))
	time.Sleep(200 * time.Millisecond)

	raw := buf.String()
	// The prose must be terminated and the milestone must stand on its own
	// line: one newline closes the unterminated prose, the milestone line
	// follows, and its own trailing newline terminates it.
	require.Equal(t,
		"This repository handles pull requests\nprogress: agent run completed\n",
		raw)
}

func TestProgressEmitter_QueryCompleted(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer
	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	bus.Publish(events.EventTypeQueryCompleted, events.QueryCompletedEvent("hello", "bye", 100, 0.01, 500))
	time.Sleep(200 * time.Millisecond)

	lines := readLines(&buf)
	require.Equal(t, []string{"progress: agent run completed"}, lines)
}

func TestProgressEmitter_ToolStart_EditableTools(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer
	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	// edit_file with path
	bus.Publish(events.EventTypeToolStart, events.ToolStartEvent(
		"edit_file", "call1", `{"path":"foo/bar.go","old":"x","new":"y"}`, "", "", false, "", 0))
	time.Sleep(200 * time.Millisecond)

	lines := readLines(&buf)
	require.Len(t, lines, 1)
	require.Equal(t, "progress: tool: edit_file foo/bar.go", lines[0])

	buf.Reset()

	// write_file with path
	bus.Publish(events.EventTypeToolStart, events.ToolStartEvent(
		"write_file", "call2", `{"path":"baz.txt","content":"hi"}`, "", "", false, "", 1))
	time.Sleep(200 * time.Millisecond)

	lines = readLines(&buf)
	require.Len(t, lines, 1)
	require.Equal(t, "progress: tool: write_file baz.txt", lines[0])

	buf.Reset()

	// read_file with path
	bus.Publish(events.EventTypeToolStart, events.ToolStartEvent(
		"read_file", "call3", `{"path":"README.md","view_range":[1,10]}`, "", "", false, "", 2))
	time.Sleep(200 * time.Millisecond)

	lines = readLines(&buf)
	require.Len(t, lines, 1)
	require.Equal(t, "progress: tool: read_file README.md", lines[0])
}

func TestProgressEmitter_ToolStart_Shell(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer
	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	bus.Publish(events.EventTypeToolStart, events.ToolStartEvent(
		"shell_command", "call1", `{"command":"ls -la /tmp","background":false}`, "", "", false, "", 0))
	time.Sleep(200 * time.Millisecond)

	lines := readLines(&buf)
	require.Len(t, lines, 1)
	require.Equal(t, "progress: tool: shell_command ls -la /tmp", lines[0])

	buf.Reset()

	bus.Publish(events.EventTypeToolStart, events.ToolStartEvent(
		"shell", "call2", `{"command":"echo hello"}`, "", "", false, "", 1))
	time.Sleep(200 * time.Millisecond)

	lines = readLines(&buf)
	require.Len(t, lines, 1)
	require.Equal(t, "progress: tool: shell echo hello", lines[0])
}

func TestProgressEmitter_ToolStart_Search(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer
	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	bus.Publish(events.EventTypeToolStart, events.ToolStartEvent(
		"search", "call1", `{"query":"func main"}`, "", "", false, "", 0))
	time.Sleep(200 * time.Millisecond)

	lines := readLines(&buf)
	require.Len(t, lines, 1)
	require.Equal(t, "progress: tool: search func main", lines[0])

	buf.Reset()

	bus.Publish(events.EventTypeToolStart, events.ToolStartEvent(
		"search_files", "call2", `{"search_pattern":"*.go"}`, "", "", false, "", 1))
	time.Sleep(200 * time.Millisecond)

	lines = readLines(&buf)
	require.Len(t, lines, 1)
	require.Equal(t, "progress: tool: search_files *.go", lines[0])
}

func TestProgressEmitter_ToolStart_UnknownTool(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer
	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	bus.Publish(events.EventTypeToolStart, events.ToolStartEvent(
		"run_subagent", "call1", `{"prompt":"do something","persona":"coder"}`, "", "", false, "", 0))
	time.Sleep(200 * time.Millisecond)

	lines := readLines(&buf)
	require.Len(t, lines, 1)
	// Unknown tool: no preview, just tool name
	require.Equal(t, "progress: tool: run_subagent", lines[0])
}

func TestProgressEmitter_ToolStart_NoArgs(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer
	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	bus.Publish(events.EventTypeToolStart, events.ToolStartEvent(
		"edit_file", "call1", "", "", "", false, "", 0))
	time.Sleep(200 * time.Millisecond)

	lines := readLines(&buf)
	require.Len(t, lines, 1)
	require.Equal(t, "progress: tool: edit_file", lines[0])
}

func TestProgressEmitter_ToolStart_120CharTruncation(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer
	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	// Path that is 150 chars long
	longPath := strings.Repeat("a", 150)
	bus.Publish(events.EventTypeToolStart, events.ToolStartEvent(
		"read_file", "call1", `{"path":"`+longPath+`"}`, "", "", false, "", 0))
	time.Sleep(200 * time.Millisecond)

	lines := readLines(&buf)
	require.Len(t, lines, 1)
	// "progress: tool: read_file " (26 chars) + 120 chars of path = 146 chars total
	require.Equal(t, 26+120, len(lines[0]), "line should be 26 prefix + 120 path = 146 chars")
	require.Equal(t, "progress: tool: read_file "+strings.Repeat("a", 120), lines[0])
}

func TestProgressEmitter_ToolStart_FirstLineOnly(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer
	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	bus.Publish(events.EventTypeToolStart, events.ToolStartEvent(
		"shell_command", "call1", `{"command":"echo hello\necho world"}`, "", "", false, "", 0))
	time.Sleep(200 * time.Millisecond)

	lines := readLines(&buf)
	require.Len(t, lines, 1)
	require.Equal(t, "progress: tool: shell_command echo hello", lines[0])
}

func TestProgressEmitter_Error(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer
	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	bus.Publish(events.EventTypeError, events.ErrorEvent("something broke", nil))
	time.Sleep(200 * time.Millisecond)

	lines := readLines(&buf)
	require.Len(t, lines, 1)
	require.Equal(t, "progress: error: something broke", lines[0])
}

func TestProgressEmitter_Error_200CharTruncation(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer
	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	longMsg := strings.Repeat("x", 250)
	bus.Publish(events.EventTypeError, events.ErrorEvent(longMsg, nil))
	time.Sleep(200 * time.Millisecond)

	lines := readLines(&buf)
	require.Len(t, lines, 1)
	// "progress: error: " (17 chars) + 200 chars = 217 chars total
	require.Equal(t, 17+200, len(lines[0]))
	require.Equal(t, "progress: error: "+strings.Repeat("x", 200), lines[0])
}

func TestProgressEmitter_Error_Unknown(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer
	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	bus.Publish(events.EventTypeError, map[string]interface{}{})
	time.Sleep(200 * time.Millisecond)

	lines := readLines(&buf)
	require.Len(t, lines, 1)
	require.Equal(t, "progress: error: unknown error", lines[0])
}

func TestProgressEmitter_IgnoresOtherEvents(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer
	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	bus.Publish(events.EventTypeStreamChunk, map[string]interface{}{"chunk": "hello"})
	bus.Publish(events.EventTypeMetricsUpdate, map[string]interface{}{"tokens": 100})
	bus.Publish(events.EventTypeTodoUpdate, []map[string]interface{}{})
	time.Sleep(200 * time.Millisecond)

	lines := readLines(&buf)
	require.Empty(t, lines, "should produce no output for non-progress events")
}

func TestProgressEmitter_FullSequence(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer
	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	bus.Publish(events.EventTypeQueryStarted, events.QueryStartedEvent("do stuff", "test", "test:test"))
	bus.Publish(events.EventTypeToolStart, events.ToolStartEvent(
		"edit_file", "c1", `{"path":"main.go","old":"a","new":"b"}`, "", "", false, "", 0))
	bus.Publish(events.EventTypeToolStart, events.ToolStartEvent(
		"shell_command", "c2", `{"command":"go build ./..."}`, "", "", false, "", 1))
	bus.Publish(events.EventTypeQueryCompleted, events.QueryCompletedEvent("do stuff", "done", 50, 0.01, 1000))
	time.Sleep(200 * time.Millisecond)

	lines := readLines(&buf)
	require.Equal(t, []string{
		"progress: agent run started",
		"progress: tool: edit_file main.go",
		"progress: tool: shell_command go build ./...",
		"progress: agent run completed",
	}, lines)
}

func TestProgressEmitter_ControlCharsStripped(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer
	pe := startProgressEmitterDirect(context.Background(), bus, &buf)
	defer pe.stop()

	// Include actual control chars (0x01, 0x02, 0x7F) in the path,
	// JSON-escaped so the payload is valid JSON. After unmarshal the
	// decoded string contains the raw control chars, which the emitter
	// must strip.
	args := `{"path":"foo\u0001bar\u0002baz\u007fqux"}`
	bus.Publish(events.EventTypeToolStart, events.ToolStartEvent(
		"read_file", "c1", args, "", "", false, "", 0))
	time.Sleep(200 * time.Millisecond)

	lines := readLines(&buf)
	require.Len(t, lines, 1)
	require.Equal(t, "progress: tool: read_file foobarbazqux", lines[0])
}

// --- Flag-off test ---

func TestProgressEvents_FlagOff_NoOutput(t *testing.T) {
	// Save and restore the flag value
	saved := progressEventsTarget
	defer func() { progressEventsTarget = saved }()

	progressEventsTarget = ""

	bus := events.NewEventBus()
	pe := startProgressEmitter(context.Background(), bus)
	require.Nil(t, pe, "with flag unset, startProgressEmitter must return nil")

	// Even if events are published, nothing should be emitted anywhere.
	bus.Publish(events.EventTypeQueryStarted, events.QueryStartedEvent("hello", "test", "test:test"))
	bus.Publish(events.EventTypeToolStart, events.ToolStartEvent(
		"edit_file", "c1", `{"path":"x.go"}`, "", "", false, "", 0))
	bus.Publish(events.EventTypeQueryCompleted, events.QueryCompletedEvent("hello", "bye", 10, 0, 100))
	time.Sleep(200 * time.Millisecond)
}

// --- File target test ---

func TestProgressEmitter_FileTarget(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/progress.log"

	bus := events.NewEventBus()
	pe := startProgressEmitterToFile(context.Background(), bus, path)
	defer pe.stop()

	bus.Publish(events.EventTypeQueryStarted, events.QueryStartedEvent("hello", "test", "test:test"))
	bus.Publish(events.EventTypeQueryCompleted, events.QueryCompletedEvent("hello", "bye", 10, 0, 100))
	time.Sleep(200 * time.Millisecond)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := readLinesFromData(data)
	require.Equal(t, []string{
		"progress: agent run started",
		"progress: agent run completed",
	}, lines)
}

func TestProgressEmitter_FileTarget_BadPath_Warns(t *testing.T) {
	saved := progressEventsTarget
	defer func() { progressEventsTarget = saved }()

	progressEventsTarget = "/nonexistent/dir/file.log"

	bus := events.NewEventBus()
	pe := startProgressEmitter(context.Background(), bus)
	require.Nil(t, pe, "should return nil when file cannot be opened")
}

// --- E2E-ish test with newTestAgent ---

func TestProgressEvents_E2E_DirectMode(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/progress.log"

	t.Setenv("SPROUT_CONFIG", dir)

	a, err := agent.NewAgentWithModel("test:test")
	require.NoError(t, err)
	defer a.Shutdown()

	bus := events.NewEventBus()
	a.SetEventBus(bus)
	SetupAgentEvents(a, bus, nil)

	// Use a file target for this test
	saved := progressEventsTarget
	defer func() { progressEventsTarget = saved }()
	progressEventsTarget = path

	pe := startProgressEmitter(context.Background(), bus)
	defer pe.stop()

	// ProcessQuery publishes QueryStarted, runs, then QueryCompleted
	// For the test agent with "test:test" model, ProcessQuery will
	// fail fast (no real LLM), but the events should still be published.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ProcessQuery(ctx, a, bus, "hello world")

	time.Sleep(500 * time.Millisecond)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	// At minimum we should see the started event
	require.True(t, len(lines) >= 1, "should have at least one progress line, got: %v", lines)
	require.Equal(t, "progress: agent run started", lines[0])

	// QueryCompleted or error should also appear
	foundCompleted := false
	foundError := false
	for _, l := range lines {
		if l == "progress: agent run completed" {
			foundCompleted = true
		}
		if strings.HasPrefix(l, "progress: error:") {
			foundError = true
		}
	}
	require.True(t, foundCompleted || foundError,
		"should have either 'completed' or 'error' line, got: %v", lines)
}

// --- Helper functions ---

// startProgressEmitterDirect creates a progressEmitter writing to the given
// io.Writer, bypassing the flag-based target resolution. Used by tests.
func startProgressEmitterDirect(ctx context.Context, bus *events.EventBus, target io.Writer) *progressEmitter {
	w := &nopCloser{target}
	subName := "progress-events-test"
	ch := bus.Subscribe(subName)

	pe := &progressEmitter{writer: w, bus: bus, subName: subName}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				pe.handleEvent(evt)
			}
		}
	}()

	return pe
}

// startProgressEmitterToFile creates a progressEmitter writing to a file path.
func startProgressEmitterToFile(ctx context.Context, bus *events.EventBus, path string) *progressEmitter {
	f, err := os.Create(path)
	if err != nil {
		panic(err) // tests should never hit this
	}
	subName := "progress-events-file-test"
	ch := bus.Subscribe(subName)

	pe := &progressEmitter{writer: f, bus: bus, subName: subName}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				pe.handleEvent(evt)
			}
		}
	}()

	return pe
}

// readLines reads all non-blank lines from a bytes.Buffer. Blank lines are
// skipped because every milestone is prefixed with a newline (see
// progressEmitter.handleEvent), which adds a leading blank line to the raw
// output; the milestone text itself is what the tests assert on.
func readLines(buf *bytes.Buffer) []string {
	return readLinesFromData(buf.Bytes())
}

// readLinesFromData is readLines over a byte slice.
func readLinesFromData(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	var lines []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		if s := scanner.Text(); s != "" {
			lines = append(lines, s)
		}
	}
	return lines
}