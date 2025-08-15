package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RunLogger writes structured JSONL events for a single agent run.
type RunLogger struct {
	mu   sync.Mutex
	f    *os.File
	id   string
	path string
}

var (
	globalRunLogger *RunLogger
	runOnce         sync.Once
)

// GetRunLogger creates (once) and returns the run logger.
// Log file: .ledit/runlogs/run-YYYYmmdd_HHMMSS.jsonl
func GetRunLogger() *RunLogger {
	runOnce.Do(func() {
		_ = os.MkdirAll(".ledit/runlogs", 0755)
		name := time.Now().Format("20060102_150405")
		path := filepath.Join(".ledit", "runlogs", fmt.Sprintf("run-%s.jsonl", name))
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			// Fallback: disable run logging if file can't be opened
			globalRunLogger = &RunLogger{}
			return
		}
		globalRunLogger = &RunLogger{f: f, id: name, path: path}
	})
	return globalRunLogger
}

// Close closes the underlying file, if open.
func (r *RunLogger) Close() error {
	if r == nil || r.f == nil {
		return nil
	}
	return r.f.Close()
}

// LogEvent writes a JSON line with the provided type and fields.
func (r *RunLogger) LogEvent(eventType string, fields map[string]any) {
	if r == nil || r.f == nil {
		return
	}
	payload := map[string]any{
		"ts":   time.Now().Format(time.RFC3339Nano),
		"type": eventType,
	}
	for k, v := range fields {
		payload[k] = v
	}
	// Basic secret redactions
	redact := func(s string) string {
		replacements := []string{"AWS_SECRET", "AWS_ACCESS_KEY", "OPENAI_API_KEY", "DEEPINFRA_API_KEY"}
		out := s
		for _, k := range replacements {
			out = strings.ReplaceAll(out, k, "<REDACTED>")
		}
		return out
	}
	// Redact common string fields
	if msg, ok := payload["response"].(string); ok {
		payload["response"] = redact(msg)
	}
	if msg, ok := payload["request"].(string); ok {
		payload["request"] = redact(msg)
	}
	if msgs, ok := payload["messages"].(string); ok {
		payload["messages"] = redact(msgs)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = r.f.Write(append(b, '\n'))
}
