//go:build !js

package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/skills"
)

// writeSkillMDForWebui writes a minimal valid SKILL.md to the given directory.
func writeSkillMDForWebui(t *testing.T, dir, name, description string) {
	t.Helper()
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n\nBody content.\n"
	if err := os.WriteFile(filepath.Join(dir, skills.SkillFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

func TestSkillsAPI_Integration_InstallListRemoveFlow(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmpDir)
	t.Setenv("HOME", tmpDir)

	// Create a local "registry repo" with one skill.
	repoDir := t.TempDir()
	skillSubdir := filepath.Join(repoDir, "skills", "webui-test")
	if err := os.MkdirAll(skillSubdir, 0o755); err != nil {
		t.Fatalf("mkdir skill subdir: %v", err)
	}
	writeSkillMDForWebui(t, skillSubdir, "webui-test", "WebUI integration test skill")

	// Inject test registry override.
	reg := &skills.Registry{
		Version: 1,
		Skills: []skills.RegistryEntry{
			{
				ID:          "webui-test",
				Name:        "WebUI Test",
				Description: "WebUI integration test skill",
				GitURL:      "file://" + repoDir,
				GitRef:      "main",
				PathInRepo:  "skills/webui-test",
			},
		},
	}
	skills.RegistryOverrideForTest(reg)
	defer skills.RegistryOverrideForTest(nil)

	ws := newSkillsWS()

	// 1. POST /api/skills/install with registry ID.
	body, _ := json.Marshal(map[string]string{"source": "webui-test"})
	req := httptest.NewRequest(http.MethodPost, "/api/skills/install", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleAPIInstallSkill(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("install: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. GET /api/skills — assert 200, 1 entry with correct id.
	req = httptest.NewRequest(http.MethodGet, "/api/skills", nil)
	rec = httptest.NewRecorder()
	ws.handleAPIListSkills(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var list []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list JSON: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 skill in list, got %d", len(list))
	}
	if list[0]["id"] != "webui-test" {
		t.Errorf("expected id=webui-test, got %v", list[0]["id"])
	}

	// 3. POST /api/skills/remove — assert 200.
	body, _ = json.Marshal(map[string]string{"id": "webui-test"})
	req = httptest.NewRequest(http.MethodPost, "/api/skills/remove", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	ws.handleAPIRemoveSkill(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("remove: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// 4. GET /api/skills again — assert empty list [].
	req = httptest.NewRequest(http.MethodGet, "/api/skills", nil)
	rec = httptest.NewRecorder()
	ws.handleAPIListSkills(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list after remove: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var emptyList []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &emptyList); err != nil {
		t.Fatalf("decode empty list JSON: %v", err)
	}
	if len(emptyList) != 0 {
		t.Errorf("expected empty list after remove, got %d entries", len(emptyList))
	}
}
