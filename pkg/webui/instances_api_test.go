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
			Port:       54000,
			WorkingDir: "/tmp/project-a",
			StartTime:  now.Add(-2 * time.Minute),
			LastPing:   now,
		},
		"stale": {
			ID:         "stale",
			PID:        999999,
			Port:       54000,
			WorkingDir: "/tmp/project-b",
			StartTime:  now.Add(-10 * time.Minute),
			LastPing:   now.Add(-1 * time.Hour),
		},
	}
	writeJSONFile(t, filepath.Join(getLeditConfigDir(), "instances.json"), instances)
	writeJSONFile(t, filepath.Join(getLeditConfigDir(), "webui_host.json"), webUIHostRecordDTO{
		PID:       currentPID,
		Port:      54000,
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
	if response.ActiveHostPort != 54000 {
		t.Fatalf("expected active host port 54000, got %d", response.ActiveHostPort)
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

func TestHandleAPISSHHostsReturnsParsedEntries(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0755); err != nil {
		t.Fatalf("failed to create ssh dir: %v", err)
	}
	config := `
Host devbox
  User alice
  HostName devbox.internal
  Port 2222

Host *.wildcard
  User ignored
`
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(config), 0644); err != nil {
		t.Fatalf("failed to write ssh config: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/instances/ssh-hosts", nil)
	w := httptest.NewRecorder()

	server := &ReactWebServer{}
	server.handleAPISSHHosts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var response struct {
		Hosts []sshHostEntryDTO `json:"hosts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(response.Hosts) != 1 {
		t.Fatalf("expected 1 parsed host, got %d", len(response.Hosts))
	}
	if response.Hosts[0].Alias != "devbox" {
		t.Fatalf("expected alias devbox, got %q", response.Hosts[0].Alias)
	}
	if response.Hosts[0].User != "alice" || response.Hosts[0].Hostname != "devbox.internal" || response.Hosts[0].Port != "2222" {
		t.Fatalf("unexpected host entry: %+v", response.Hosts[0])
	}
}

func TestHandleAPISSHOpenRejectsInvalidMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/instances/ssh-open", nil)
	w := httptest.NewRecorder()

	server := &ReactWebServer{}
	server.handleAPISSHOpen(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", w.Code)
	}
}

func TestHandleAPISSHOpenRejectsMissingAlias(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/instances/ssh-open", bytes.NewReader([]byte(`{"host_alias":""}`)))
	w := httptest.NewRecorder()

	server := &ReactWebServer{}
	server.handleAPISSHOpen(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}

	var payload sshLaunchErrorDTO
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode ssh-open error payload: %v", err)
	}
	if payload.Error == "" {
		t.Fatalf("expected structured error payload, got %+v", payload)
	}
}

func TestPersistedSSHSessionRegistryRoundTrip(t *testing.T) {
	t.Setenv("LEDIT_CONFIG", t.TempDir())

	session := &sshWorkspaceSession{
		Key:                 "devbox::$HOME",
		HostAlias:           "devbox",
		RemoteWorkspacePath: "$HOME",
		RemotePort:          55421,
		RemotePID:           4321,
		StartedAt:           time.Now().UTC().Truncate(time.Second),
	}

	if err := persistSSHSession(session); err != nil {
		t.Fatalf("persistSSHSession failed: %v", err)
	}

	persisted, err := readPersistedSSHSession(session.Key)
	if err != nil {
		t.Fatalf("readPersistedSSHSession failed: %v", err)
	}
	if persisted == nil {
		t.Fatalf("expected persisted session, got nil")
	}
	if persisted.HostAlias != session.HostAlias || persisted.RemoteWorkspacePath != session.RemoteWorkspacePath || persisted.RemotePort != session.RemotePort || persisted.RemotePID != session.RemotePID {
		t.Fatalf("unexpected persisted session: %+v", persisted)
	}

	if err := removePersistedSSHSession(session.Key); err != nil {
		t.Fatalf("removePersistedSSHSession failed: %v", err)
	}

	persisted, err = readPersistedSSHSession(session.Key)
	if err != nil {
		t.Fatalf("readPersistedSSHSession after delete failed: %v", err)
	}
	if persisted != nil {
		t.Fatalf("expected persisted session to be removed, got %+v", persisted)
	}
}

func TestHandleAPISSHSessionsReturnsPersistedEntries(t *testing.T) {
	t.Setenv("LEDIT_CONFIG", t.TempDir())

	if err := writePersistedSSHSessionRegistry(map[string]persistedSSHWorkspaceSession{
		"devbox::$HOME": {
			Key:                 "devbox::$HOME",
			HostAlias:           "devbox",
			RemoteWorkspacePath: "$HOME",
			RemotePort:          55421,
			RemotePID:           4321,
			StartedAt:           time.Now().UTC().Truncate(time.Second),
		},
	}); err != nil {
		t.Fatalf("writePersistedSSHSessionRegistry failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/instances/ssh-sessions", nil)
	w := httptest.NewRecorder()

	server := &ReactWebServer{sshSessions: make(map[string]*sshWorkspaceSession)}
	server.handleAPISSHSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var response struct {
		Sessions []sshSessionEntryDTO `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(response.Sessions) != 1 {
		t.Fatalf("expected 1 ssh session, got %d", len(response.Sessions))
	}
	if response.Sessions[0].HostAlias != "devbox" {
		t.Fatalf("expected host alias devbox, got %q", response.Sessions[0].HostAlias)
	}
}

func TestHandleAPISSHSessionDeleteRemovesPersistedEntry(t *testing.T) {
	t.Setenv("LEDIT_CONFIG", t.TempDir())

	if err := writePersistedSSHSessionRegistry(map[string]persistedSSHWorkspaceSession{
		"devbox::$HOME": {
			Key:                 "devbox::$HOME",
			HostAlias:           "devbox",
			RemoteWorkspacePath: "$HOME",
			RemotePort:          55421,
			RemotePID:           4321,
			StartedAt:           time.Now().UTC().Truncate(time.Second),
		},
	}); err != nil {
		t.Fatalf("writePersistedSSHSessionRegistry failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/instances/ssh-close", bytes.NewReader([]byte(`{"key":"devbox::$HOME"}`)))
	w := httptest.NewRecorder()

	server := &ReactWebServer{sshSessions: make(map[string]*sshWorkspaceSession)}
	server.handleAPISSHSessionDelete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	persisted, err := readPersistedSSHSession("devbox::$HOME")
	if err != nil {
		t.Fatalf("readPersistedSSHSession failed: %v", err)
	}
	if persisted != nil {
		t.Fatalf("expected persisted session to be deleted, got %+v", persisted)
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
