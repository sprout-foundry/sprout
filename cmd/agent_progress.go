//go:build !js

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// progressEventsTarget is the --progress-events flag value.
// Valid values: "stderr", "stdout", or a filesystem path.
// Empty means progress events are disabled (default).
var progressEventsTarget string

// progressEmitter holds the writer and subscriber cleanup for progress events.
type progressEmitter struct {
	writer   io.WriteCloser
	bus      *events.EventBus
	subName  string
	stopOnce sync.Once
	// startedEmitted suppresses duplicate query_started events. The agent
	// publishes query_started from several code paths (the CLI's
	// ProcessQuery, the seed query's prepare phase, and seed's rich event
	// publisher); only the first should produce a milestone line. It is
	// reset on completion or error so a subsequent (new) query in the same
	// session emits its own "started" milestone.
	startedEmitted bool
}

// startProgressEmitter subscribes to the event bus and writes one-line
// progress milestones to the configured target. Returns nil if the target
// is empty (flag not set). On file-open failure it prints a single warning
// to stderr and returns nil (run continues without progress events).
func startProgressEmitter(ctx context.Context, bus *events.EventBus) *progressEmitter {
	if progressEventsTarget == "" {
		return nil
	}

	var w io.WriteCloser
	switch progressEventsTarget {
	case "stderr":
		w = &nopCloser{os.Stderr}
	case "stdout":
		w = &nopCloser{os.Stdout}
	default:
		f, err := os.Create(progressEventsTarget)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: cannot open progress-events file %q: %v; progress events disabled\n", progressEventsTarget, err)
			return nil
		}
		w = f
	}

	subName := "progress-events"
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

// handleEvent processes a single UIEvent and emits a progress line.
func (pe *progressEmitter) handleEvent(evt events.UIEvent) {
	data, _ := evt.Data.(map[string]interface{})
	var line string

	switch evt.Type {
	case events.EventTypeQueryStarted:
		// The agent publishes query_started from multiple code paths;
		// emit the milestone only once per run.
		if pe.startedEmitted {
			return
		}
		pe.startedEmitted = true
		line = "progress: agent run started"
	case events.EventTypeToolStart:
		line = formatToolStart(data)
	case events.EventTypeQueryCompleted:
		pe.startedEmitted = false
		line = "progress: agent run completed"
	case events.EventTypeError:
		pe.startedEmitted = false
		line = formatError(data)
	default:
		return
	}

	if line == "" {
		return
	}

	// Every milestone is prefixed with a newline so it always starts on its
	// own line in the consumer's log stream. The container's stdout and
	// stderr are merged into a single line-oriented stream (docker log
	// multiplexing), and the assistant's streamed prose is written to stdout
	// without a guaranteed trailing newline — a milestone arriving right
	// after the final prose chunk would otherwise be glued onto the tail of
	// the response text and become unparseable (observed in E2E:
	// "…pull requests.progress: agent run completed"). When the stream is
	// already at a line boundary the prefix only adds a harmless blank line.
	if _, err := io.WriteString(pe.writer, "\n"+line+"\n"); err != nil {
		// Best-effort: stop writing further lines on I/O error.
		pe.stop()
	}
}

// formatToolStart builds the progress line for a tool_start event.
func formatToolStart(data map[string]interface{}) string {
	name, _ := data["tool_name"].(string)
	if name == "" {
		return ""
	}

	argsJSON, _ := data["arguments"].(string)
	preview := extractPreview(name, argsJSON)

	if preview != "" {
		return fmt.Sprintf("progress: tool: %s %s", name, preview)
	}
	return fmt.Sprintf("progress: tool: %s", name)
}

// extractPreview parses the arguments JSON and extracts a relevant preview
// string based on the tool name. Returns "" if no preview is available.
func extractPreview(toolName, argsJSON string) string {
	if argsJSON == "" {
		return ""
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}

	var raw string
	switch toolName {
	case "edit_file", "write_file", "read_file", "patch":
		raw, _ = args["path"].(string)
	case "shell_command", "shell":
		raw, _ = args["command"].(string)
	case "search", "search_files":
		raw, _ = args["query"].(string)
		if raw == "" {
			raw, _ = args["search_pattern"].(string)
		}
	default:
		return ""
	}

	if raw == "" {
		return ""
	}

	// First line only
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		raw = raw[:idx]
	}

	// Strip control characters (keep printable ASCII and common Unicode)
	raw = controlCharRegex.ReplaceAllString(raw, "")

	// Truncate to 120 chars
	if len(raw) > 120 {
		raw = raw[:120]
	}

	return raw
}

// formatError builds the progress line for an error event.
func formatError(data map[string]interface{}) string {
	msg, _ := data["message"].(string)
	if msg == "" {
		if errMsg, ok := data["error"].(string); ok {
			msg = errMsg
		}
	}
	if msg == "" {
		return "progress: error: unknown error"
	}

	// First line only, ≤200 chars, strip control chars
	if idx := strings.Index(msg, "\n"); idx >= 0 {
		msg = msg[:idx]
	}
	msg = controlCharRegex.ReplaceAllString(msg, "")
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return fmt.Sprintf("progress: error: %s", msg)
}

// controlCharRegex matches control characters (0x00-0x1F except newline/tab,
// and 0x7F). We keep newlines and tabs for initial processing but they get
// stripped by the first-line extraction.
var controlCharRegex = regexp.MustCompile(`[\x00-\x08\x0B-\x0C\x0E-\x1F\x7F]`)

// stop unsubscribes from the event bus and closes the writer. Idempotent.
func (pe *progressEmitter) stop() {
	if pe == nil {
		return
	}
	pe.stopOnce.Do(func() {
		if pe.bus != nil {
			pe.bus.Unsubscribe(pe.subName)
		}
		if pe.writer != nil {
			pe.writer.Close()
		}
	})
}

// nopCloser wraps an io.Writer with a no-op Close.
type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }
