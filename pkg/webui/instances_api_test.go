package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHandleAPIInstancesFiltersStaleAndReturnsHostMetadata(t *testing.T) {
	t.Setenv("LEDIT_CONFIG", t.TempDir())

	now := time.Now()
	currentPID := os.Getpid()

	instances := map[string]rawInstanceInfo{
		"active": {
			ID:         "active",
			PID:        currentPID,
			Port:       54321,
			WorkingDir: "/tmp/project-a",
			StartTime:  now.Add(-2 * time.Minute),
			LastPing:   now,
		},
		"stale": {
			ID:         "stale",
			PID:        999999,
			Port:       54321,
			WorkingDir: "/tmp/project-b",
			StartTime:  now.Add(-10 * time.Minute),
			LastPing:   now.Add(-1 * time.Hour),
		},
	}
	writeJSONFile(t, filepath.Join(getLeditConfigDir(), "instances.json"), instances)
	writeJSONFile(t, filepath.Join(getLeditConfigDir(), "webui_host.json"), webUIHostRecordDTO{
		PID:       currentPID,
		Port:      54321,
		StartedAt: now.Add(-2 * time.Minute),
		UpdatedAt: now,
	})
	writeJSONFile(t, filepath.Join(getLeditConfigDir(), "webui_desired_host.json"), desiredHostRecordDTO{
		PID:       currentPID,
		UpdatedAt: now,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
	w := httptest.NewRecorder()

	server := &ReactWebServer{}
	server.handleAPIInstances(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var response struct {
		Instances      []instanceInfoDTO `json:"instances"`
		ActiveHostPID  int               `json:"active_host_pid"`
		ActiveHostPort int               `json:"active_host_port"`
		DesiredHostPID int               `json:"desired_host_pid"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(response.Instances) != 1 {
		t.Fatalf("expected 1 active instance, got %d", len(response.Instances))
	}
	if response.Instances[0].PID != currentPID {
		t.Fatalf("expected instance PID %d, got %d", currentPID, response.Instances[0].PID)
	}
	if !response.Instances[0].IsHost {
		t.Fatalf("expected active instance to be host")
	}
	if response.ActiveHostPID != currentPID {
		t.Fatalf("expected active host pid %d, got %d", currentPID, response.ActiveHostPID)
	}
	if response.ActiveHostPort != 54321 {
		t.Fatalf("expected active host port 54321, got %d", response.ActiveHostPort)
	}
	if response.DesiredHostPID != currentPID {
		t.Fatalf("expected desired host pid %d, got %d", currentPID, response.DesiredHostPID)
	}
}

func TestHandleAPIInstanceSelectWritesDesiredHost(t *testing.T) {
	t.Setenv("LEDIT_CONFIG", t.TempDir())
	pid := os.Getpid()

	payload := []byte(`{"pid":` + itoaForTest(pid) + `}`)
	req := httptest.NewRequest(http.MethodPost, "/api/instances/select", bytes.NewReader(payload))
	w := httptest.NewRecorder()

	server := &ReactWebServer{}
	server.handleAPIInstanceSelect(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	data, err := os.ReadFile(filepath.Join(getLeditConfigDir(), "webui_desired_host.json"))
	if err != nil {
		t.Fatalf("failed to read desired host file: %v", err)
	}
	var desired desiredHostRecordDTO
	if err := json.Unmarshal(data, &desired); err != nil {
		t.Fatalf("failed to decode desired host file: %v", err)
	}
	if desired.PID != pid {
		t.Fatalf("expected desired pid %d, got %d", pid, desired.PID)
	}
}

func TestHandleAPIInstanceSelectRejectsInvalidPID(t *testing.T) {
	t.Setenv("LEDIT_CONFIG", t.TempDir())

	req := httptest.NewRequest(http.MethodPost, "/api/instances/select", bytes.NewReader([]byte(`{"pid":0}`)))
	w := httptest.NewRecorder()

	server := &ReactWebServer{}
	server.handleAPIInstanceSelect(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestHandleAPIInstancesRejectsInvalidMethod(t *testing.T) {
	t.Setenv("LEDIT_CONFIG", t.TempDir())

	req := httptest.NewRequest(http.MethodPost, "/api/instances", nil)
	w := httptest.NewRecorder()

	server := &ReactWebServer{}
	server.handleAPIInstances(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", w.Code)
	}
}

func writeJSONFile(t *testing.T, path string, v interface{}) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal json: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
}

func itoaForTest(v int) string {
	if v == 0 {
		return "0"
	}
	negative := v < 0
	if negative {
		v = -v
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + (v % 10))
		v /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
