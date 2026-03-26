package webui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type instanceInfoDTO struct {
	ID         string    `json:"id"`
	PID        int       `json:"pid"`
	Port       int       `json:"port"`
	WorkingDir string    `json:"working_dir"`
	StartTime  time.Time `json:"start_time"`
	LastPing   time.Time `json:"last_ping"`
	SessionID  string    `json:"session_id,omitempty"`
	IsHost     bool      `json:"is_host"`
	IsCurrent  bool      `json:"is_current"`
}

type rawInstanceInfo struct {
	ID         string    `json:"id"`
	Port       int       `json:"port"`
	PID        int       `json:"pid"`
	StartTime  time.Time `json:"start_time"`
	WorkingDir string    `json:"working_dir"`
	LastPing   time.Time `json:"last_ping"`
	SessionID  string    `json:"session_id,omitempty"`
}

type webUIHostRecordDTO struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type desiredHostRecordDTO struct {
	PID       int       `json:"pid"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ws *ReactWebServer) handleAPIInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	instancesPath := filepath.Join(getLeditConfigDir(), "instances.json")
	hostPath := filepath.Join(getLeditConfigDir(), "webui_host.json")
	desiredPath := filepath.Join(getLeditConfigDir(), "webui_desired_host.json")

	instancesMap := map[string]rawInstanceInfo{}
	if data, err := os.ReadFile(instancesPath); err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &instancesMap)
	}

	hostRecord := webUIHostRecordDTO{}
	if data, err := os.ReadFile(hostPath); err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &hostRecord)
	}

	desiredPID := 0
	if data, err := os.ReadFile(desiredPath); err == nil && len(data) > 0 {
		var desired desiredHostRecordDTO
		if err := json.Unmarshal(data, &desired); err == nil {
			desiredPID = desired.PID
		}
	}

	instances := make([]instanceInfoDTO, 0, len(instancesMap))
	staleCutoff := time.Now().Add(-12 * time.Second)
	for _, instance := range instancesMap {
		if instance.PID <= 0 || instance.LastPing.Before(staleCutoff) || !isPIDAlive(instance.PID) {
			continue
		}
		instances = append(instances, instanceInfoDTO{
			ID:         instance.ID,
			PID:        instance.PID,
			Port:       instance.Port,
			WorkingDir: instance.WorkingDir,
			StartTime:  instance.StartTime,
			LastPing:   instance.LastPing,
			SessionID:  instance.SessionID,
			IsHost:     hostRecord.PID == instance.PID,
			IsCurrent:  instance.PID == os.Getpid(),
		})
	}

	sort.Slice(instances, func(i, j int) bool {
		return instances[i].StartTime.After(instances[j].StartTime)
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"instances":        instances,
		"current_pid":      os.Getpid(),
		"active_host_pid":  hostRecord.PID,
		"active_host_port": hostRecord.Port,
		"desired_host_pid": desiredPID,
	})
}

func (ws *ReactWebServer) handleAPIInstanceSelect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PID int `json:"pid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.PID <= 0 {
		http.Error(w, "pid is required", http.StatusBadRequest)
		return
	}
	if !isPIDAlive(req.PID) {
		http.Error(w, "selected instance is not alive", http.StatusBadRequest)
		return
	}

	if err := os.MkdirAll(getLeditConfigDir(), 0755); err != nil {
		http.Error(w, "Failed to prepare config dir", http.StatusInternalServerError)
		return
	}

	desired := desiredHostRecordDTO{PID: req.PID, UpdatedAt: time.Now()}
	data, err := json.MarshalIndent(desired, "", "  ")
	if err != nil {
		http.Error(w, "Failed to encode selection", http.StatusInternalServerError)
		return
	}

	tmp := filepath.Join(getLeditConfigDir(), "webui_desired_host.json.tmp")
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		http.Error(w, "Failed to write selection", http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmp, filepath.Join(getLeditConfigDir(), "webui_desired_host.json")); err != nil {
		http.Error(w, "Failed to apply selection", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "instance selection updated",
		"pid":     req.PID,
	})
}

func getLeditConfigDir() string {
	if dir := strings.TrimSpace(os.Getenv("LEDIT_CONFIG")); dir != "" {
		return dir
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "ledit")
	}
	homeDir := strings.TrimSpace(os.Getenv("HOME"))
	if homeDir == "" {
		return "/data/data/com.termux/files/home/.ledit"
	}
	return filepath.Join(homeDir, ".ledit")
}
