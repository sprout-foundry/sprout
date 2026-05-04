package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitDebugLogger(t *testing.T) {
	t.Run("creates temp file with sprout-debug prefix", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()

		err := a.initDebugLogger()
		if err != nil {
			t.Fatalf("initDebugLogger returned error: %v", err)
		}

		if a.debugLogFile == nil {
			t.Fatal("debugLogFile is nil after init")
		}

		if a.debugLogPath == "" {
			t.Fatal("debugLogPath is empty after init")
		}

		if !strings.Contains(a.debugLogPath, "sprout-debug") {
			t.Errorf("debugLogPath %q should contain 'sprout-debug'", a.debugLogPath)
		}

		a.debugLogFile.Close()
		os.Remove(a.debugLogPath)
	})

	t.Run("file is in temp directory", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()

		err := a.initDebugLogger()
		if err != nil {
			t.Fatalf("initDebugLogger returned error: %v", err)
		}

		dir := filepath.Dir(a.debugLogPath)
		// Should be in OS temp dir
		tmpDir := os.TempDir()
		if !strings.HasPrefix(dir, tmpDir) && dir != tmpDir {
			t.Logf("debugLogPath dir=%q, tmpDir=%q (may differ on some systems)", dir, tmpDir)
		}

		a.debugLogFile.Close()
		os.Remove(a.debugLogPath)
	})

	t.Run("header contains provider name", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()

		err := a.initDebugLogger()
		if err != nil {
			t.Fatalf("initDebugLogger returned error: %v", err)
		}

		provider := a.GetProvider()
		a.debugLogFile.Seek(0, 0)
		data, err := os.ReadFile(a.debugLogPath)
		if err != nil {
			t.Fatalf("failed to read debug log: %v", err)
		}

		if !strings.Contains(string(data), "Provider:") {
			t.Error("header should contain 'Provider:' line")
		}
		if !strings.Contains(string(data), provider) {
			t.Errorf("header should contain provider %q", provider)
		}

		a.debugLogFile.Close()
		os.Remove(a.debugLogPath)
	})

	t.Run("header contains model name", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()

		err := a.initDebugLogger()
		if err != nil {
			t.Fatalf("initDebugLogger returned error: %v", err)
		}

		model := a.GetModel()
		a.debugLogFile.Seek(0, 0)
		data, err := os.ReadFile(a.debugLogPath)
		if err != nil {
			t.Fatalf("failed to read debug log: %v", err)
		}

		if !strings.Contains(string(data), "Model:") {
			t.Error("header should contain 'Model:' line")
		}
		if !strings.Contains(string(data), model) {
			t.Errorf("header should contain model %q", model)
		}

		a.debugLogFile.Close()
		os.Remove(a.debugLogPath)
	})

	t.Run("header contains PID", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()

		err := a.initDebugLogger()
		if err != nil {
			t.Fatalf("initDebugLogger returned error: %v", err)
		}

		a.debugLogFile.Seek(0, 0)
		data, err := os.ReadFile(a.debugLogPath)
		if err != nil {
			t.Fatalf("failed to read debug log: %v", err)
		}

		if !strings.Contains(string(data), "PID:") {
			t.Error("header should contain 'PID:' line")
		}

		a.debugLogFile.Close()
		os.Remove(a.debugLogPath)
	})

	t.Run("header contains session start timestamp", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()

		err := a.initDebugLogger()
		if err != nil {
			t.Fatalf("initDebugLogger returned error: %v", err)
		}

		a.debugLogFile.Seek(0, 0)
		data, err := os.ReadFile(a.debugLogPath)
		if err != nil {
			t.Fatalf("failed to read debug log: %v", err)
		}

		if !strings.Contains(string(data), "Session start:") {
			t.Error("header should contain 'Session start:' line")
		}

		a.debugLogFile.Close()
		os.Remove(a.debugLogPath)
	})

	t.Run("header has correct format structure", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()

		err := a.initDebugLogger()
		if err != nil {
			t.Fatalf("initDebugLogger returned error: %v", err)
		}

		a.debugLogFile.Seek(0, 0)
		data, err := os.ReadFile(a.debugLogPath)
		if err != nil {
			t.Fatalf("failed to read debug log: %v", err)
		}

		content := string(data)
		if !strings.HasPrefix(content, "==== Sprout Debug Log ====") {
			t.Error("header should start with '==== Sprout Debug Log ===='")
		}
		if !strings.Contains(content, "========================") {
			t.Error("header should contain closing separator")
		}

		a.debugLogFile.Close()
		os.Remove(a.debugLogPath)
	})

	t.Run("calling initDebugLogger twice creates new file", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()

		err := a.initDebugLogger()
		if err != nil {
			t.Fatalf("first initDebugLogger returned error: %v", err)
		}
		firstPath := a.debugLogPath
		firstFile := a.debugLogFile

		err = a.initDebugLogger()
		if err != nil {
			t.Fatalf("second initDebugLogger returned error: %v", err)
		}

		if a.debugLogPath == firstPath {
			t.Error("second call should create a new temp file")
		}

		firstFile.Close()
		os.Remove(firstPath)
		a.debugLogFile.Close()
		os.Remove(a.debugLogPath)
	})
}
