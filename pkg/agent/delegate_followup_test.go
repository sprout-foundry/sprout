package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// parseDelegateConfig: follow_up parsing
// ---------------------------------------------------------------------------

func TestParseDelegateConfig_FollowUp(t *testing.T) {
	t.Run("ParsesFollowUpMessages", func(t *testing.T) {
		args := map[string]interface{}{
			"prompt":     "write tests",
			"follow_up":  []interface{}{"fix the flaky test", "add more coverage"},
		}
		cfg, err := parseDelegateConfig(args)
		require.NoError(t, err)
		assert.Equal(t, "write tests", cfg.Prompt)
		require.Len(t, cfg.FollowUpMessages, 2)
		assert.Equal(t, "fix the flaky test", cfg.FollowUpMessages[0])
		assert.Equal(t, "add more coverage", cfg.FollowUpMessages[1])
	})

	t.Run("ParsesEmptyFollowUpArray", func(t *testing.T) {
		args := map[string]interface{}{
			"prompt":     "review code",
			"follow_up":  []interface{}{},
		}
		cfg, err := parseDelegateConfig(args)
		require.NoError(t, err)
		// Empty slice should result in nil or empty — either is fine
		assert.Empty(t, cfg.FollowUpMessages)
	})

	t.Run("MissingFollowUpResultsInNil", func(t *testing.T) {
		args := map[string]interface{}{
			"prompt": "write tests",
		}
		cfg, err := parseDelegateConfig(args)
		require.NoError(t, err)
		assert.Nil(t, cfg.FollowUpMessages)
	})

	t.Run("SkipsNonStringValues", func(t *testing.T) {
		args := map[string]interface{}{
			"prompt":     "write tests",
			"type":       "debugger",
			"follow_up":  []interface{}{"valid message", 42, nil, true, 3.14, "another valid"},
		}
		cfg, err := parseDelegateConfig(args)
		require.NoError(t, err)
		require.Len(t, cfg.FollowUpMessages, 2)
		assert.Equal(t, "valid message", cfg.FollowUpMessages[0])
		assert.Equal(t, "another valid", cfg.FollowUpMessages[1])
	})

	t.Run("ParsesFollowUpWithOtherFields", func(t *testing.T) {
		args := map[string]interface{}{
			"prompt":         "implement feature",
			"role":           "coder",
			"provider":       "openai",
			"model":          "gpt-4",
			"context":        "previous work",
			"max_iterations": float64(50),
			"follow_up":      []interface{}{"handle edge cases", "add logging"},
			"tools":          []interface{}{"read_file", "write_file"},
			"files":          []interface{}{"pkg/agent/agent.go"},
		}
		cfg, err := parseDelegateConfig(args)
		require.NoError(t, err)
		assert.Equal(t, "implement feature", cfg.Prompt)
		assert.Equal(t, "coder", cfg.Role)
		assert.Equal(t, "openai", cfg.Provider)
		assert.Equal(t, "gpt-4", cfg.Model)
		assert.Equal(t, "previous work", cfg.Context)
		assert.Equal(t, 50, cfg.MaxIterations)
		require.Len(t, cfg.Tools, 2)
		assert.Equal(t, "read_file", cfg.Tools[0])
		require.Len(t, cfg.Files, 1)
		assert.Equal(t, "pkg/agent/agent.go", cfg.Files[0])
		require.Len(t, cfg.FollowUpMessages, 2)
		assert.Equal(t, "handle edge cases", cfg.FollowUpMessages[0])
		assert.Equal(t, "add logging", cfg.FollowUpMessages[1])
	})
}

// ---------------------------------------------------------------------------
// DelegateConfig.Validate with FollowUpMessages
// ---------------------------------------------------------------------------

func TestDelegateConfig_FollowUpValidation(t *testing.T) {
	t.Run("ValidatesWithFollowUpMessages", func(t *testing.T) {
		cfg := &DelegateConfig{
			Prompt:           "write tests",
			MaxIterations:    10,
			FollowUpMessages: []string{"fix issue A", "fix issue B"},
		}
		err := cfg.Validate()
		require.NoError(t, err)
		// FollowUpMessages should be untouched
		require.Len(t, cfg.FollowUpMessages, 2)
		assert.Equal(t, "fix issue A", cfg.FollowUpMessages[0])
	})

	t.Run("ValidatesWithEmptyFollowUpSlice", func(t *testing.T) {
		cfg := &DelegateConfig{
			Prompt:           "review",
			FollowUpMessages: []string{},
		}
		err := cfg.Validate()
		require.NoError(t, err)
		assert.Empty(t, cfg.FollowUpMessages)
	})

	t.Run("ValidatesWithNilFollowUp", func(t *testing.T) {
		cfg := &DelegateConfig{
			Prompt:           "debug",
			FollowUpMessages: nil,
		}
		err := cfg.Validate()
		require.NoError(t, err)
		assert.Nil(t, cfg.FollowUpMessages)
	})

	t.Run("PromptStillRequiredWithFollowUp", func(t *testing.T) {
		cfg := &DelegateConfig{
			Prompt:           "",
			FollowUpMessages: []string{"msg"},
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prompt is required")
	})

	t.Run("WhitespaceOnlyPromptWithFollowUp", func(t *testing.T) {
		cfg := &DelegateConfig{
			Prompt:           "   \t  ",
			FollowUpMessages: []string{"msg"},
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prompt is required")
	})
}

// ---------------------------------------------------------------------------
// RecordFollowUpInjection on DelegateStreamBridge
// ---------------------------------------------------------------------------

func TestRecordFollowUpInjection(t *testing.T) {
	t.Run("PublishesFollowUpInjetedEvent", func(t *testing.T) {
		parent, bus := newTestAgentForStreamBridge(t)

		ch := bus.Subscribe("test-client")
		defer bus.Unsubscribe("test-client")

		bridge := NewDelegateStreamBridge(parent, "test-delegate-1")
		bridge.RecordFollowUpInjection("please fix this bug")

		select {
		case event := <-ch:
			assert.Equal(t, events.EventTypeDelegateActivity, event.Type)
			data, ok := event.Data.(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, "test-delegate-1", data["delegate_id"])
			assert.Equal(t, "follow_up_injected", data["action"])
			assert.Equal(t, "please fix this bug", data["summary"])
			// parent.delegateDepth is 0, so depth should be 0+1=1
			assert.Equal(t, 1, data["depth"])
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for follow_up_injected event")
		}
	})

	t.Run("DerivesDepthFromParentAgent", func(t *testing.T) {
		parent, bus := newTestAgentForStreamBridge(t)
		parent.delegateDepth = 3 // simulate a nested delegate

		ch := bus.Subscribe("test-client")
		defer bus.Unsubscribe("test-client")

		bridge := NewDelegateStreamBridge(parent, "test-delegate-depth")
		bridge.RecordFollowUpInjection("nested message")

		select {
		case event := <-ch:
			data := event.Data.(map[string]interface{})
			// depth should be parent.delegateDepth + 1 = 4
			assert.Equal(t, 4, data["depth"])
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for follow_up_injected event")
		}
	})

	t.Run("TruncatesLongMessages", func(t *testing.T) {
		parent, bus := newTestAgentForStreamBridge(t)

		ch := bus.Subscribe("test-client")
		defer bus.Unsubscribe("test-client")

		bridge := NewDelegateStreamBridge(parent, "test-delegate-2")
		longMsg := strings.Repeat("x", 200)
		bridge.RecordFollowUpInjection(longMsg)

		select {
		case event := <-ch:
			data := event.Data.(map[string]interface{})
			summary := data["summary"].(string)
			// truncateSummary truncates to 100 chars and appends "..."
			assert.Equal(t, 103, len(summary), "expected truncation to 100 chars + '...'")
			assert.True(t, strings.HasSuffix(summary, "..."))
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for follow_up_injected event")
		}
	})

	t.Run("DoesNotTruncateShortMessages", func(t *testing.T) {
		parent, bus := newTestAgentForStreamBridge(t)

		ch := bus.Subscribe("test-client")
		defer bus.Unsubscribe("test-client")

		bridge := NewDelegateStreamBridge(parent, "test-delegate-3")
		shortMsg := "short"
		bridge.RecordFollowUpInjection(shortMsg)

		select {
		case event := <-ch:
			data := event.Data.(map[string]interface{})
			assert.Equal(t, shortMsg, data["summary"])
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for follow_up_injected event")
		}
	})

	t.Run("HandlesEmptyMessage", func(t *testing.T) {
		parent, bus := newTestAgentForStreamBridge(t)

		ch := bus.Subscribe("test-client")
		defer bus.Unsubscribe("test-client")

		bridge := NewDelegateStreamBridge(parent, "test-delegate-4")
		bridge.RecordFollowUpInjection("")

		select {
		case event := <-ch:
			data := event.Data.(map[string]interface{})
			assert.Equal(t, "", data["summary"])
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for follow_up_injected event")
		}
	})

	t.Run("MultipleInjectionsArePublished", func(t *testing.T) {
		parent, bus := newTestAgentForStreamBridge(t)

		ch := bus.Subscribe("test-client")
		defer bus.Unsubscribe("test-client")

		bridge := NewDelegateStreamBridge(parent, "test-delegate-5")

		bridge.RecordFollowUpInjection("message one")
		bridge.RecordFollowUpInjection("message two")
		bridge.RecordFollowUpInjection("message three")

		for i := 0; i < 3; i++ {
			select {
			case event := <-ch:
				assert.Equal(t, events.EventTypeDelegateActivity, event.Type)
				data := event.Data.(map[string]interface{})
				assert.Equal(t, "follow_up_injected", data["action"])
			case <-time.After(500 * time.Millisecond):
				t.Fatalf("timed out waiting for event %d", i+1)
			}
		}
	})

	t.Run("NilParentAgentDoesNotPanic", func(t *testing.T) {
		bridge := NewDelegateStreamBridge(nil, "test-delegate-6")

		// Should not panic
		bridge.RecordFollowUpInjection("safe")
	})

	t.Run("NilEventBusDoesNotPanic", func(t *testing.T) {
		parent := &Agent{} // eventBus is nil
		bridge := NewDelegateStreamBridge(parent, "test-delegate-7")

		// Should not panic
		bridge.RecordFollowUpInjection("safe")
	})
}

// ---------------------------------------------------------------------------
// runDelegateQuery: follow-up injection goroutine behavior
// ---------------------------------------------------------------------------

func TestRunDelegateQuery_FollowUpInjection(t *testing.T) {
	t.Run("InjectsFollowUpMessagesIntoChannel", func(t *testing.T) {
		// Create a minimal delegate agent with a buffered input channel.
		// We do NOT set a client, so ProcessQuery will error quickly,
		// but the follow-up injection goroutine runs concurrently and
		// should still deliver messages to the channel.
		delegate := &Agent{
			inputInjectionChan:  make(chan string, 10),
		}
		bridge := NewDelegateStreamBridge(&Agent{}, "test-1")
		bridge.Start()
		defer bridge.Stop()

		cfg := &DelegateConfig{
			Prompt:           "dummy prompt",
			FollowUpMessages: []string{"msg-alpha", "msg-beta"},
		}

		// runDelegateQuery will error on ProcessQuery (no client),
		// but the goroutine should still inject messages.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err := runDelegateQuery(ctx, delegate, "dummy prompt", bridge, cfg)
		// ProcessQuery will fail because there's no client
		assert.Error(t, err)

		// Give the goroutine time to inject messages (500ms delay between them)
		time.Sleep(1200 * time.Millisecond)

		// Drain the channel and check messages were injected
		delegate.inputInjectionMutex.Lock()
		received := make([]string, 0, 2)
		for {
			select {
			case msg := <-delegate.inputInjectionChan:
				received = append(received, msg)
			default:
				goto done
			}
		}
		delegate.inputInjectionMutex.Unlock()
	done:

		require.Len(t, received, 2)
		assert.Equal(t, "msg-alpha", received[0])
		assert.Equal(t, "msg-beta", received[1])
	})

	t.Run("NoFollowUpMessagesSkipsGoroutine", func(t *testing.T) {
		// When FollowUpMessages is nil/empty, no goroutine should start.
		delegate := &Agent{
			inputInjectionChan: make(chan string, 10),
		}
		bridge := NewDelegateStreamBridge(&Agent{}, "test-2")
		bridge.Start()
		defer bridge.Stop()

		cfg := &DelegateConfig{
			Prompt:           "dummy prompt",
			FollowUpMessages: nil,
		}

		ctx := context.Background()
		_, err := runDelegateQuery(ctx, delegate, "dummy prompt", bridge, cfg)
		assert.Error(t, err) // ProcessQuery fails without client

		// Wait briefly then check channel is empty (no goroutine started)
		time.Sleep(100 * time.Millisecond)

		delegate.inputInjectionMutex.Lock()
		select {
		case <-delegate.inputInjectionChan:
			t.Fatal("expected no messages, goroutine should not have started")
		default:
			// Good — channel is empty
		}
		delegate.inputInjectionMutex.Unlock()
	})

	t.Run("EmptyFollowUpSliceSkipsGoroutine", func(t *testing.T) {
		delegate := &Agent{
			inputInjectionChan: make(chan string, 10),
		}
		bridge := NewDelegateStreamBridge(&Agent{}, "test-3")
		bridge.Start()
		defer bridge.Stop()

		cfg := &DelegateConfig{
			Prompt:           "dummy prompt",
			FollowUpMessages: []string{},
		}

		ctx := context.Background()
		_, err := runDelegateQuery(ctx, delegate, "dummy prompt", bridge, cfg)
		assert.Error(t, err)

		time.Sleep(100 * time.Millisecond)

		delegate.inputInjectionMutex.Lock()
		select {
		case <-delegate.inputInjectionChan:
			t.Fatal("expected no messages for empty slice")
		default:
		}
		delegate.inputInjectionMutex.Unlock()
	})

	t.Run("SingleFollowUpMessageInjectedImmediately", func(t *testing.T) {
		// First message should be injected immediately (no 500ms wait).
		delegate := &Agent{
			inputInjectionChan: make(chan string, 10),
		}
		bridge := NewDelegateStreamBridge(&Agent{}, "test-4")
		bridge.Start()
		defer bridge.Stop()

		cfg := &DelegateConfig{
			Prompt:           "dummy",
			FollowUpMessages: []string{"first-message"},
		}

		ctx := context.Background()
		_, err := runDelegateQuery(ctx, delegate, "dummy", bridge, cfg)
		assert.Error(t, err)

		// Very short wait — first message should be injected right away
		time.Sleep(200 * time.Millisecond)

		delegate.inputInjectionMutex.Lock()
		select {
		case msg := <-delegate.inputInjectionChan:
			assert.Equal(t, "first-message", msg)
		default:
			t.Fatal("expected first message to be injected immediately")
		}
		delegate.inputInjectionMutex.Unlock()
	})

	t.Run("RecordsFollowUpInjectionInBridge", func(t *testing.T) {
		parent, bus := newTestAgentForStreamBridge(t)

		ch := bus.Subscribe("test-client")
		defer bus.Unsubscribe("test-client")

		delegate := &Agent{
			inputInjectionChan: make(chan string, 10),
		}
		bridge := NewDelegateStreamBridge(parent, "test-5")
		bridge.Start()
		defer bridge.Stop()

		cfg := &DelegateConfig{
			Prompt:           "dummy",
			FollowUpMessages: []string{"tracked-msg"},
		}

		ctx := context.Background()
		_, err := runDelegateQuery(ctx, delegate, "dummy", bridge, cfg)
		assert.Error(t, err)

		// Wait for injection + event publication
		time.Sleep(500 * time.Millisecond)

		select {
		case event := <-ch:
			data := event.Data.(map[string]interface{})
			assert.Equal(t, "follow_up_injected", data["action"])
			assert.Equal(t, "tracked-msg", data["summary"])
		case <-time.After(1 * time.Second):
			t.Fatal("timed out waiting for follow_up_injected event from goroutine")
		}
	})

	t.Run("HandlesSingleMessageNoDelay", func(t *testing.T) {
		// With exactly 1 follow-up message, there's no 500ms delay.
		// The 500ms delay only applies between messages (i > 0).
		delegate := &Agent{
			inputInjectionChan: make(chan string, 10),
		}
		bridge := NewDelegateStreamBridge(&Agent{}, "test-6")
		bridge.Start()
		defer bridge.Stop()

		cfg := &DelegateConfig{
			Prompt:           "dummy",
			FollowUpMessages: []string{"only-one"},
		}

		ctx := context.Background()
		_, err := runDelegateQuery(ctx, delegate, "dummy", bridge, cfg)
		assert.Error(t, err)

		time.Sleep(200 * time.Millisecond)

		delegate.inputInjectionMutex.Lock()
		select {
		case msg := <-delegate.inputInjectionChan:
			assert.Equal(t, "only-one", msg)
		default:
			t.Fatal("expected message to be injected without delay")
		}
		delegate.inputInjectionMutex.Unlock()
	})

	t.Run("ChannelFullBackpressureDoesNotCrash", func(t *testing.T) {
		// Fill the inputInjectionChan to capacity (inputInjectionBufferSize = 10),
		// then call runDelegateQuery with FollowUpMessages. The goroutine should
		// receive errors from InjectInputContext (channel full) and handle them
		// gracefully by logging a warning and continuing — no crash.
		delegate := &Agent{
			inputInjectionChan: make(chan string, inputInjectionBufferSize),
		}

		// Fill the channel to capacity
		for i := 0; i < inputInjectionBufferSize; i++ {
			delegate.inputInjectionChan <- fmt.Sprintf("filler-%d", i)
		}

		parent, bus := newTestAgentForStreamBridge(t)
		ch := bus.Subscribe("test-client")
		defer bus.Unsubscribe("test-client")

		bridge := NewDelegateStreamBridge(parent, "test-backpressure")
		bridge.Start()
		defer bridge.Stop()

		cfg := &DelegateConfig{
			Prompt:           "dummy",
			FollowUpMessages: []string{"overflow-msg-1", "overflow-msg-2"},
		}

		ctx := context.Background()

		// This should NOT panic — even though the channel is full, the goroutine
		// handles InjectInputContext errors gracefully.
		_, err := runDelegateQuery(ctx, delegate, "dummy", bridge, cfg)
		assert.Error(t, err) // ProcessQuery fails because there's no client

		// Wait for the goroutine to attempt injection (+ 500ms delay between messages)
		time.Sleep(1200 * time.Millisecond)

		// Verify no messages made it into the channel (they should all have failed)
		// because the channel is still full from our fillers.
		delegate.inputInjectionMutex.Lock()
		// Drain filler messages first
		drained := 0
		for {
			select {
			case msg := <-delegate.inputInjectionChan:
				drained++
				assert.True(t, strings.HasPrefix(msg, "filler-"),
					"expected only filler messages, got: %s", msg)
			default:
				goto done
			}
		}
		delegate.inputInjectionMutex.Unlock()
	done:
		assert.Equal(t, inputInjectionBufferSize, drained,
			"expected all filler messages still in channel, no overflow messages injected")

		// Verify the "follow_up_injected" event was NOT published (injections failed)
		select {
		case <-ch:
			t.Fatal("expected no follow_up_injected events when channel is full")
		default:
			// Good — no event published, as expected
		}
	})
}

// ---------------------------------------------------------------------------
// DelegateConfig.FollowUpMessages JSON serialization
// ---------------------------------------------------------------------------

func TestDelegateConfig_FollowUpJSONRoundTrip(t *testing.T) {
	t.Run("RoundTripWithFollowUpMessages", func(t *testing.T) {
		cfg := &DelegateConfig{
			Prompt:           "write tests",
			Role:             "tester",
			FollowUpMessages: []string{"fix edge case", "add benchmarks"},
		}

		data, err := json.Marshal(cfg)
		require.NoError(t, err)

		var decoded DelegateConfig
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, "write tests", decoded.Prompt)
		assert.Equal(t, "tester", decoded.Role)
		require.Len(t, decoded.FollowUpMessages, 2)
		assert.Equal(t, "fix edge case", decoded.FollowUpMessages[0])
		assert.Equal(t, "add benchmarks", decoded.FollowUpMessages[1])
	})

	t.Run("FollowUpOmitempty", func(t *testing.T) {
		cfg := &DelegateConfig{
			Prompt: "write tests",
		}

		data, err := json.Marshal(cfg)
		require.NoError(t, err)

		// The "follow_up" key should not appear when FollowUpMessages is nil
		assert.False(t, strings.Contains(string(data), "follow_up"),
			"follow_up should be omitted when nil/empty: %s", string(data))
	})

	t.Run("RoundTripWithEmptyFollowUp", func(t *testing.T) {
		cfg := &DelegateConfig{
			Prompt:           "review",
			FollowUpMessages: []string{},
		}

		data, err := json.Marshal(cfg)
		require.NoError(t, err)

		var decoded DelegateConfig
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		// Empty slice serializes to [], which unmarshals as empty slice
		assert.Empty(t, decoded.FollowUpMessages)
	})

	t.Run("RoundTripWithFullConfig", func(t *testing.T) {
		cfg := &DelegateConfig{
			Prompt:           "implement feature X",
			Role:             "coder",
			Provider:         "anthropic",
			Model:            "claude-sonnet-4-20250514",
			Tools:            []string{"read_file", "write_file", "shell_command"},
			Context:          "previous agent work summary",
			MaxIterations:    30,
			Files:            []string{"pkg/core/engine.go"},
			FollowUpMessages: []string{"handle nil case", "test error paths", "verify output"},
		}

		data, err := json.Marshal(cfg)
		require.NoError(t, err)

		var decoded DelegateConfig
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, cfg.Prompt, decoded.Prompt)
		assert.Equal(t, cfg.Role, decoded.Role)
		assert.Equal(t, cfg.Provider, decoded.Provider)
		assert.Equal(t, cfg.Model, decoded.Model)
		assert.Equal(t, cfg.Context, decoded.Context)
		assert.Equal(t, cfg.MaxIterations, decoded.MaxIterations)
		assert.Equal(t, cfg.Tools, decoded.Tools)
		assert.Equal(t, cfg.Files, decoded.Files)
		require.Len(t, decoded.FollowUpMessages, 3)
		assert.Equal(t, "handle nil case", decoded.FollowUpMessages[0])
		assert.Equal(t, "test error paths", decoded.FollowUpMessages[1])
		assert.Equal(t, "verify output", decoded.FollowUpMessages[2])
	})
}

// ---------------------------------------------------------------------------
// truncateSummary helper (used by RecordFollowUpInjection)
// ---------------------------------------------------------------------------

func TestTruncateSummary(t *testing.T) {
	t.Run("DoesNotTruncateShortString", func(t *testing.T) {
		result := truncateSummary("hello", 10)
		assert.Equal(t, "hello", result)
	})

	t.Run("DoesNotTruncateExactLength", func(t *testing.T) {
		result := truncateSummary("hello", 5)
		assert.Equal(t, "hello", result)
	})

	t.Run("TruncatesLongString", func(t *testing.T) {
		result := truncateSummary("hello world", 5)
		assert.Equal(t, "hello...", result)
		assert.Equal(t, 8, len(result))
	})

	t.Run("TruncatesZeroMaxLen", func(t *testing.T) {
		result := truncateSummary("hello", 0)
		assert.Equal(t, "...", result)
	})

	t.Run("HandlesEmptyString", func(t *testing.T) {
		result := truncateSummary("", 10)
		assert.Equal(t, "", result)
	})

	t.Run("UsedByRecordFollowUpInjection", func(t *testing.T) {
		parent, bus := newTestAgentForStreamBridge(t)

		ch := bus.Subscribe("test-client")
		defer bus.Unsubscribe("test-client")

		bridge := NewDelegateStreamBridge(parent, "test-truncate")
		// RecordFollowUpInjection truncates to 100 chars
		longMsg := strings.Repeat("A", 150)
		bridge.RecordFollowUpInjection(longMsg)

		select {
		case event := <-ch:
			data := event.Data.(map[string]interface{})
			summary := data["summary"].(string)
			// 100 chars + "..." = 103
			assert.Equal(t, 103, len(summary))
			assert.True(t, strings.HasSuffix(summary, "..."))
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for event")
		}
	})
}
