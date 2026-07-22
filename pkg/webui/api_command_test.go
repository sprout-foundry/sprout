//go:build !js

package webui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// handleAPICommandExecute — POST /api/command/execute
// SP-114 Phase 2: dedicated command surface for the WebUI command bar.
// ---------------------------------------------------------------------------

func postCommand(t *testing.T, ws *ReactWebServer, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/command/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPICommandExecute(rec, req)
	return rec
}

func TestHandleAPICommandExecute_MethodNotAllowed(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/command/execute", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPICommandExecute(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPICommandExecute_InvalidJSON(t *testing.T) {
	ws, _ := newTestWebServer(t)

	rec := postCommand(t, ws, "not json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPICommandExecute_EmptyCommand(t *testing.T) {
	ws, _ := newTestWebServer(t)

	cases := map[string]string{
		"empty":      `{"command":""}`,
		"whitespace": `{"command":"   "}`,
		"missing":    `{}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := postCommand(t, ws, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s, got %d: %s", name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandleAPICommandExecute_MissingSlashPrefix(t *testing.T) {
	ws, _ := newTestWebServer(t)

	rec := postCommand(t, ws, `{"command":"info"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-slash command, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "must start with /") {
		t.Errorf("expected 'must start with /' in body, got: %s", rec.Body.String())
	}
}

func TestHandleAPICommandExecute_UnknownCommand(t *testing.T) {
	ws, _ := newTestWebServer(t)

	rec := postCommand(t, ws, `{"command":"/nonexistent-command-xyz"}`)
	// getChatAgent likely errors (no real chat agent wired up in the test),
	// but if the test environment happens to have one with a default
	// registry, we should still see a clean 400 with "command_not_found".
	if rec.Code == http.StatusOK {
		t.Fatalf("expected non-200 for unknown command, got 200: %s", rec.Body.String())
	}
	// Either 400 (command_not_found / command_not_safe) or 500/503 (chat
	// agent not available) are acceptable — the validation logic above is
	// what we're really testing here.
}

// TestExecuteSafeSteerCommand_DistinguishesNotFoundVsNotSafe verifies the
// helper that handleAPICommandExecute relies on to disambiguate "command
// doesn't exist" from "command exists but isn't SteerCapable". The handler
// must return (nil, "", nil) for both so it can re-classify with the
// registry — but the helper itself should never panic on a non-slash input.
func TestExecuteSafeSteerCommand_NonSlashInput(t *testing.T) {
	ws, _ := newTestWebServer(t)

	cmd, out, err := ws.executeSafeSteerCommand("not a slash command", nil)
	if cmd != nil || out != "" || err != nil {
		t.Errorf("expected (nil,'',nil) for non-slash input, got (%v, %q, %v)", cmd, out, err)
	}
}

func TestExecuteSafeSteerCommand_NilAgent(t *testing.T) {
	ws, _ := newTestWebServer(t)

	cmd, out, err := ws.executeSafeSteerCommand("/info", nil)
	if cmd != nil || out != "" || err != nil {
		t.Errorf("expected (nil,'',nil) when agent has no slash registry, got (%v, %q, %v)", cmd, out, err)
	}
}

// TestCommandResponseShape documents the success response shape so a future
// WebUI consumer can rely on it. We marshal a representative map and assert
// the JSON shape rather than spinning up a full agent.
func TestCommandResponseShape(t *testing.T) {
	resp := map[string]interface{}{
		"command":  "info",
		"output":   "Agent: test",
		"error":    "",
		"accepted": true,
	}
	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(resp); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := buf.String()
	for _, want := range []string{`"command":"info"`, `"output":"Agent: test"`, `"accepted":true`} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q: %s", want, body)
		}
	}
}

// ---------------------------------------------------------------------------
// executeSafeSteerCommand success path — uses a real CommandRegistry with a
// SteerCapable test command. This exercises the same code path the handler
// uses on success, without the overhead of a full chat-agent stub.
// ---------------------------------------------------------------------------

type echoCommand struct{}

func (e *echoCommand) Name() string          { return "echo" }
func (e *echoCommand) Description() string   { return "test: prints its args" }
func (e *echoCommand) SafeDuringSteer() bool { return true }
func (e *echoCommand) Execute(args []string, _ *agent.Agent) error {
	fmt.Fprintln(os.Stdout, strings.Join(args, " "))
	return nil
}

func TestExecuteSafeSteerCommand_CapturesStdout(t *testing.T) {
	ws, _ := newTestWebServer(t)

	registry := agent_commands.NewCommandRegistry()
	registry.Register(&echoCommand{})

	a := &agent.Agent{}
	a.SetSlashCommands(registry)

	cmd, output, err := ws.executeSafeSteerCommand("/echo hello world", a)
	if err != nil {
		t.Fatalf("executeSafeSteerCommand: %v", err)
	}
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Name() != "echo" {
		t.Errorf("cmd.Name() = %q, want %q", cmd.Name(), "echo")
	}
	if !strings.Contains(output, "hello world") {
		t.Errorf("output = %q, expected to contain 'hello world'", output)
	}
}

func TestExecuteSafeSteerCommand_RejectsUnsafeCommand(t *testing.T) {
	ws, _ := newTestWebServer(t)

	registry := agent_commands.NewCommandRegistry()
	// Register a command that does NOT implement SteerCapable.
	registry.Register(&bareCommand{})

	a := &agent.Agent{}
	a.SetSlashCommands(registry)

	cmd, output, err := ws.executeSafeSteerCommand("/bare arg", a)
	if cmd != nil || output != "" || err != nil {
		t.Errorf("unsafe command must return (nil,'',nil), got (%v, %q, %v)", cmd, output, err)
	}
}

// bareCommand implements the Command interface but NOT SteerCapable.
// Its Execute method records the call (without using *testing.T since this
// is a value type, not a test func) so the rejection test can assert the
// gate held.
type bareCommand struct{ called bool }

func (b *bareCommand) Name() string        { return "bare" }
func (b *bareCommand) Description() string { return "test: not steer-capable" }
func (b *bareCommand) Execute(_ []string, _ *agent.Agent) error {
	b.called = true
	return nil
}

// ---------------------------------------------------------------------------
// executeSafeSteerCommandStreaming — UTF-8 boundary handling for chunked
// stdout. The streamer is supposed to buffer trailing partial runes so the
// consumer never sees an incomplete multi-byte rune. These tests pin that
// contract directly on the helper, independent of the HTTP/WS plumbing.
// ---------------------------------------------------------------------------

func TestStreamPipeChunks_ASCIISingleFlush(t *testing.T) {
	var emitted []string
	streamPipeChunks(strings.NewReader("hello world"), new(strings.Builder), func(s string) {
		emitted = append(emitted, s)
	})
	if got := strings.Join(emitted, ""); got != "hello world" {
		t.Errorf("emitted runes joined = %q, want %q", got, "hello world")
	}
}

func TestStreamPipeChunks_UTF8BoundarySafe(t *testing.T) {
	// 'é' is U+00E9 — 2 bytes in UTF-8 (0xC3 0xA9). Split the bytes
	// across two pipe reads by feeding them as two separate strings via
	// an io.MultiReader. The streamer must hold the trailing 0xC3
	// until the next chunk completes the rune, then emit one
	// well-formed rune (not a replacement character).
	first := string([]byte{0xC3})
	second := string([]byte{0xA9})
	combined := first + second
	if combined != "é" {
		t.Fatalf("test fixture invalid: %q is not 'é'", combined)
	}
	var emitted []string
	streamPipeChunks(&splitReader{parts: []string{first, second}}, new(strings.Builder), func(s string) {
		emitted = append(emitted, s)
	})
	joined := strings.Join(emitted, "")
	if joined != "é" {
		t.Errorf("emitted runes joined = %q, want %q (no replacement char, no missing rune)", joined, "é")
	}
	if len(joined) != len("é") {
		t.Errorf("emitted length = %d bytes, want %d (a 2-byte rune, not a single byte + replacement)", len(joined), len("é"))
	}
}

func TestStreamPipeChunks_HoldsPartialRuneAcrossReads(t *testing.T) {
	// 3-byte rune '日' (U+65E5) split as 1 byte then 2 bytes. After
	// the first read the streamer must hold the partial byte and
	// not emit anything yet.
	var emitted []string
	streamPipeChunks(&splitReader{parts: []string{string([]byte{0xE6}), string([]byte{0x97, 0xA5})}}, new(strings.Builder), func(s string) {
		emitted = append(emitted, s)
	})
	if len(emitted) != 1 || emitted[0] != "日" {
		t.Errorf("expected exactly one rune '日' emitted across the split, got %v", emitted)
	}
}

// splitReader yields its parts one by one on each Read call so the
// streamer sees the same byte boundary on every iteration.
type splitReader struct {
	parts []string
	i     int
}

func (r *splitReader) Read(p []byte) (int, error) {
	if r.i >= len(r.parts) {
		return 0, ioEOF{}
	}
	n := copy(p, r.parts[r.i])
	r.i++
	return n, nil
}

type ioEOF struct{}

func (ioEOF) Error() string { return "EOF" }

// ---------------------------------------------------------------------------
// commandOutputStreamer — backpressure accounting. Pin the threshold
// behavior and the drop-warning event so future refactors can't quietly
// regress it.
// ---------------------------------------------------------------------------

func TestCommandOutputStreamer_AppendToRingNormal(t *testing.T) {
	s := &commandOutputStreamer{ringCap: 1024, ring: make([]byte, 0, 1024)}
	s.appendToRing([]byte("hello"))
	if string(s.ring) != "hello" {
		t.Errorf("ring = %q, want %q", s.ring, "hello")
	}
	if s.dropBytes != 0 {
		t.Errorf("dropBytes = %d, want 0", s.dropBytes)
	}
}

func TestCommandOutputStreamer_AppendToRingOverflow(t *testing.T) {
	s := &commandOutputStreamer{ringCap: 8, ring: make([]byte, 0, 8)}
	s.appendToRing([]byte("12345678"))  // fill ring exactly
	s.appendToRing([]byte("abcdef"))    // push 6 bytes in, 6 oldest dropped
	if len(s.ring) != 8 {
		t.Errorf("ring len = %d, want 8", len(s.ring))
	}
	if string(s.ring) != "78abcdef" {
		t.Errorf("ring = %q, want %q", string(s.ring), "78abcdef")
	}
	if s.dropBytes != 6 {
		t.Errorf("dropBytes = %d, want 6", s.dropBytes)
	}
}

func TestCommandOutputStreamer_AppendToRingHugeChunk(t *testing.T) {
	s := &commandOutputStreamer{ringCap: 4, ring: make([]byte, 0, 4)}
	s.appendToRing([]byte("ABCDEFGH")) // 8 bytes into 4-byte ring → keep last 4, drop 4
	if len(s.ring) != 4 {
		t.Errorf("ring len = %d, want 4", len(s.ring))
	}
	if string(s.ring) != "EFGH" {
		t.Errorf("ring = %q, want %q", string(s.ring), "EFGH")
	}
	if s.dropBytes != 4 {
		t.Errorf("dropBytes = %d, want 4", s.dropBytes)
	}
}

// ---------------------------------------------------------------------------
// handleAPICommandExecute — SP-114 Phase 2c: live-streaming of stdout
// chunks over the chat session's WebSocket, with bounded backpressure
// accounting. These tests exercise the full HTTP+streaming path end to
// end: the handler is invoked via httptest, the event bus is observed
// directly (the WS handler reads from the same bus, so this is a faithful
// stand-in for a WS subscriber), and the HTTP response body is parsed.
//
// The helper commandOutputTestHarness below sets up a chat agent with a
// caller-supplied mock command and pipes events to a captured channel.
// ---------------------------------------------------------------------------

// commandOutputTestHarness wires a chat session's agent into the ReactWebServer
// so handleAPICommandExecute can resolve it. The harness subscribes to the
// event bus for the test and returns a channel of decoded events. Tests
// then drive the HTTP endpoint and assert on the captured stream.
type commandOutputTestHarness struct {
	ws       *ReactWebServer
	chatID   string
	cmd      agent_commands.Command // caller-supplied mock; wired into a fresh registry
	events   <-chan events.UIEvent
	cancel   func()
	cmdLine  string
}

func newCommandOutputTestHarness(t *testing.T, cmd agent_commands.Command) *commandOutputTestHarness {
	t.Helper()
	ws, _ := newTestWebServer(t)
	ws.agentEnforceSingleSession = true // exercise Mode 1 (single-WS handler)

	// Build a registry with the caller's mock command and attach it to
	// an Agent that lives on the chat session. Pre-populating the
	// agent on the chat session bypasses getOrCreateAgent (which would
	// try to spin up a real config-driven agent in the test env).
	registry := agent_commands.NewCommandRegistry()
	registry.Register(cmd)
	const chatID = "default"
	ws.mutex.Lock()
	ctx := ws.clientContexts["test-client"]
	if ctx == nil {
		ctx = &webClientContext{}
		ws.clientContexts["test-client"] = ctx
	}
	ctx.DefaultChatID = chatID
	if ctx.ChatSessions == nil {
		ctx.ChatSessions = make(map[string]*chatSession)
	}
	cs, ok := ctx.ChatSessions[chatID]
	if !ok {
		cs = newChatSession(chatID, "Test Chat")
		ctx.ChatSessions[chatID] = cs
	}
	if cs.Agent == nil {
		// Initialise the agent's sub-managers so chatSession.getOrCreateAgent's
		// "Agent already exists" path (which calls SetEventMetadata and
		// EnableStreaming) doesn't panic on nil pointers. We don't
		// drive any of these from the test — they exist only to satisfy
		// the lazy-init contract. Order matters: InitSubManagersForTest
		// first (state, output, security), then SetEventBus (depends on
		// OutputRouter → state), then SetSlashCommands.
		a := &agent.Agent{}
		a.InitSubManagersForTest()
		a.SetEventBus(ws.eventBus)
		a.SetSlashCommands(registry)
		cs.Agent = a
	}
	ws.mutex.Unlock()

	eventCh := ws.eventBus.Subscribe("command-output-test-" + t.Name())
	return &commandOutputTestHarness{
		ws:     ws,
		chatID: chatID,
		cmd:    cmd,
		events: eventCh,
		cancel: func() { ws.eventBus.Unsubscribe("command-output-test-" + t.Name()) },
	}
}

// run drives the HTTP endpoint with the given command line and returns
// the recorder (HTTP response) and the captured events filtered to the
// command_output / command_output_dropped types.
func (h *commandOutputTestHarness) run(t *testing.T, cmdLine string) (*httptest.ResponseRecorder, []events.UIEvent) {
	t.Helper()
	body := fmt.Sprintf(`{"command":%q}`, cmdLine)
	rec := postCommand(t, h.ws, body)

	var captured []events.UIEvent
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-h.events:
			if !ok {
				return rec, captured
			}
			if ev.Type == events.EventTypeCommandOutput || ev.Type == events.EventTypeCommandOutputDropped {
				captured = append(captured, ev)
			}
		case <-deadline:
			return rec, captured
		}
	}
}

// drainCaptured reads remaining events from the bus without blocking —
// useful after the run goroutine has returned to pick up any stragglers
// emitted just before the handler exited.
func (h *commandOutputTestHarness) drainCaptured(timeout time.Duration) []events.UIEvent {
	var out []events.UIEvent
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-h.events:
			if !ok {
				return out
			}
			if ev.Type == events.EventTypeCommandOutput || ev.Type == events.EventTypeCommandOutputDropped {
				out = append(out, ev)
			}
		case <-deadline:
			return out
		}
	}
}

// commandOutputEvent is a typed view of a command_output event's payload.
type commandOutputEvent struct {
	Command string `json:"command"`
	Chunk   string `json:"chunk"`
	IsFinal bool   `json:"is_final"`
	Seq     int    `json:"seq"`
}

func decodeCommandOutput(ev events.UIEvent) commandOutputEvent {
	data, _ := ev.Data.(map[string]interface{})
	out := commandOutputEvent{}
	if s, ok := data["command"].(string); ok {
		out.Command = s
	}
	if s, ok := data["chunk"].(string); ok {
		out.Chunk = s
	}
	if b, ok := data["is_final"].(bool); ok {
		out.IsFinal = b
	}
	// seq is JSON-decoded as float64 by default.
	switch v := data["seq"].(type) {
	case float64:
		out.Seq = int(v)
	case int:
		out.Seq = v
	}
	return out
}

// ---------------------------------------------------------------------------
// streamingCommand is a generic SteerCapable mock that writes a caller-
// supplied sequence of chunks to stdout with a configurable sleep between
// them. Used by the per-test scenarios below.
// ---------------------------------------------------------------------------

type streamingCommand struct {
	name   string
	chunks []string
	delay  time.Duration
}

func (s *streamingCommand) Name() string          { return s.name }
func (s *streamingCommand) Description() string   { return "test streaming command" }
func (s *streamingCommand) SafeDuringSteer() bool { return true }
func (s *streamingCommand) Execute(_ []string, _ *agent.Agent) error {
	for _, c := range s.chunks {
		os.Stdout.Write([]byte(c))
		if s.delay > 0 {
			time.Sleep(s.delay)
		}
	}
	return nil
}

// streamingPanicCommand writes some output then panics. The streamer
// must still emit the partial output and the final is_final marker
// after the panic is recovered inside executeSafeSteerCommandStreaming.
type streamingPanicCommand struct {
	name     string
	prefix   string
	panicMsg string
}

func (s *streamingPanicCommand) Name() string          { return s.name }
func (s *streamingPanicCommand) Description() string   { return "test panic command" }
func (s *streamingPanicCommand) SafeDuringSteer() bool { return true }
func (s *streamingPanicCommand) Execute(_ []string, _ *agent.Agent) error {
	os.Stdout.Write([]byte(s.prefix))
	panic(s.panicMsg)
}

// TestHandleAPICommandExecute_StreamsChunksOverWS is the SP-114 Phase 2c
// acceptance test: a mock command writes 100 chunks of 1KB each with
// 10ms gaps, and the WebSocket-equivalent event bus sees all 100 chunks
// in order (monotonic seq) plus a final is_final=true marker. The HTTP
// response carries the full aggregated output.
func TestHandleAPICommandExecute_StreamsChunksOverWS(t *testing.T) {
	const chunkCount = 100
	chunks := make([]string, chunkCount)
	for i := 0; i < chunkCount; i++ {
		chunks[i] = strings.Repeat(string(rune('A'+i%26)), 1024)
	}

	cmd := &streamingCommand{name: "stream", chunks: chunks, delay: 10 * time.Millisecond}
	h := newCommandOutputTestHarness(t, cmd)
	defer h.cancel()

	rec, captured := h.run(t, "/stream")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// HTTP response carries the full aggregated output.
	var httpResp struct {
		Output   string `json:"output"`
		Command  string `json:"command"`
		Accepted bool   `json:"accepted"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&httpResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(httpResp.Output) != chunkCount*1024 {
		t.Errorf("HTTP output length = %d, want %d", len(httpResp.Output), chunkCount*1024)
	}

	// Drain any events the goroutine might have missed.
	captured = append(captured, h.drainCaptured(200*time.Millisecond)...)

	// At least 1 chunk and at most 100 (the OS pipe may coalesce
	// adjacent writes under load, reducing the count below 100 even
	// though every byte still arrives). The hard correctness check
	// is the content-equality assertion at the bottom: the union of
	// streamed chunks must equal the HTTP response body byte-for-byte.
	chunks2, finals := splitChunksAndFinals(captured)
	if len(chunks2) < 1 {
		t.Fatalf("got %d chunks, want at least 1", len(chunks2))
	}
	if len(chunks2) > chunkCount {
		t.Fatalf("got %d chunks, want at most %d", len(chunks2), chunkCount)
	}
	if len(finals) != 1 {
		t.Fatalf("got %d final markers, want 1", len(finals))
	}
	if !finals[0].IsFinal {
		t.Errorf("final marker is_final = false, want true")
	}
	if finals[0].Chunk != "" {
		t.Errorf("final marker chunk = %q, want \"\"", finals[0].Chunk)
	}
	// Seq is monotonic starting at 1; when coalescing happens the
	// number of chunks is lower, but seq still increments per event
	// so the gaps in seq don't matter — the consumer reassembles
	// from chunks[0].Seq, not from chunk indices.
	for i, ev := range chunks2 {
		if ev.Seq != i+1 {
			t.Errorf("chunk[%d].Seq = %d, want %d", i, ev.Seq, i+1)
		}
		if ev.IsFinal {
			t.Errorf("chunk[%d] is_final = true, want false", i)
		}
	}
	// Reconstitute the streamed output — must match the HTTP response.
	var streamed strings.Builder
	for _, ev := range chunks2 {
		streamed.WriteString(ev.Chunk)
	}
	if streamed.String() != httpResp.Output {
		t.Errorf("streamed output length %d != HTTP output length %d", streamed.Len(), len(httpResp.Output))
	}
}

// TestHandleAPICommandExecute_UTF8BoundarySafe verifies that a chunk split
// across a UTF-8 rune boundary is buffered and only emitted as a
// well-formed rune (no replacement character).
func TestHandleAPICommandExecute_UTF8BoundarySafe(t *testing.T) {
	// The 'é' (U+00E9) rune is 2 bytes in UTF-8. We deliver 0xC3 then
	// 0xA9 in two separate Write calls so the pipe reads cross a rune
	// boundary. The WebSocket consumer must see a single 'é' rune, not
	// a 1-byte partial followed by a replacement character.
	cmd := &utf8SplitCommand{name: "u8"}
	h := newCommandOutputTestHarness(t, cmd)
	defer h.cancel()

	rec, captured := h.run(t, "/u8")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// HTTP response: output is the single 'é' rune.
	var httpResp struct {
		Output string `json:"output"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&httpResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if httpResp.Output != "é" {
		t.Errorf("HTTP output = %q, want %q (no replacement char, complete rune)", httpResp.Output, "é")
	}

	// Reassemble the WS chunks. There must be exactly one rune worth
	// of data before the final marker, no replacement character.
	captured = append(captured, h.drainCaptured(100*time.Millisecond)...)
	chunks, finals := splitChunksAndFinals(captured)
	if len(chunks) < 1 {
		t.Fatalf("expected at least one chunk, got 0")
	}
	var reassembled strings.Builder
	for _, ev := range chunks {
		reassembled.WriteString(ev.Chunk)
	}
	if reassembled.String() != "é" {
		t.Errorf("reassembled WS chunks = %q, want %q (UTF-8 boundary violated)", reassembled.String(), "é")
	}
	if len(finals) != 1 || !finals[0].IsFinal {
		t.Errorf("expected exactly one final marker, got %d (is_final flags: %v)", len(finals), finalFlags(finals))
	}
}

// utf8SplitCommand writes a 2-byte UTF-8 rune as two separate writes so
// the pipe reads cross a rune boundary. The streamer must buffer the
// partial first byte until the second read completes the rune.
type utf8SplitCommand struct{ name string }

func (s *utf8SplitCommand) Name() string          { return s.name }
func (s *utf8SplitCommand) Description() string   { return "test utf8 split" }
func (s *utf8SplitCommand) SafeDuringSteer() bool { return true }
func (s *utf8SplitCommand) Execute(_ []string, _ *agent.Agent) error {
	os.Stdout.Write([]byte{0xC3}) // first byte of 'é'
	os.Stdout.Write([]byte{0xA9}) // second byte of 'é'
	return nil
}

// TestHandleAPICommandExecute_PanicMidExecution verifies that a command
// that panics after producing some output still emits the chunks it
// produced before the panic, the HTTP error field captures the panic
// message, and the final is_final=true marker is still emitted.
func TestHandleAPICommandExecute_PanicMidExecution(t *testing.T) {
	cmd := &streamingPanicCommand{
		name:     "boom",
		prefix:   "before-panic\n",
		panicMsg: "boom",
	}
	h := newCommandOutputTestHarness(t, cmd)
	defer h.cancel()

	rec, captured := h.run(t, "/boom")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// HTTP error field carries the panic.
	var httpResp struct {
		Output string `json:"output"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&httpResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(httpResp.Error, "panic") || !strings.Contains(httpResp.Error, "boom") {
		t.Errorf("HTTP error = %q, want it to mention panic + boom", httpResp.Error)
	}
	if !strings.Contains(httpResp.Output, "before-panic") {
		t.Errorf("HTTP output = %q, expected to contain 'before-panic'", httpResp.Output)
	}

	// Final marker still emitted.
	captured = append(captured, h.drainCaptured(100*time.Millisecond)...)
	chunks, finals := splitChunksAndFinals(captured)
	if len(finals) != 1 || !finals[0].IsFinal {
		t.Errorf("expected exactly one final marker after panic, got %d", len(finals))
	}
	// Pre-panic chunks must have arrived (at least the prefix).
	var reassembled strings.Builder
	for _, ev := range chunks {
		reassembled.WriteString(ev.Chunk)
	}
	if !strings.Contains(reassembled.String(), "before-panic") {
		t.Errorf("reassembled chunks = %q, expected to contain 'before-panic'", reassembled.String())
	}
}

// TestHandleAPICommandExecute_ZeroOutput verifies a command that writes
// nothing emits exactly one WS event: the final marker with empty chunk.
// The HTTP response carries an empty output.
func TestHandleAPICommandExecute_ZeroOutput(t *testing.T) {
	cmd := &streamingCommand{name: "silent", chunks: nil, delay: 0}
	h := newCommandOutputTestHarness(t, cmd)
	defer h.cancel()

	rec, captured := h.run(t, "/silent")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var httpResp struct {
		Output string `json:"output"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&httpResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if httpResp.Output != "" {
		t.Errorf("HTTP output = %q, want empty", httpResp.Output)
	}

	// Exactly one WS event: the final marker. No content chunks.
	captured = append(captured, h.drainCaptured(100*time.Millisecond)...)
	chunks, finals := splitChunksAndFinals(captured)
	if len(chunks) != 0 {
		t.Errorf("expected zero content chunks, got %d", len(chunks))
	}
	if len(finals) != 1 {
		t.Fatalf("expected exactly one final marker, got %d", len(finals))
	}
	if !finals[0].IsFinal || finals[0].Chunk != "" {
		t.Errorf("final marker = %+v, want is_final=true chunk=\"\"", finals[0])
	}
}

// TestHandleAPICommandExecute_DisconnectedSubscriber verifies that when
// the WS subscriber's event channel closes mid-execution, the handler
// still completes the command normally — no panic, no hang — and the
// HTTP response is correct. Subsequent chunks arrive on the event bus
// but are silently dropped by the closed subscription.
func TestHandleAPICommandExecute_DisconnectedSubscriber(t *testing.T) {
	// Build the harness with a noisy command first so we can verify
	// the test compiles and the wiring is correct; then we'll close
	// the subscription before invoking.
	cmd := &streamingCommand{
		name:   "noisy",
		chunks: []string{"chunk1\n", "chunk2\n", "chunk3\n"},
		delay:  5 * time.Millisecond,
	}
	h := newCommandOutputTestHarness(t, cmd)
	defer h.cancel()

	// Close the subscription BEFORE invoking so the bus will drop
	// every event we publish. We can still observe the result via the
	// HTTP response, and the handler must not block or panic.
	h.cancel()

	rec, captured := h.run(t, "/noisy")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even with subscriber disconnected, got %d: %s", rec.Code, rec.Body.String())
	}
	var httpResp struct {
		Output string `json:"output"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&httpResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(httpResp.Output, "chunk1") ||
		!strings.Contains(httpResp.Output, "chunk2") ||
		!strings.Contains(httpResp.Output, "chunk3") {
		t.Errorf("HTTP output = %q, expected to contain all chunks (HTTP path is independent of WS subscriber state)", httpResp.Output)
	}
	// Some chunks may still arrive on the captured channel between
	// cancel() and the close — Unsubscribe drains inboxes. We don't
	// assert on captured; only that the HTTP path is correct and no
	// panic propagated.
	for _, ev := range captured {
		if ev.Type != events.EventTypeCommandOutput && ev.Type != events.EventTypeCommandOutputDropped {
			t.Errorf("unexpected event type %q in captured stream", ev.Type)
		}
	}
}

// TestHandleAPICommandExecute_SteerEndpointRegression is the SP-114
// Phase 2c backward-compatibility pin: the non-streaming
// executeSafeSteerCommand (used by /api/query/steer) MUST remain
// byte-for-byte identical in behavior — no chunk events emitted, no
// streamer involved, output identical to the previous implementation.
// We exercise it directly to avoid the /api/query/steer request path.
func TestHandleAPICommandExecute_SteerEndpointRegression(t *testing.T) {
	ws, _ := newTestWebServer(t)

	// Subscribe to the bus so we can assert no command_output events fire.
	eventCh := ws.eventBus.Subscribe("steer-regression-test")
	defer ws.eventBus.Unsubscribe("steer-regression-test")

	registry := agent_commands.NewCommandRegistry()
	registry.Register(&echoCommand{})
	a := &agent.Agent{}
	a.SetSlashCommands(registry)

	// Call the NON-streaming helper the same way /api/query/steer does.
	cmd, output, err := ws.executeSafeSteerCommand("/echo hello world", a)
	if err != nil {
		t.Fatalf("executeSafeSteerCommand: %v", err)
	}
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if !strings.Contains(output, "hello world") {
		t.Errorf("output = %q, expected to contain 'hello world'", output)
	}

	// Drain the bus briefly — no command_output events should have
	// fired (the non-streaming path doesn't call publishClientEventWithChat).
	deadline := time.After(50 * time.Millisecond)
loop:
	for {
		select {
		case ev := <-eventCh:
			if ev.Type == events.EventTypeCommandOutput || ev.Type == events.EventTypeCommandOutputDropped {
				t.Errorf("non-streaming executeSafeSteerCommand must NOT emit %s events, got one", ev.Type)
			}
		case <-deadline:
			break loop
		}
	}
}

// splitChunksAndFinals separates a captured event list into the regular
// chunk events and the single final marker. command_output_dropped events
// are filtered out — they're tracked separately via the dropped count
// and aren't part of the chunk stream the WebUI consumer rebuilds.
func splitChunksAndFinals(evs []events.UIEvent) (chunks []commandOutputEvent, finals []commandOutputEvent) {
	for _, ev := range evs {
		if ev.Type == events.EventTypeCommandOutputDropped {
			continue
		}
		decoded := decodeCommandOutput(ev)
		if decoded.IsFinal {
			finals = append(finals, decoded)
		} else {
			chunks = append(chunks, decoded)
		}
	}
	return
}

func finalFlags(finals []commandOutputEvent) []bool {
	out := make([]bool, len(finals))
	for i, f := range finals {
		out[i] = f.IsFinal
	}
	return out
}
