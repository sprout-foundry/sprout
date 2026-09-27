//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
	"github.com/sprout-foundry/sprout/pkg/utils/pidalive"
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

type sshHostEntryDTO struct {
	Alias    string `json:"alias"`
	Hostname string `json:"hostname,omitempty"`
	User     string `json:"user,omitempty"`
	Port     string `json:"port,omitempty"`
}

type sshLaunchRequestDTO struct {
	HostAlias           string `json:"host_alias"`
	RemoteWorkspacePath string `json:"remote_workspace_path,omitempty"`
}

type sshBrowseRequestDTO struct {
	HostAlias string `json:"host_alias"`
	Path      string `json:"path,omitempty"`
}

type sshSessionEntryDTO struct {
	Key                 string    `json:"key"`
	HostAlias           string    `json:"host_alias"`
	RemoteWorkspacePath string    `json:"remote_workspace_path"`
	LocalPort           int       `json:"local_port,omitempty"`
	RemotePort          int       `json:"remote_port"`
	RemotePID           int       `json:"remote_pid,omitempty"`
	URL                 string    `json:"url,omitempty"`
	StartedAt           time.Time `json:"started_at"`
	Active              bool      `json:"active"`
}

type sshLaunchErrorDTO struct {
	Error   string `json:"error"`
	Step    string `json:"step,omitempty"`
	Details string `json:"details,omitempty"`
	LogPath string `json:"log_path,omitempty"`
}

type sshLaunchStatusDTO struct {
	Key        string    `json:"key"`
	Step       string    `json:"step"`
	Status     string    `json:"status"`
	InProgress bool      `json:"in_progress"`
	LastError  string    `json:"last_error,omitempty"`
	Details    string    `json:"details,omitempty"`
	LogPath    string    `json:"log_path,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
	// ProxyBase, ProxyURL, and LocalPort are non-empty/non-zero when the launch
	// has completed successfully (in_progress=false, last_error="").
	ProxyBase string `json:"proxy_base,omitempty"`
	ProxyURL  string `json:"proxy_url,omitempty"`
	LocalPort int    `json:"local_port,omitempty"`
}

func (ws *ReactWebServer) handleAPIInstances(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	instancesPath := filepath.Join(getSproutStateDir(), "instances.json")
	hostPath := filepath.Join(getSproutStateDir(), "webui_host.json")
	desiredPath := filepath.Join(getSproutStateDir(), "webui_desired_host.json")

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
		if instance.PID <= 0 || instance.LastPing.Before(staleCutoff) || !pidalive.IsAlive(instance.PID) {
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"instances":        instances,
		"current_pid":      os.Getpid(),
		"active_host_pid":  hostRecord.PID,
		"active_host_port": hostRecord.Port,
		"desired_host_pid": desiredPID,
	})
}

func (ws *ReactWebServer) handleAPIInstanceSelect(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		PID int `json:"pid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}
	if req.PID <= 0 {
		writeJSONErr(w, http.StatusBadRequest, "pid_required", "pid is required")
		return
	}
	if !pidalive.IsAlive(req.PID) {
		writeJSONErr(w, http.StatusBadRequest, "selected_instance_not_alive", "selected instance is not alive")
		return
	}

	if err := os.MkdirAll(getSproutStateDir(), 0755); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_prepare_config_dir", "Failed to prepare config dir")
		return
	}

	desired := desiredHostRecordDTO{PID: req.PID, UpdatedAt: time.Now()}
	data, err := json.MarshalIndent(desired, "", "  ")
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_encode_selection", "Failed to encode selection")
		return
	}

	tmp := filepath.Join(getSproutStateDir(), "webui_desired_host.json.tmp")
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_write_selection", "Failed to write selection")
		return
	}
	if err := os.Rename(tmp, filepath.Join(getSproutStateDir(), "webui_desired_host.json")); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_apply_selection", "Failed to apply selection")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "instance selection updated",
		"pid":     req.PID,
	})
}

func getSproutStateDir() string {
	// Resolve config dir for instances.json — this is state data, not config.
	// Use StateDir so it follows the $SPROUT_STATE_DIR → XDG → HOME chain.
	if dir, err := envutil.StateDir(); err == nil {
		return dir
	}
	// Fallback: Termux home
	return filepath.Join("/data/data/com.termux/files/home", ".local", "state", "sprout")
}
