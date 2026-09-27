//go:build !js

package webui

// instances_api_ssh.go — the SSH host/session launch endpoints, split out
// of instances_api.go. The handlers (hosts, open, launch-status, browse,
// sessions, session-delete) and the .ssh/config parser (parseSSHConfigFile)
// form the SSH-workspace-launch API surface; the shared SSH DTOs stay in
// instances_api.go.
import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

func (ws *ReactWebServer) handleAPISSHHosts(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_determine_home_directory", "Failed to determine home directory")
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"hosts": hosts,
	})
}

func (ws *ReactWebServer) handleAPISSHOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeSSHJSONError(w, http.StatusMethodNotAllowed, sshLaunchErrorDTO{Error: "Method not allowed"})
		return
	}

	var req sshLaunchRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSSHJSONError(w, http.StatusBadRequest, sshLaunchErrorDTO{Error: "Invalid JSON"})
		return
	}

	hostAlias := strings.TrimSpace(req.HostAlias)
	if hostAlias == "" {
		writeSSHJSONError(w, http.StatusBadRequest, sshLaunchErrorDTO{Error: "host_alias is required"})
		return
	}

	// Normalise the path here (mirrors launchSSHWorkspace) so we can return
	// the canonical session_key to the caller before launch starts.
	remoteWorkspacePath := strings.TrimSpace(req.RemoteWorkspacePath)
	if remoteWorkspacePath == "" {
		remoteWorkspacePath = "$HOME"
	}
	remoteWorkspacePath = normalizeRemoteWorkspacePath(remoteWorkspacePath)
	sessionKey := hostAlias + "::" + remoteWorkspacePath

	// Fire-and-forget: the launch runs in the background.  The caller polls
	// /api/instances/ssh-launch-status for progress and the final proxy URL.
	utils.SafeGo(ws.log(), "fire-and-forget SSH launch", func() {
		_, _ = ws.launchSSHWorkspace(req)
	})

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"message":     "ssh workspace launch started",
		"session_key": sessionKey,
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
	remoteWorkspacePath = normalizeRemoteWorkspacePath(remoteWorkspacePath)

	status := ws.getSSHLaunchStatus(hostAlias + "::" + remoteWorkspacePath)
	if status == nil {
		writeSSHJSONError(w, http.StatusNotFound, sshLaunchErrorDTO{Error: "No SSH launch status available"})
		return
	}

	writeJSON(w, http.StatusOK, sshLaunchStatusDTO{
		Key:        status.Key,
		Step:       status.Step,
		Status:     status.Status,
		InProgress: status.InProgress,
		LastError:  status.LastError,
		Details:    status.Details,
		LogPath:    status.LogPath,
		UpdatedAt:  status.UpdatedAt,
		ProxyBase:  status.ProxyBase,
		ProxyURL:   status.ProxyURL,
		LocalPort:  status.LocalPort,
	})
}

func (ws *ReactWebServer) handleAPISSHBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeSSHJSONError(w, http.StatusMethodNotAllowed, sshLaunchErrorDTO{Error: "Method not allowed"})
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":   "ssh directory entries loaded",
		"path":      resolvedPath,
		"home_path": homePath,
		"files":     entries,
	})
}

func writeSSHJSONError(w http.ResponseWriter, status int, payload sshLaunchErrorDTO) {
	writeJSON(w, status, payload)
}

func (ws *ReactWebServer) handleAPISSHSessions(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	sessions, err := ws.listSSHSessions()
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessions": sessions,
	})
}

func (ws *ReactWebServer) handleAPISSHSessionDelete(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	if req.Key == "" {
		writeJSONErr(w, http.StatusBadRequest, "key_required", "key is required")
		return
	}

	if err := ws.closeSSHSession(req.Key); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
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
