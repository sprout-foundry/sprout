package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestGetSkillManifestWithFrontMatter(t *testing.T) {
	content := `---
name: my-skill
description: A test skill
---
This is the body of the skill.`

	manifest, body, err := GetSkillManifest(content)
	if err != nil {
		t.Fatalf("GetSkillManifest returned error: %v", err)
	}

	if len(manifest) != 2 {
		t.Fatalf("manifest length = %d, want 2", len(manifest))
	}
	if manifest["name"] != "my-skill" {
		t.Errorf("manifest[name] = %q, want %q", manifest["name"], "my-skill")
	}
	if manifest["description"] != "A test skill" {
		t.Errorf("manifest[description] = %q, want %q", manifest["description"], "A test skill")
	}

	wantBody := "This is the body of the skill."
	if body != wantBody {
		t.Errorf("body = %q, want %q", body, wantBody)
	}
}

func TestGetSkillManifestWithoutFrontMatter(t *testing.T) {
	content := "Just a body with no front matter."

	manifest, body, err := GetSkillManifest(content)
	if err != nil {
		t.Fatalf("GetSkillManifest returned error: %v", err)
	}

	if len(manifest) != 0 {
		t.Errorf("manifest length = %d, want 0 (no front matter)", len(manifest))
	}

	// Without --- delimiters, lines are not collected into body (no body section opened)
	if body != "" {
		t.Errorf("body = %q, want empty (no --- delimiters to open body section)", body)
	}
}

func TestGetSkillManifestOnlyOpeningDelimiter(t *testing.T) {
	content := `---
This is not front matter, just content after an opening ---`

	// With only one ---, it opens the front-matter block but never closes it,
	// so everything after is treated as front-matter lines (key: value parsing)
	// and the body is empty.
	manifest, body, err := GetSkillManifest(content)
	if err != nil {
		t.Fatalf("GetSkillManifest returned error: %v", err)
	}

	// The line "This is not front matter..." is collected into frontMatterLines
	// but since it doesn't contain ":", it won't parse as a key-value pair
	if len(manifest) != 0 {
		t.Errorf("manifest length = %d, want 0 (no valid key: value pairs)", len(manifest))
	}

	if body != "" {
		t.Errorf("body = %q, want empty (no closing ---)", body)
	}
}

func TestGetSkillManifestEmptyFrontMatter(t *testing.T) {
	content := `---
---
Body after empty front matter.`

	manifest, body, err := GetSkillManifest(content)
	if err != nil {
		t.Fatalf("GetSkillManifest returned error: %v", err)
	}

	if len(manifest) != 0 {
		t.Errorf("manifest length = %d, want 0 (empty front matter)", len(manifest))
	}

	wantBody := "Body after empty front matter."
	if body != wantBody {
		t.Errorf("body = %q, want %q", body, wantBody)
	}
}

func TestGetSkillManifestMultiLineBody(t *testing.T) {
	content := `---
id: skill-1
---
Line one
Line two
Line three`

	manifest, body, err := GetSkillManifest(content)
	if err != nil {
		t.Fatalf("GetSkillManifest returned error: %v", err)
	}

	if manifest["id"] != "skill-1" {
		t.Errorf("manifest[id] = %q, want %q", manifest["id"], "skill-1")
	}

	wantBody := "Line one\nLine two\nLine three"
	if body != wantBody {
		t.Errorf("body = %q, want %q", body, wantBody)
	}
}

func TestResolveSkillPathAbsolute(t *testing.T) {
	// Absolute paths are returned as-is
	result := resolveSkillPath("/absolute/path/to/skill")
	if result != "/absolute/path/to/skill" {
		t.Errorf("resolveSkillPath(absolute) = %q, want %q", result, "/absolute/path/to/skill")
	}
}

func TestResolveSkillPathRelative(t *testing.T) {
	// Relative paths become absolute (joined with exe dir or cwd)
	result := resolveSkillPath("relative/path/to/skill")
	// The result should be an absolute path
	if result == "relative/path/to/skill" {
		t.Errorf("resolveSkillPath(relative) returned unchanged relative path; expected absolute")
	}
	// It should contain "relative/path/to/skill"
	if result[len(result)-len("relative/path/to/skill"):] != "relative/path/to/skill" {
		t.Errorf("resolveSkillPath(relative) = %q, should end with %q", result, "relative/path/to/skill")
	}
}

// TestLoadSkillFromDiskForCustomSkill is the regression test that ensures
// the pkg/skills refactor didn't break the user/project-skill activation
// path. The flow is:
//  1. Config has the custom skill registered (discovery runs at Load
//     time and writes Path = relative-to-cwd).
//  2. LoadSkill tries the embedded pkg/skills library first — misses,
//     because this ID isn't shipped.
//  3. LoadSkill falls back to disk via resolveSkillPath + os.ReadFile.
//  4. The returned SkillInfo carries the on-disk content and the
//     "project"/"user" source from the config metadata.
//
// If the fallback branch is removed or the embed lookup silently
// returns success for unknown IDs, this test fails loudly.
func TestLoadSkillFromDiskForCustomSkill(t *testing.T) {
	tmp := t.TempDir()
	skillRoot := filepath.Join(tmp, ".sprout", "skills", "my-custom")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatalf("mkdir skill root: %v", err)
	}
	body := "---\nname: My Custom\ndescription: A user-supplied test skill.\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origWd)

	cfg := &configuration.Config{
		Skills: map[string]configuration.Skill{
			"my-custom": {
				ID:          "my-custom",
				Name:        "My Custom",
				Description: "A user-supplied test skill.",
				Path:        filepath.Join(".sprout", "skills", "my-custom"),
				Enabled:     true,
				Metadata:    map[string]string{"source": "project"},
			},
		},
	}

	info, err := LoadSkill("my-custom", cfg)
	if err != nil {
		t.Fatalf("LoadSkill(custom): %v", err)
	}
	if info.ID != "my-custom" {
		t.Errorf("ID = %q, want my-custom", info.ID)
	}
	if info.Source != "project" {
		t.Errorf("Source = %q, want project", info.Source)
	}
	if info.Content != body {
		t.Errorf("Content = %q, want %q", info.Content, body)
	}
}

// TestLoadSkillBuiltinPrefersEmbeddedContent confirms the built-in path
// still wins for IDs that ship with the binary, even if a same-named
// SKILL.md exists in .sprout/skills/. This was the prior behaviour and
// must hold so that built-in semantics aren't silently overridable.
func TestLoadSkillBuiltinPrefersEmbeddedContent(t *testing.T) {
	tmp := t.TempDir()
	override := filepath.Join(tmp, ".sprout", "skills", "self-help")
	if err := os.MkdirAll(override, 0o755); err != nil {
		t.Fatalf("mkdir override: %v", err)
	}
	overrideBody := "---\nname: HIJACKED\ndescription: override.\n---\noverride body"
	if err := os.WriteFile(filepath.Join(override, "SKILL.md"), []byte(overrideBody), 0o644); err != nil {
		t.Fatalf("write override SKILL.md: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origWd)

	cfg := &configuration.Config{
		Skills: map[string]configuration.Skill{
			"self-help": {
				ID:          "self-help",
				Name:        "Self-Help",
				Description: "Internal help and settings reference.",
				Path:        filepath.Join(".sprout", "skills", "self-help"),
				Enabled:     true,
				Metadata:    map[string]string{"source": "project"},
			},
		},
	}

	info, err := LoadSkill("self-help", cfg)
	if err != nil {
		t.Fatalf("LoadSkill(self-help): %v", err)
	}
	if info.Source != "builtin" {
		t.Errorf("Source = %q, want builtin (embedded library must win over disk override)", info.Source)
	}
	if info.Content == overrideBody {
		t.Errorf("Content matches the on-disk override; embedded built-in should have won")
	}
}
