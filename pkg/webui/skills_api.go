//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/skills"
)

// GET /api/skills — list installed skills + their origin metadata.
func (ws *ReactWebServer) handleAPIListSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dir, err := skills.DefaultSkillsDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type skillJSON struct {
		ID          string        `json:"id"`
		Origin      skills.Origin `json:"origin"`
		InstalledAt string        `json:"installed_at,omitempty"`
		UpdatedAt   string        `json:"updated_at,omitempty"`
	}
	var out []skillJSON
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		installDir := filepath.Join(dir, e.Name())
		origin, _ := skills.LoadOrigin(installDir)
		ts := ""
		if !origin.InstalledAt.IsZero() {
			ts = origin.InstalledAt.Format(time.RFC3339)
		}
		out = append(out, skillJSON{
			ID:          e.Name(),
			Origin:      origin,
			InstalledAt: ts,
			UpdatedAt:   ts,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if out == nil {
		out = []skillJSON{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// GET /api/skills/registry — list registry entries (starter skills).
func (ws *ReactWebServer) handleAPIListRegistry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	reg, err := skills.LoadRegistry()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reg.Skills)
}

// POST /api/skills/install
func (ws *ReactWebServer) handleAPIInstallSkill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var req struct {
		Source string `json:"source"`
		Ref    string `json:"ref,omitempty"`
		Force  bool   `json:"force,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Source == "" {
		http.Error(w, "source is required", http.StatusBadRequest)
		return
	}
	opts := skills.InstallOptions{Force: req.Force}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	var results []skills.InstallResult
	var err error
	switch {
	case strings.HasPrefix(req.Source, "http://"),
		strings.HasPrefix(req.Source, "https://"),
		strings.HasPrefix(req.Source, "git@"),
		strings.HasPrefix(req.Source, "ssh://"),
		strings.HasPrefix(req.Source, "git+ssh://"),
		strings.HasSuffix(req.Source, ".git"):
		results, err = skills.InstallFromGit(ctx, req.Source, req.Ref, opts)
	case strings.Contains(req.Source, "/"),
		strings.HasPrefix(req.Source, "."),
		filepath.IsAbs(req.Source):
		results, err = skills.InstallFromPath(req.Source, opts)
	default:
		results, err = skills.InstallFromRegistry(ctx, req.Source, opts)
	}
	if err != nil {
		log.Printf("handleAPIInstallSkill: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Hot-reload: make the newly installed skill discoverable via list_skills
	// without requiring a sprout restart.
	if ws.agent != nil {
		if refreshErr := ws.agent.RefreshSkills(); refreshErr != nil {
			log.Printf("[skills] installed but config reload failed: %v", refreshErr)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(results)
}

// POST /api/skills/update
func (ws *ReactWebServer) handleAPIUpdateSkill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	results, err := skills.Update(ctx, req.ID, skills.InstallOptions{Force: true})
	if err != nil {
		log.Printf("handleAPIUpdateSkill: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Hot-reload: pick up any frontmatter changes from the updated skill.
	if ws.agent != nil {
		if refreshErr := ws.agent.RefreshSkills(); refreshErr != nil {
			log.Printf("[skills] updated but config reload failed: %v", refreshErr)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(results)
}

// POST /api/skills/remove
func (ws *ReactWebServer) handleAPIRemoveSkill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	if err := skills.Uninstall(req.ID); err != nil {
		log.Printf("handleAPIRemoveSkill: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Hot-reload: remove the uninstalled skill from the active config so it
	// no longer appears in list_skills without a restart.
	if ws.agent != nil {
		if refreshErr := ws.agent.RefreshSkills(); refreshErr != nil {
			log.Printf("[skills] removed but config reload failed: %v", refreshErr)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "removed", "id": req.ID})
}

// Dispatcher: /api/skills/<sub>
func (ws *ReactWebServer) handleAPISkillsRoutes(w http.ResponseWriter, r *http.Request) {
	sub := strings.TrimPrefix(r.URL.Path, "/api/skills/")
	sub = strings.TrimSuffix(sub, "/")
	switch sub {
	case "registry":
		ws.handleAPIListRegistry(w, r)
	case "install":
		ws.handleAPIInstallSkill(w, r)
	case "update":
		ws.handleAPIUpdateSkill(w, r)
	case "remove":
		ws.handleAPIRemoveSkill(w, r)
	case "":
		ws.handleAPIListSkills(w, r)
	default:
		http.NotFound(w, r)
	}
}
