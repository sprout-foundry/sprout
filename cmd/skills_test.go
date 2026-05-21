//go:build !js

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// testWorkingDir creates a temp dir, changes to it, and returns a cleanup func.
// Each test that calls runSkillsAllow/runSkillsRevoke/runSkillsList should use
// this to isolate file-system effects.
func testWorkingDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to %s: %v", dir, err)
	}
	return dir
}

// ---------------------------------------------------------------------------
// runSkillsAllow
// ---------------------------------------------------------------------------

func TestRunSkillsAllow_CreatesNewAllowlist(t *testing.T) {
	dir := testWorkingDir(t)

	// No allowlist exists yet
	err := runSkillsAllow([]string{"project-planning"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allowed := configuration.ReadAllowedSkills(dir)
	if allowed == nil {
		t.Fatal("expected allowlist to be created, got nil")
	}
	if !allowed["project-planning"] {
		t.Errorf("expected project-planning in allowlist, got %v", allowed)
	}
	if len(allowed) != 1 {
		t.Errorf("expected exactly 1 skill, got %d: %v", len(allowed), allowed)
	}
}

func TestRunSkillsAllow_AddMultipleSkills(t *testing.T) {
	dir := testWorkingDir(t)

	err := runSkillsAllow([]string{"project-planning", "browse-debugging", "repo-onboarding"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allowed := configuration.ReadAllowedSkills(dir)
	if allowed == nil {
		t.Fatal("expected allowlist to be created, got nil")
	}
	expected := []string{"project-planning", "browse-debugging", "repo-onboarding"}
	for _, id := range expected {
		if !allowed[id] {
			t.Errorf("expected %s in allowlist, got %v", id, allowed)
		}
	}
	if len(allowed) != len(expected) {
		t.Errorf("expected %d skills, got %d: %v", len(expected), len(allowed), allowed)
	}
}

func TestRunSkillsAllow_IdempotentForExistingSkill(t *testing.T) {
	dir := testWorkingDir(t)

	// Pre-seed the allowlist
	if err := configuration.WriteAllowedSkills(dir, []string{"project-planning"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Adding the same skill again should not error
	err := runSkillsAllow([]string{"project-planning"})
	if err != nil {
		t.Fatalf("unexpected error when adding existing skill: %v", err)
	}

	allowed := configuration.ReadAllowedSkills(dir)
	if len(allowed) != 1 {
		t.Errorf("expected 1 skill after idempotent add, got %d: %v", len(allowed), allowed)
	}
	if !allowed["project-planning"] {
		t.Error("expected project-planning to still be in allowlist")
	}
}

func TestRunSkillsAllow_AddToExistingAllowlist(t *testing.T) {
	dir := testWorkingDir(t)

	// Pre-seed with existing skills
	if err := configuration.WriteAllowedSkills(dir, []string{"project-planning"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Add new skills alongside existing ones
	err := runSkillsAllow([]string{"browse-debugging", "repo-onboarding"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allowed := configuration.ReadAllowedSkills(dir)
	if len(allowed) != 3 {
		t.Errorf("expected 3 skills, got %d: %v", len(allowed), allowed)
	}
	if !allowed["project-planning"] {
		t.Error("expected project-planning to still be present")
	}
	if !allowed["browse-debugging"] {
		t.Error("expected browse-debugging to be present")
	}
	if !allowed["repo-onboarding"] {
		t.Error("expected repo-onboarding to be present")
	}
}

func TestRunSkillsAllow_MixedNewAndExisting(t *testing.T) {
	dir := testWorkingDir(t)

	// Pre-seed with one skill
	if err := configuration.WriteAllowedSkills(dir, []string{"project-planning"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Add one new and one existing
	err := runSkillsAllow([]string{"project-planning", "browse-debugging"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allowed := configuration.ReadAllowedSkills(dir)
	if len(allowed) != 2 {
		t.Errorf("expected 2 skills, got %d: %v", len(allowed), allowed)
	}
}

func TestRunSkillsAllow_SkipsEmptyAndWhitespaceIDs(t *testing.T) {
	dir := testWorkingDir(t)

	err := runSkillsAllow([]string{"  ", "", "project-planning", "  browse-debugging  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allowed := configuration.ReadAllowedSkills(dir)
	if len(allowed) != 2 {
		t.Errorf("expected 2 skills (empty/whitespace skipped), got %d: %v", len(allowed), allowed)
	}
	// "  browse-debugging  " should be trimmed to "browse-debugging"
	if !allowed["browse-debugging"] {
		t.Error("expected trimmed browse-debugging in allowlist")
	}
	if !allowed["project-planning"] {
		t.Error("expected project-planning in allowlist")
	}
}

func TestRunSkillsAllow_AllEmptyIDsProducesMessage(t *testing.T) {
	dir := testWorkingDir(t)

	// All empty/whitespace IDs — should create an empty allowlist file but not error
	err := runSkillsAllow([]string{"", "  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The allowlist file is created with an empty map (not nil)
	allowed := configuration.ReadAllowedSkills(dir)
	if allowed == nil {
		t.Fatal("expected allowlist file to be created (empty map), got nil")
	}
	if len(allowed) != 0 {
		t.Errorf("expected 0 skills in allowlist, got %d: %v", len(allowed), allowed)
	}
}

func TestRunSkillsAllow_MixOfEmptyAndRealIDs(t *testing.T) {
	dir := testWorkingDir(t)

	err := runSkillsAllow([]string{"", "project-planning", "  ", "browse-debugging"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allowed := configuration.ReadAllowedSkills(dir)
	if len(allowed) != 2 {
		t.Errorf("expected 2 skills, got %d: %v", len(allowed), allowed)
	}
}

// ---------------------------------------------------------------------------
// runSkillsRevoke
// ---------------------------------------------------------------------------

func TestRunSkillsRevoke_RemovesExistingSkill(t *testing.T) {
	dir := testWorkingDir(t)

	if err := configuration.WriteAllowedSkills(dir, []string{"project-planning", "browse-debugging"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	err := runSkillsRevoke([]string{"project-planning"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allowed := configuration.ReadAllowedSkills(dir)
	if allowed == nil {
		t.Fatal("expected allowlist to still exist")
	}
	if allowed["project-planning"] {
		t.Error("expected project-planning to be removed")
	}
	if !allowed["browse-debugging"] {
		t.Error("expected browse-debugging to still be present")
	}
}

func TestRunSkillsRevoke_RemoveMultipleSkills(t *testing.T) {
	dir := testWorkingDir(t)

	if err := configuration.WriteAllowedSkills(dir, []string{"project-planning", "browse-debugging", "repo-onboarding"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	err := runSkillsRevoke([]string{"project-planning", "repo-onboarding"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allowed := configuration.ReadAllowedSkills(dir)
	if allowed == nil {
		t.Fatal("expected allowlist to still exist")
	}
	if len(allowed) != 1 {
		t.Errorf("expected 1 remaining skill, got %d: %v", len(allowed), allowed)
	}
	if !allowed["browse-debugging"] {
		t.Error("expected browse-debugging to still be present")
	}
}

func TestRunSkillsRevoke_SkillNotFound(t *testing.T) {
	dir := testWorkingDir(t)

	if err := configuration.WriteAllowedSkills(dir, []string{"project-planning"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Revoking a skill that doesn't exist should still succeed (just report not found)
	err := runSkillsRevoke([]string{"nonexistent-skill"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allowed := configuration.ReadAllowedSkills(dir)
	if len(allowed) != 1 {
		t.Errorf("expected 1 skill unchanged, got %d: %v", len(allowed), allowed)
	}
	if !allowed["project-planning"] {
		t.Error("expected project-planning to still be present")
	}
}

func TestRunSkillsRevoke_MixedFoundAndNotFound(t *testing.T) {
	dir := testWorkingDir(t)

	if err := configuration.WriteAllowedSkills(dir, []string{"project-planning", "browse-debugging"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	err := runSkillsRevoke([]string{"project-planning", "nonexistent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allowed := configuration.ReadAllowedSkills(dir)
	if len(allowed) != 1 {
		t.Errorf("expected 1 remaining skill, got %d: %v", len(allowed), allowed)
	}
	if !allowed["browse-debugging"] {
		t.Error("expected browse-debugging to still be present")
	}
}

func TestRunSkillsRevoke_NoAllowlistExists(t *testing.T) {
	testWorkingDir(t)

	// No allowlist file — should return error
	err := runSkillsRevoke([]string{"project-planning"})
	if err == nil {
		t.Fatal("expected error when no allowlist exists")
	}
	if !strings.Contains(err.Error(), "no skill allowlist is configured") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRunSkillsRevoke_SkipsEmptyAndWhitespaceIDs(t *testing.T) {
	dir := testWorkingDir(t)

	if err := configuration.WriteAllowedSkills(dir, []string{"project-planning", "browse-debugging"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	err := runSkillsRevoke([]string{"", "  ", "project-planning"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allowed := configuration.ReadAllowedSkills(dir)
	if len(allowed) != 1 {
		t.Errorf("expected 1 remaining skill, got %d: %v", len(allowed), allowed)
	}
	if !allowed["browse-debugging"] {
		t.Error("expected browse-debugging to still be present")
	}
}

func TestRunSkillsRevoke_AllEmptyIDs(t *testing.T) {
	dir := testWorkingDir(t)

	if err := configuration.WriteAllowedSkills(dir, []string{"project-planning"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	err := runSkillsRevoke([]string{"", "  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Nothing should be removed
	allowed := configuration.ReadAllowedSkills(dir)
	if len(allowed) != 1 {
		t.Errorf("expected 1 skill unchanged, got %d: %v", len(allowed), allowed)
	}
}

func TestRunSkillsRevoke_RemoveAllSkills(t *testing.T) {
	dir := testWorkingDir(t)

	if err := configuration.WriteAllowedSkills(dir, []string{"project-planning"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	err := runSkillsRevoke([]string{"project-planning"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The file still exists but should be empty (ReadAllowedSkills returns empty map, not nil)
	allowed := configuration.ReadAllowedSkills(dir)
	if allowed == nil {
		t.Fatal("expected allowlist file to still exist (empty map), got nil")
	}
	if len(allowed) != 0 {
		t.Errorf("expected 0 remaining skills, got %d: %v", len(allowed), allowed)
	}
}

// ---------------------------------------------------------------------------
// runSkillsList
// ---------------------------------------------------------------------------

func TestRunSkillsList_NoAllowlist(t *testing.T) {
	testWorkingDir(t)

	err := runSkillsList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not error; just prints "No skill allowlist configured"
}

func TestRunSkillsList_WithEntries(t *testing.T) {
	dir := testWorkingDir(t)

	if err := configuration.WriteAllowedSkills(dir, []string{"project-planning", "browse-debugging"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	err := runSkillsList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should succeed — output verification is done via the lack of error
	// The function just prints the list to stdout
}

func TestRunSkillsList_SingleEntry(t *testing.T) {
	dir := testWorkingDir(t)

	if err := configuration.WriteAllowedSkills(dir, []string{"project-planning"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	err := runSkillsList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSkillsList_EmptyAllowlistFile(t *testing.T) {
	dir := testWorkingDir(t)

	// Create an empty allowlist (zero skills)
	if err := configuration.WriteAllowedSkills(dir, []string{}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	err := runSkillsList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// An empty allowlist file exists (non-nil map with 0 entries),
	// so it should list zero allowed skills, not the "no allowlist" message.
	allowed := configuration.ReadAllowedSkills(dir)
	if allowed == nil {
		t.Fatal("expected allowlist file to exist")
	}
	if len(allowed) != 0 {
		t.Fatalf("expected 0 skills in empty allowlist, got %d", len(allowed))
	}
}

// ---------------------------------------------------------------------------
// integration-style round-trip tests
// ---------------------------------------------------------------------------

func TestRunSkillsAllow_Revoke_RoundTrip(t *testing.T) {
	dir := testWorkingDir(t)

	// Add skills
	if err := runSkillsAllow([]string{"project-planning", "browse-debugging"}); err != nil {
		t.Fatalf("allow failed: %v", err)
	}

	// Verify both present
	allowed := configuration.ReadAllowedSkills(dir)
	if len(allowed) != 2 {
		t.Fatalf("after allow: expected 2 skills, got %d", len(allowed))
	}

	// Revoke one
	if err := runSkillsRevoke([]string{"project-planning"}); err != nil {
		t.Fatalf("revoke failed: %v", err)
	}

	// Verify only browse-debugging remains
	allowed = configuration.ReadAllowedSkills(dir)
	if len(allowed) != 1 {
		t.Fatalf("after revoke: expected 1 skill, got %d", len(allowed))
	}
	if !allowed["browse-debugging"] {
		t.Error("expected browse-debugging to remain")
	}
}

func TestRunSkillsAllow_AllowList_Sequential(t *testing.T) {
	dir := testWorkingDir(t)

	// Add first batch
	if err := runSkillsAllow([]string{"project-planning"}); err != nil {
		t.Fatalf("first allow failed: %v", err)
	}

	// Add second batch
	if err := runSkillsAllow([]string{"browse-debugging"}); err != nil {
		t.Fatalf("second allow failed: %v", err)
	}

	// List (should not error)
	if err := runSkillsList(); err != nil {
		t.Fatalf("list failed: %v", err)
	}

	allowed := configuration.ReadAllowedSkills(dir)
	if len(allowed) != 2 {
		t.Errorf("expected 2 skills after sequential adds, got %d: %v", len(allowed), allowed)
	}
}

func TestRunSkillsAllow_WritesSortedOutput(t *testing.T) {
	dir := testWorkingDir(t)

	// Add in non-alphabetical order
	if err := runSkillsAllow([]string{"z-skill", "a-skill", "m-skill"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read raw file contents to verify sort order
	path := filepath.Join(dir, ".sprout", "allowed_skills")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	lines := strings.TrimSpace(string(data))
	// Should be: a-skill\nm-skill\nz-skill
	expected := "a-skill\nm-skill\nz-skill"
	if lines != expected {
		t.Errorf("expected sorted output %q, got %q", expected, lines)
	}
}

func TestRunSkillsRevoke_EmptyAllowlistFileAfterRemoveAll(t *testing.T) {
	dir := testWorkingDir(t)

	if err := configuration.WriteAllowedSkills(dir, []string{"project-planning"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	err := runSkillsRevoke([]string{"project-planning"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should still exist (empty), not be deleted
	path := filepath.Join(dir, ".sprout", "allowed_skills")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected allowlist file to still exist after removing all skills")
	}
}
