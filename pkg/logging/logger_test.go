package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureOutput captures stdout and stderr during test execution
func captureOutput(f func()) (stdout, stderr string) {
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	f()

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var bufOut, bufErr bytes.Buffer
	io.Copy(&bufOut, rOut)
	io.Copy(&bufErr, rErr)
	return bufOut.String(), bufErr.String()
}

// setupTestLogger creates a logger with a temporary directory
func setupTestLogger(t *testing.T) (*Logger, string) {
	t.Helper()
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	t.Cleanup(func() {
		os.Setenv("HOME", originalHome)
	})
	os.Setenv("HOME", tmpDir)

	logger, err := NewLogger()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	t.Cleanup(func() {
		logger.Close()
	})

	return logger, tmpDir
}

func TestNewLogger(t *testing.T) {
	logger, tmpDir := setupTestLogger(t)

	if logger == nil {
		t.Fatal("Expected non-nil logger")
	}

	// Verify log file was created
	logPath := filepath.Join(tmpDir, ".ledit", "ledit.log")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Errorf("Expected log file to be created at %s", logPath)
	}
}

func TestLoggerInit(t *testing.T) {
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	t.Cleanup(func() {
		os.Setenv("HOME", originalHome)
	})
	os.Setenv("HOME", tmpDir)

	logger := &Logger{}
	err := logger.init()
	if err != nil {
		t.Fatalf("Unexpected error in init: %v", err)
	}

	// Verify .ledit directory was created
	leditDir := filepath.Join(tmpDir, ".ledit")
	if _, err := os.Stat(leditDir); os.IsNotExist(err) {
		t.Error("Expected .ledit directory to be created")
	}

	// Verify log file exists
	logPath := filepath.Join(leditDir, "ledit.log")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("Expected ledit.log to be created")
	}

	logger.Close()
}

func TestLoggerLog(t *testing.T) {
	logger, tmpDir := setupTestLogger(t)

	stdout, _ := captureOutput(func() {
		logger.Log("info", "test message %d", 42)
	})

	// Check stdout output contains expected parts
	if !strings.Contains(stdout, "[INFO]") {
		t.Errorf("Expected log level [INFO] in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "test message 42") {
		t.Errorf("Expected message text in output, got: %s", stdout)
	}
	// Check timestamp format (YYYY-MM-DD HH:MM:SS)
	if !strings.Contains(stdout, "] [") {
		t.Errorf("Expected timestamp format in output, got: %s", stdout)
	}

	// Verify file was written
	logPath := filepath.Join(tmpDir, ".ledit", "ledit.log")
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}
	if !strings.Contains(string(content), "test message 42") {
		t.Errorf("Expected message in log file, got: %s", string(content))
	}
}

func TestLoggerLevels(t *testing.T) {
	logger, _ := setupTestLogger(t)

	tests := []struct {
		name     string
		logFunc  func(string, ...interface{})
		level    string
		message  string
	}{
		{"Debug", logger.Debug, "DEBUG", "debug message"},
		{"Info", logger.Info, "INFO", "info message"},
		{"Warn", logger.Warn, "WARN", "warn message"},
		{"Error", logger.Error, "ERROR", "error message"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, _ := captureOutput(func() {
				tt.logFunc(tt.message)
			})

			if !strings.Contains(stdout, "["+tt.level+"]") {
				t.Errorf("Expected [%s] level in output, got: %s", tt.level, stdout)
			}
			if !strings.Contains(stdout, tt.message) {
				t.Errorf("Expected message '%s' in output, got: %s", tt.message, stdout)
			}
		})
	}
}

func TestLoggerClose(t *testing.T) {
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	t.Cleanup(func() {
		os.Setenv("HOME", originalHome)
	})
	os.Setenv("HOME", tmpDir)

	logger, err := NewLogger()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Close should succeed
	if err := logger.Close(); err != nil {
		t.Errorf("Unexpected error on Close: %v", err)
	}

	// Closing already closed logger returns error (expected behavior)
	err = logger.Close()
	if err == nil {
		t.Error("Expected error on second close of already-closed file")
	}
}

func TestLoggerWithNilFile(t *testing.T) {
	logger := &Logger{logFile: nil}

	// Should not panic when logFile is nil
	stdout, _ := captureOutput(func() {
		logger.Log("info", "test message")
	})

	// With nil file, should output nothing
	if stdout != "" {
		t.Errorf("Expected no output with nil logFile, got: %s", stdout)
	}
}

func TestWriteLocalCopy(t *testing.T) {
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	t.Cleanup(func() {
		os.Setenv("HOME", originalHome)
	})
	os.Setenv("HOME", tmpDir)

	// Create the .ledit directory first (WriteLocalCopy doesn't create it)
	if err := os.MkdirAll(filepath.Join(tmpDir, ".ledit"), 0755); err != nil {
		t.Fatalf("Failed to create .ledit directory: %v", err)
	}

	content := []byte("test content")
	WriteLocalCopy("testfile.txt", content)

	// Verify file was written
	path := filepath.Join(tmpDir, ".ledit", "testfile.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("Expected content %q, got %q", string(content), string(data))
	}
}

func TestGetLogPath(t *testing.T) {
	originalHome := os.Getenv("HOME")
	t.Cleanup(func() {
		os.Setenv("HOME", originalHome)
	})
	os.Setenv("HOME", "/home/testuser")

	path := GetLogPath()
	expected := filepath.Join("/home/testuser", ".ledit", "ledit.log")
	if path != expected {
		t.Errorf("Expected path %q, got %q", expected, path)
	}
}

func TestLoggerFormatSpecifiers(t *testing.T) {
	logger, _ := setupTestLogger(t)

	tests := []struct {
		name    string
		format  string
		args    []interface{}
		want    string
	}{
		{
			name:   "integer",
			format: "count: %d",
			args:   []interface{}{42},
			want:   "count: 42",
		},
		{
			name:   "string",
			format: "name: %s",
			args:   []interface{}{"test"},
			want:   "name: test",
		},
		{
			name:   "multiple args",
			format: "item %s with value %d",
			args:   []interface{}{"foo", 123},
			want:   "item foo with value 123",
		},
		{
			name:   "float",
			format: "pi = %.2f",
			args:   []interface{}{3.14159},
			want:   "pi = 3.14",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, _ := captureOutput(func() {
				logger.Log("info", tt.format, tt.args...)
			})
			if !strings.Contains(stdout, tt.want) {
				t.Errorf("Expected %q in output, got: %s", tt.want, stdout)
			}
		})
	}
}

// Test setup for request logger tests
func setupTestHome(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	t.Cleanup(func() {
		os.Setenv("HOME", originalHome)
	})
	os.Setenv("HOME", tmpDir)
	return tmpDir
}

func TestLogRequestPayload(t *testing.T) {
	tmpDir := setupTestHome(t)

	payload := map[string]interface{}{
		"model":    "gpt-4",
		"prompt":   "test prompt",
		"stream":   true,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	LogRequestPayload(payloadJSON, "openrouter", "gpt-4", true)

	// Verify lastRequest.json was created
	lastReqPath := filepath.Join(tmpDir, ".ledit", "lastRequest.json")
	data, err := os.ReadFile(lastReqPath)
	if err != nil {
		t.Fatalf("Failed to read lastRequest.json: %v", err)
	}
	if string(data) != string(payloadJSON) {
		t.Errorf("Expected payload %q, got %q", string(payloadJSON), string(data))
	}

	// Verify timestamped file was created (check for api_request_*.json)
	entries, err := os.ReadDir(filepath.Join(tmpDir, ".ledit"))
	if err != nil {
		t.Fatalf("Failed to read .ledit directory: %v", err)
	}
	foundTimestamped := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "api_request_") && strings.HasSuffix(entry.Name(), ".json") {
			foundTimestamped = true
			// Verify it contains timestamp, provider, model, streaming fields
			tsData, err := os.ReadFile(filepath.Join(tmpDir, ".ledit", entry.Name()))
			if err != nil {
				t.Errorf("Failed to read timestamped file: %v", err)
				continue
			}
			var tsEntry map[string]interface{}
			if err := json.Unmarshal(tsData, &tsEntry); err != nil {
				t.Errorf("Failed to unmarshal timestamped entry: %v", err)
				continue
			}
			if tsEntry["provider"] != "openrouter" {
				t.Errorf("Expected provider 'openrouter', got %v", tsEntry["provider"])
			}
			if tsEntry["model"] != "gpt-4" {
				t.Errorf("Expected model 'gpt-4', got %v", tsEntry["model"])
			}
			if tsEntry["streaming"] != true {
				t.Errorf("Expected streaming true, got %v", tsEntry["streaming"])
			}
			if tsEntry["request"] == nil {
				t.Error("Expected request field to be populated")
			}
			break
		}
	}
	if !foundTimestamped {
		t.Error("Expected timestamped api_request_*.json file to be created")
	}
}

func TestLogRequestPayloadOnError(t *testing.T) {
	tmpDir := setupTestHome(t)

	payload := map[string]interface{}{
		"model":  "gpt-4o",
		"prompt": "error test",
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	// Simulate an error
	testErr := fmt.Errorf("test connection error")
	LogRequestPayloadOnError(payloadJSON, "openrouter", "gpt-4o", false, "connection", testErr)

	// Verify lastRequest.json was created
	lastReqPath := filepath.Join(tmpDir, ".ledit", "lastRequest.json")
	data, err := os.ReadFile(lastReqPath)
	if err != nil {
		t.Fatalf("Failed to read lastRequest.json: %v", err)
	}
	if string(data) != string(payloadJSON) {
		t.Errorf("Expected payload %q, got %q", string(payloadJSON), string(data))
	}

	// Verify timestamped error file was created
	entries, err := os.ReadDir(filepath.Join(tmpDir, ".ledit"))
	if err != nil {
		t.Fatalf("Failed to read .ledit directory: %v", err)
	}
	foundErrorFile := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "error_request_connection_") && strings.HasSuffix(entry.Name(), ".json") {
			foundErrorFile = true
			// Verify it contains error context
			errData, err := os.ReadFile(filepath.Join(tmpDir, ".ledit", entry.Name()))
			if err != nil {
				t.Errorf("Failed to read error file: %v", err)
				continue
			}
			var errEntry map[string]interface{}
			if err := json.Unmarshal(errData, &errEntry); err != nil {
				t.Errorf("Failed to unmarshal error entry: %v", err)
				continue
			}
			if errEntry["error_type"] != "connection" {
				t.Errorf("Expected error_type 'connection', got %v", errEntry["error_type"])
			}
			if errEntry["error"] != "test connection error" {
				t.Errorf("Expected error message 'test connection error', got %v", errEntry["error"])
			}
			if errEntry["provider"] != "openrouter" {
				t.Errorf("Expected provider 'openrouter', got %v", errEntry["provider"])
			}
			break
		}
	}
	if !foundErrorFile {
		t.Error("Expected error_request_connection_*.json file to be created")
	}
}

func TestWriteLocalCopyRequest(t *testing.T) {
	_ = setupTestHome(t)

	// LEDIT_COPY_LOGS_TO_CWD should not be set by default
	originalEnv := os.Getenv("LEDIT_COPY_LOGS_TO_CWD")
	t.Cleanup(func() {
		os.Setenv("LEDIT_COPY_LOGS_TO_CWD", originalEnv)
	})

	// Create a test CWD
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	data := []byte("test request data")

	// Without env var, should not write
	os.Unsetenv("LEDIT_COPY_LOGS_TO_CWD")
	WriteLocalCopyRequest("test_req.json", data)

	testPath := filepath.Join(cwd, "test_req.json")
	if _, err := os.Stat(testPath); !os.IsNotExist(err) {
		t.Error("Expected file not to be written when env var is not set")
		os.Remove(testPath)
	}

	// With env var set to 1, should write to CWD
	os.Setenv("LEDIT_COPY_LOGS_TO_CWD", "1")
	WriteLocalCopyRequest("test_req2.json", data)

	testPath2 := filepath.Join(cwd, "test_req2.json")
	if _, err := os.Stat(testPath2); os.IsNotExist(err) {
		t.Error("Expected file to be written when env var is set to 1")
	} else {
		content, err := os.ReadFile(testPath2)
		if err != nil {
			t.Errorf("Failed to read file: %v", err)
		}
		if string(content) != string(data) {
			t.Errorf("Expected content %q, got %q", string(data), string(content))
		}
		os.Remove(testPath2)
	}
}

func TestLogRequestPayloadWithNonStreaming(t *testing.T) {
	tmpDir := setupTestHome(t)

	payload := []byte(`{"prompt": "hello"}`)
	LogRequestPayload(payload, "anthropic", "claude-3", false)

	// Verify timestamped file has streaming=false
	entries, err := os.ReadDir(filepath.Join(tmpDir, ".ledit"))
	if err != nil {
		t.Fatalf("Failed to read .ledit directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "api_request_") {
			data, err := os.ReadFile(filepath.Join(tmpDir, ".ledit", entry.Name()))
			if err != nil {
				t.Fatalf("Failed to read file: %v", err)
			}
			var result map[string]interface{}
			json.Unmarshal(data, &result)
			if result["streaming"] != false {
				t.Errorf("Expected streaming=false, got %v", result["streaming"])
			}
			break
		}
	}
}

func TestLogRequestPayloadJSONFormat(t *testing.T) {
	tmpDir := setupTestHome(t)

	// Verify the timestamped file is valid JSON with proper formatting (indented)
	payload := []byte(`{"test":"value"}`)
	LogRequestPayload(payload, "test-provider", "test-model", true)

	entries, _ := os.ReadDir(filepath.Join(tmpDir, ".ledit"))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "api_request_") {
			data, err := os.ReadFile(filepath.Join(tmpDir, ".ledit", entry.Name()))
			if err != nil {
				t.Fatalf("Failed to read file: %v", err)
			}
			// Check for indentation (should contain newlines and spaces)
			if !bytes.Contains(data, []byte("\n")) {
				t.Error("Expected JSON output to be indented/formatted with newlines")
			}
			break
		}
	}
}
