package skills

import (
	"strings"
	"testing"
)

// TestBuiltinsContainsKnownSkills locks in the set of skills the rest of
// the codebase depends on by ID. If a skill is renamed or removed,
// dependent surfaces (config defaults, CLI list, model-facing
// list_skills tool) all silently lose it — this test catches that.
func TestBuiltinsContainsKnownSkills(t *testing.T) {
	required := []string{
		"browse-debugging",
		"project-planning",
		"self-help",
		"workflow-automation",
		"static-site",
		"web-app",
		"web-app-data",
	}
	got := Builtins()
	for _, id := range required {
		if _, ok := got[id]; !ok {
			t.Errorf("Builtins() missing required skill %q", id)
		}
	}
}

// TestBuiltinsContainsStaticSiteStackSkill is the stack-skill discovery
// gate: the static-site starter's skill must be discoverable as a builtin
// (a directory under library/ with valid SKILL.md frontmatter). The
// starter's auto-activation path (pkg/agent) looks the skill up by the
// starter ID, so the ID must match the starter's id — "static-site".
func TestBuiltinsContainsStaticSiteStackSkill(t *testing.T) {
	got := Builtins()
	b, ok := got["static-site"]
	if !ok {
		t.Fatal("Builtins() missing the static-site stack skill")
	}
	if b.Name == "" {
		t.Error("static-site skill has an empty Name")
	}
	if b.Description == "" {
		t.Error("static-site skill has an empty Description")
	}
	if b.Path != LogicalPath+"/static-site" {
		t.Errorf("static-site Path = %q, want %q", b.Path, LogicalPath+"/static-site")
	}
}

// TestBuiltinsContainsWebAppStackSkill is the counterpart discovery gate
// for the web-app starter: its skill must be a builtin whose ID matches
// the starter's id — "web-app" — because the starter's auto-activation
// path (pkg/agent) looks the skill up by that ID.
func TestBuiltinsContainsWebAppStackSkill(t *testing.T) {
	got := Builtins()
	b, ok := got["web-app"]
	if !ok {
		t.Fatal("Builtins() missing the web-app stack skill")
	}
	if b.Name == "" {
		t.Error("web-app skill has an empty Name")
	}
	if b.Description == "" {
		t.Error("web-app skill has an empty Description")
	}
	if b.Path != LogicalPath+"/web-app" {
		t.Errorf("web-app Path = %q, want %q", b.Path, LogicalPath+"/web-app")
	}
}

// TestBuiltinsContainsWebAppDataStackSkill is the discovery gate for the
// web-app-data starter: its skill must be a builtin whose ID matches the
// starter's id — "web-app-data" — because the starter's auto-activation
// path (pkg/agent) looks the skill up by that ID.
func TestBuiltinsContainsWebAppDataStackSkill(t *testing.T) {
	got := Builtins()
	b, ok := got["web-app-data"]
	if !ok {
		t.Fatal("Builtins() missing the web-app-data stack skill")
	}
	if b.Name == "" {
		t.Error("web-app-data skill has an empty Name")
	}
	if b.Description == "" {
		t.Error("web-app-data skill has an empty Description")
	}
	if b.Path != LogicalPath+"/web-app-data" {
		t.Errorf("web-app-data Path = %q, want %q", b.Path, LogicalPath+"/web-app-data")
	}
}

// TestBuiltinsAllHaveValidMetadata is the structural gate: every
// shipped skill must have a parseable SKILL.md with non-empty name
// and description. A malformed skill would be silently skipped by
// Builtins() in production; this test fails the build instead.
func TestBuiltinsAllHaveValidMetadata(t *testing.T) {
	got := Builtins()
	if len(got) == 0 {
		t.Fatal("Builtins() returned empty — embedded library is missing or unparseable")
	}
	for id, b := range got {
		if b.ID != id {
			t.Errorf("skill %q: Builtin.ID = %q, want %q", id, b.ID, id)
		}
		if strings.TrimSpace(b.Name) == "" {
			t.Errorf("skill %q: empty Name", id)
		}
		if strings.TrimSpace(b.Description) == "" {
			t.Errorf("skill %q: empty Description", id)
		}
		if !strings.Contains(b.Content, "---") {
			t.Errorf("skill %q: Content missing frontmatter delimiter", id)
		}
		if b.Path == "" {
			t.Errorf("skill %q: empty Path", id)
		}
	}
}

// TestReadContentRejectsTraversal is the security boundary: activate_skill
// passes a model-supplied ID into ReadContent. A separator-bearing ID
// must be refused so the model can't pivot the skill loader into an
// arbitrary file read.
func TestReadContentRejectsTraversal(t *testing.T) {
	bad := []string{
		"../etc/passwd",
		"workflow-automation/../../etc",
		"foo/bar",
		"foo\\bar",
		"..",
		".",
		"",
	}
	for _, id := range bad {
		if _, err := ReadContent(id); err == nil {
			t.Errorf("ReadContent(%q) accepted, want rejection", id)
		}
	}
}

// TestReadContentRoundtrip ensures Builtins().Content and ReadContent()
// return identical bytes for the same ID. If these diverge the activation
// path (which calls ReadContent) and the registry (which reads
// Builtins) would show different content for the same skill.
func TestReadContentRoundtrip(t *testing.T) {
	for id, b := range Builtins() {
		got, err := ReadContent(id)
		if err != nil {
			t.Errorf("ReadContent(%q) error: %v", id, err)
			continue
		}
		if got != b.Content {
			t.Errorf("ReadContent(%q) drifted from Builtins()[%q].Content", id, id)
		}
	}
}

// TestIDsIsSorted is a small contract test — IDs() is documented as
// returning alphabetical order so the CLI `skill list` output is stable
// across runs.
func TestIDsIsSorted(t *testing.T) {
	ids := IDs()
	for i := 1; i < len(ids); i++ {
		if ids[i-1] > ids[i] {
			t.Errorf("IDs() not sorted: %v", ids)
			break
		}
	}
}
