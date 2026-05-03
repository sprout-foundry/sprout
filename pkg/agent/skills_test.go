package agent

import "testing"

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
