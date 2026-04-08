package webui

import (
	"bufio"
	"encoding/json"
	"errors"
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
	UpdatedAt  time.Time `json:"updated_at"`
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

// rejectIfSSHProxy rejects the request with 403 Forbidden if this server
// instance was reached via an SSH proxy tunnel.  This prevents nested SSH
// sessions (SSH-hopping), which would be confusing and error-prone.
func (ws *ReactWebServer) rejectIfSSHProxy(w http.ResponseWriter) bool {
	if ws.sshHostAlias != "" {
		writeSSHJSONError(w, http.StatusForbidden, sshLaunchErrorDTO{
			Error: "Cannot open SSH sessions from within an SSH proxy session",
		})
		return true
	}
	return false
}

func (ws *ReactWebServer) handleAPISSHHosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ws.rejectIfSSHProxy(w) {
		return
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, "Failed to determine home directory", http.StatusInternalServerError)
		return
	}

	hostsMap := make(map[string]*sshHostEntryDTO)
	parseSSHConfigFile(filepath.Join(homeDir, ".ssh", "config"), hostsMap, make(map[string]struct{}))

	hosts := make([]sshHostEntryDTO, 0, len(hostsMap))
	for _, host := range hostsMap {
		if host == nil {
			continue
		}
		hosts = append(hosts, *host)
	}
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].Alias < hosts[j].Alias
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"hosts": hosts,
	})
}

func (ws *ReactWebServer) handleAPISSHOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeSSHJSONError(w, http.StatusMethodNotAllowed, sshLaunchErrorDTO{Error: "Method not allowed"})
		return
	}

	if ws.rejectIfSSHProxy(w) {
		return
	}

	var req sshLaunchRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSSHJSONError(w, http.StatusBadRequest, sshLaunchErrorDTO{Error: "Invalid JSON"})
		return
	}

	result, err := ws.launchSSHWorkspace(req)
	if err != nil {
		payload := sshLaunchErrorDTO{Error: err.Error()}
		var launchErr *sshLaunchError
		if errors.As(err, &launchErr) {
			payload.Error = launchErr.Message
			payload.Step = launchErr.Step
			payload.Details = launchErr.Details
			payload.LogPath = launchErr.LogPath
		}
		writeSSHJSONError(w, http.StatusBadRequest, payload)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	proxyPath := result.ProxyBase + "/"
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":    "ssh workspace ready",
		"url":        result.URL,
		"port":       result.LocalPort,
		// Keep SSH navigation same-origin so PWA/service-worker/session storage
		// continue to work consistently on mobile browsers.
		"proxy_url":  proxyPath,
		"proxy_base": result.ProxyBase,
	})
}

func (ws *ReactWebServer) handleAPISSHLaunchStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeSSHJSONError(w, http.StatusMethodNotAllowed, sshLaunchErrorDTO{Error: "Method not allowed"})
		return
	}

	hostAlias := strings.TrimSpace(r.URL.Query().Get("host_alias"))
	if hostAlias == "" {
		writeSSHJSONError(w, http.StatusBadRequest, sshLaunchErrorDTO{Error: "host_alias is required"})
		return
	}

	remoteWorkspacePath := strings.TrimSpace(r.URL.Query().Get("remote_workspace_path"))
	if remoteWorkspacePath == "" {
		remoteWorkspacePath = "$HOME"
	}

	status := ws.getSSHLaunchStatus(hostAlias + "::" + remoteWorkspacePath)
	if status == nil {
		writeSSHJSONError(w, http.StatusNotFound, sshLaunchErrorDTO{Error: "No SSH launch status available"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sshLaunchStatusDTO{
		Key:        status.Key,
		Step:       status.Step,
		Status:     status.Status,
		InProgress: status.InProgress,
		LastError:  status.LastError,
		UpdatedAt:  status.UpdatedAt,
	})
}

func (ws *ReactWebServer) handleAPISSHBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeSSHJSONError(w, http.StatusMethodNotAllowed, sshLaunchErrorDTO{Error: "Method not allowed"})
		return
	}

	if ws.rejectIfSSHProxy(w) {
		return
	}

	var req sshBrowseRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSSHJSONError(w, http.StatusBadRequest, sshLaunchErrorDTO{Error: "Invalid JSON"})
		return
	}

	entries, resolvedPath, homePath, err := browseSSHDirectory(strings.TrimSpace(req.HostAlias), strings.TrimSpace(req.Path))
	if err != nil {
		writeSSHJSONError(w, http.StatusBadRequest, sshLaunchErrorDTO{Error: err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":   "ssh directory entries loaded",
		"path":      resolvedPath,
		"home_path": homePath,
		"files":     entries,
	})
}

func writeSSHJSONError(w http.ResponseWriter, status int, payload sshLaunchErrorDTO) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (ws *ReactWebServer) handleAPISSHSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ws.rejectIfSSHProxy(w) {
		return
	}

	sessions, err := ws.listSSHSessions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions": sessions,
	})
}

func (ws *ReactWebServer) handleAPISSHSessionDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ws.rejectIfSSHProxy(w) {
		return
	}

	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	if req.Key == "" {
		http.Error(w, "key is required", http.StatusBadRequest)
		return
	}

	if err := ws.closeSSHSession(req.Key); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "ssh session closed",
		"key":     req.Key,
	})
}

func parseSSHConfigFile(filePath string, hostsMap map[string]*sshHostEntryDTO, visited map[string]struct{}) {
	if strings.TrimSpace(filePath) == "" {
		return
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}
	if _, seen := visited[absPath]; seen {
		return
	}
	visited[absPath] = struct{}{}

	file, err := os.Open(absPath)
	if err != nil {
		return
	}
	defer file.Close()

	baseDir := filepath.Dir(absPath)
	scanner := bufio.NewScanner(file)
	currentAliases := []string{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		key := strings.ToLower(fields[0])
		value := strings.TrimSpace(line[len(fields[0]):])
		value = strings.TrimSpace(value)

		switch key {
		case "include":
			for _, pattern := range strings.Fields(value) {
				includePath := pattern
				if strings.HasPrefix(includePath, "~/") {
					if homeDir, homeErr := os.UserHomeDir(); homeErr == nil {
						includePath = filepath.Join(homeDir, includePath[2:])
					}
				} else if !filepath.IsAbs(includePath) {
					includePath = filepath.Join(baseDir, includePath)
				}

				matches, globErr := filepath.Glob(includePath)
				if globErr != nil || len(matches) == 0 {
					parseSSHConfigFile(includePath, hostsMap, visited)
					continue
				}
				for _, match := range matches {
					parseSSHConfigFile(match, hostsMap, visited)
				}
			}
		case "host":
			currentAliases = currentAliases[:0]
			for _, alias := range strings.Fields(value) {
				if alias == "" || strings.ContainsAny(alias, "*?!") {
					continue
				}
				currentAliases = append(currentAliases, alias)
				if _, exists := hostsMap[alias]; !exists {
					hostsMap[alias] = &sshHostEntryDTO{Alias: alias}
				}
			}
		case "hostname", "user", "port":
			if len(currentAliases) == 0 {
				continue
			}
			for _, alias := range currentAliases {
				entry := hostsMap[alias]
				if entry == nil {
					continue
				}
				switch key {
				case "hostname":
					if entry.Hostname == "" {
						entry.Hostname = value
					}
				case "user":
					if entry.User == "" {
						entry.User = value
					}
				case "port":
					if entry.Port == "" {
						entry.Port = value
					}
				}
			}
		}
	}
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
