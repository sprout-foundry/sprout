//go:build !js

package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/skills"
)

// writeSkillMDForTest writes a minimal valid SKILL.md to the given directory.
func writeSkillMDForTest(t *testing.T, dir, name string) {
	t.Helper()
	content := "---\nname: " + name + "\ndescription: A test skill\n---\n# Body\n"
	if err := os.WriteFile(filepath.Join(dir, skills.SkillFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

// newSkillsWS creates a minimal ReactWebServer for handler tests.
func newSkillsWS() *ReactWebServer {
	return &ReactWebServer{}
}

// ---------------------------------------------------------------------------
// GET /api/skills — list
// ---------------------------------------------------------------------------

func TestHandleAPIListSkills_Empty(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	ws := newSkillsWS()
	req := httptest.NewRequest(http.MethodGet, "/api/skills", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIListSkills(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Errorf("expected body '[]', got %q", body)
	}
	// Must be valid JSON
	var v []interface{}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Errorf("body is not valid JSON array: %v", err)
	}
}

func TestHandleAPIListSkills_WithInstalled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	// Install a skill first via the underlying API.
	srcDir := t.TempDir()
	writeSkillMDForTest(t, srcDir, "demo-skill")
	results, err := skills.InstallFromPath(srcDir, skills.InstallOptions{})
	if err != nil {
		t.Fatalf("InstallFromPath: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("no install results")
	}

	ws := newSkillsWS()
	req := httptest.NewRequest(http.MethodGet, "/api/skills", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIListSkills(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(got))
	}
	if got[0]["id"] != "demo-skill" {
		t.Errorf("id = %v, want demo-skill", got[0]["id"])
	}
	origin, ok := got[0]["origin"].(map[string]interface{})
	if !ok {
		t.Fatalf("origin missing or not object: %v", got[0]["origin"])
	}
	if origin["type"] != "path" {
		t.Errorf("origin.type = %v, want path", origin["type"])
	}
	if got[0]["installed_at"] == nil || got[0]["installed_at"] == "" {
		t.Errorf("installed_at missing: %v", got[0]["installed_at"])
	}
}

func TestHandleAPIListSkills_MethodNotAllowed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	ws := newSkillsWS()
	req := httptest.NewRequest(http.MethodPost, "/api/skills", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIListSkills(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/skills/registry
// ---------------------------------------------------------------------------

func TestHandleAPIListRegistry(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	ws := newSkillsWS()
	req := httptest.NewRequest(http.MethodGet, "/api/skills/registry", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIListRegistry(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 5 {
		t.Errorf("expected 5 registry entries, got %d", len(got))
	}
	for i, entry := range got {
		if _, ok := entry["id"].(string); !ok {
			t.Errorf("entry[%d]: id missing or not string: %v", i, entry)
		}
		if _, ok := entry["name"].(string); !ok {
			t.Errorf("entry[%d]: name missing or not string: %v", i, entry)
		}
		if _, ok := entry["description"].(string); !ok {
			t.Errorf("entry[%d]: description missing or not string: %v", i, entry)
		}
	}
}

func TestHandleAPIListRegistry_MethodNotAllowed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	ws := newSkillsWS()
	req := httptest.NewRequest(http.MethodPost, "/api/skills/registry", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIListRegistry(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/skills/install
// ---------------------------------------------------------------------------

func TestHandleAPIInstallSkill_FromPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	srcDir := t.TempDir()
	writeSkillMDForTest(t, srcDir, "path-installed-skill")

	ws := newSkillsWS()
	body, _ := json.Marshal(map[string]string{"source": srcDir})
	req := httptest.NewRequest(http.MethodPost, "/api/skills/install", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleAPIInstallSkill(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if got[0]["skill_id"] != "path-installed-skill" {
		t.Errorf("skill_id = %v, want path-installed-skill", got[0]["skill_id"])
	}
}

func TestHandleAPIInstallSkill_AlreadyInstalled_NoForce(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	srcDir := t.TempDir()
	writeSkillMDForTest(t, srcDir, "dup-skill")

	ws := newSkillsWS()
	body, _ := json.Marshal(map[string]string{"source": srcDir})

	// First install
	req := httptest.NewRequest(http.MethodPost, "/api/skills/install", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleAPIInstallSkill(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first install: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Second install (no force) → 400 with ErrAlreadyInstalled wrap
	body2, _ := json.Marshal(map[string]string{"source": srcDir})
	req2 := httptest.NewRequest(http.MethodPost, "/api/skills/install", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	ws.handleAPIInstallSkill(rec2, req2)

	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("second install: expected 400, got %d: %s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), "skill already installed") {
		t.Errorf("expected 'skill already installed' in body, got: %s", rec2.Body.String())
	}
}

func TestHandleAPIInstallSkill_WithForce(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	srcDir := t.TempDir()
	writeSkillMDForTest(t, srcDir, "force-skill")

	ws := newSkillsWS()
	body, _ := json.Marshal(map[string]interface{}{
		"source": srcDir,
		"force":  true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/skills/install", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleAPIInstallSkill(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first install: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Second install with force=true → 200
	body2, _ := json.Marshal(map[string]interface{}{
		"source": srcDir,
		"force":  true,
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/skills/install", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	ws.handleAPIInstallSkill(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("forced install: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

func TestHandleAPIInstallSkill_MissingSource(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	ws := newSkillsWS()
	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "/api/skills/install", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleAPIInstallSkill(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIInstallSkill_MethodNotAllowed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	ws := newSkillsWS()
	req := httptest.NewRequest(http.MethodGet, "/api/skills/install", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIInstallSkill(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/skills/update
// ---------------------------------------------------------------------------

func TestHandleAPIUpdateSkill_NotInstalled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	ws := newSkillsWS()
	body, _ := json.Marshal(map[string]string{"id": "nope"})
	req := httptest.NewRequest(http.MethodPost, "/api/skills/update", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleAPIUpdateSkill(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIUpdateSkill_PathOrigin_ErrNotGitOrigin(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	srcDir := t.TempDir()
	writeSkillMDForTest(t, srcDir, "path-update-skill")
	results, err := skills.InstallFromPath(srcDir, skills.InstallOptions{})
	if err != nil {
		t.Fatalf("InstallFromPath: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("no results")
	}
	skillID := results[0].SkillID

	ws := newSkillsWS()
	body, _ := json.Marshal(map[string]string{"id": skillID})
	req := httptest.NewRequest(http.MethodPost, "/api/skills/update", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleAPIUpdateSkill(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not a git") {
		t.Errorf("expected 'not a git' in body, got: %s", rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// POST /api/skills/remove
// ---------------------------------------------------------------------------

func TestHandleAPIRemoveSkill_NotInstalled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	ws := newSkillsWS()
	body, _ := json.Marshal(map[string]string{"id": "nope"})
	req := httptest.NewRequest(http.MethodPost, "/api/skills/remove", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleAPIRemoveSkill(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not installed") {
		t.Errorf("expected 'not installed' in body, got: %s", rec.Body.String())
	}
}

func TestHandleAPIRemoveSkill_MethodNotAllowed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	ws := newSkillsWS()
	req := httptest.NewRequest(http.MethodGet, "/api/skills/remove", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIRemoveSkill(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIRemoveSkill_Success(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	srcDir := t.TempDir()
	writeSkillMDForTest(t, srcDir, "removable-skill")
	results, err := skills.InstallFromPath(srcDir, skills.InstallOptions{})
	if err != nil {
		t.Fatalf("InstallFromPath: %v", err)
	}
	skillID := results[0].SkillID

	ws := newSkillsWS()
	body, _ := json.Marshal(map[string]string{"id": skillID})
	req := httptest.NewRequest(http.MethodPost, "/api/skills/remove", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleAPIRemoveSkill(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "removed" || resp["id"] != skillID {
		t.Errorf("unexpected response: %v", resp)
	}
}

// ---------------------------------------------------------------------------
// /api/skills/<sub> dispatcher
// ---------------------------------------------------------------------------

func TestHandleAPISkillsRoutes_UnknownSubpath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	ws := newSkillsWS()
	req := httptest.NewRequest(http.MethodGet, "/api/skills/bogus", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISkillsRoutes(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPISkillsRoutes_DispatchesToList(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	ws := newSkillsWS()
	req := httptest.NewRequest(http.MethodGet, "/api/skills/", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISkillsRoutes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Registry install via HTTP (uses test override to avoid network/git)
// ---------------------------------------------------------------------------

func TestHandleAPIInstallSkill_FromRegistry(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	// Create a local "registry repo" with one skill
	repoDir := t.TempDir()
	skillDir := filepath.Join(repoDir, "skills", "test-registry-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	writeSkillMDForTest(t, skillDir, "test-registry-skill")

	// Inject test registry override
	reg := &skills.Registry{
		Version: 1,
		Skills: []skills.RegistryEntry{
			{
				ID:          "test-registry-skill",
				Name:        "Test Registry Skill",
				Description: "A test registry skill",
				GitURL:      "file://" + repoDir,
				GitRef:      "main",
				PathInRepo:  "skills/test-registry-skill",
			},
		},
	}
	skills.RegistryOverrideForTest(reg)
	t.Cleanup(func() { skills.RegistryOverrideForTest(nil) })

	ws := newSkillsWS()
	body, _ := json.Marshal(map[string]string{"source": "test-registry-skill"})
	req := httptest.NewRequest(http.MethodPost, "/api/skills/install", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleAPIInstallSkill(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if got[0]["skill_id"] != "test-registry-skill" {
		t.Errorf("skill_id = %v, want test-registry-skill", got[0]["skill_id"])
	}
}

// ---------------------------------------------------------------------------
// Dispatcher: route to known sub-paths
// ---------------------------------------------------------------------------

func TestHandleAPISkillsRoutes_RoutesToRegistry(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)
	t.Setenv("HOME", tmp)

	ws := newSkillsWS()
	req := httptest.NewRequest(http.MethodGet, "/api/skills/registry", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISkillsRoutes(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var got []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 5 {
		t.Errorf("expected 5 registry entries, got %d", len(got))
	}
}
