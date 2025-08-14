package utils

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type logRecord struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
	CID   string `json:"cid"`
}

func TestLogger_JSONModeWritesJSONWithCID(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	_ = os.Chdir(dir)

	_ = os.Setenv("LEDIT_JSON_LOGS", "1")
	_ = os.Setenv("LEDIT_CORRELATION_ID", "abc123")
	defer os.Unsetenv("LEDIT_JSON_LOGS")
	defer os.Unsetenv("LEDIT_CORRELATION_ID")

	l := GetLogger(true)
	l.Log("hello world")
	_ = l.Close()

	// Read the last JSON object from the log file; lumberjack writes raw JSON lines
	f, err := os.Open(filepath.Join(".ledit", "workspace.log"))
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()
	var lastLine string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lastLine = scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	var rec logRecord
	if err := json.Unmarshal([]byte(lastLine), &rec); err != nil {
		t.Fatalf("unmarshal: %v; content=%q", err, lastLine)
	}
	if rec.Level != "info" || rec.Msg != "hello world" || rec.CID != "abc123" {
		t.Fatalf("unexpected record: %+v", rec)
	}
}
