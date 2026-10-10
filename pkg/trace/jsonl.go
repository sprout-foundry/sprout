package trace

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/secretdetect"
)

// jsonlWriter writes JSONL format (one JSON object per line)
type jsonlWriter struct {
	mu      sync.Mutex
	file    *os.File
	encoder *json.Encoder
	// redact, when true, runs the egress secret-detection backstop over every
	// serialized line before it is written. Trace files hold full prompts and
	// raw model responses, so a secret that slipped past per-tool redaction
	// would otherwise land on disk in the clear.
	redact bool
}

// newJSONLWriter creates a new JSONL writer for the given file path. The file
// is created owner-only (0600) because it captures raw prompt/response content.
func newJSONLWriter(path string, redact bool) (*jsonlWriter, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to open JSONL file: %w", err)
	}
	// O_CREATE only applies the mode when the file is created; a pre-existing
	// file keeps its old permissions. Tighten explicitly so a trace path that
	// already exists cannot stay world-readable.
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to set JSONL file permissions: %w", err)
	}

	return &jsonlWriter{
		file:    file,
		encoder: json.NewEncoder(file),
		redact:  redact,
	}, nil
}

// Write marshals and writes a JSON object as a single line. When redaction is
// enabled the serialized line is passed through the same secret-detection
// backstop the egress path uses; RedactOpaqueJSON re-encodes the document so
// the line stays valid JSON even when a secret sits next to a JSON escape.
func (w *jsonlWriter) Write(v interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.redact {
		return w.encoder.Encode(v)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	line := secretdetect.RedactOpaqueJSON(string(raw))
	if _, err := w.file.WriteString(line + "\n"); err != nil {
		return err
	}
	return nil
}

// Close closes the underlying file
func (w *jsonlWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

// Flush flushes the underlying file
func (w *jsonlWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Sync()
}
