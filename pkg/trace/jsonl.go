package trace

import (
	"encoding/json"
	"os"
	"sync"
)

// jsonlWriter writes JSONL format (one JSON object per line)
type jsonlWriter struct {
	mu      sync.Mutex
	file    *os.File
	encoder *json.Encoder
}

// newJSONLWriter creates a new JSONL writer for the given file path
func newJSONLWriter(path string) (*jsonlWriter, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}

	return &jsonlWriter{
		file:   file,
		encoder: json.NewEncoder(file),
	}, nil
}

// Write marshals and writes a JSON object as a single line
func (w *jsonlWriter) Write(v interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.encoder.Encode(v)
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
