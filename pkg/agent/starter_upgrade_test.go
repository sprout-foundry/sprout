//go:build !js

// starter_upgrade_test.go — the SP-153 §153d acceptance tests for starter
// upgrade proposals: when .sprout/starter.json names an older version of its
// starter than the embedded starter tree, the agent is told (an upgrade
// notice reaches the turn's context) and may propose the upgrade using the
// skill's upgrade note — but never applies it silently.
//
// The tests cover four levels:
//
//   - starterUpgradeNotice (the pure helper): a strictly older project
//     version yields the notice (starter id + both versions + the
//     propose/never-apply-silently instruction); an equal or newer version,
//     and any unparseable version, yield "".
//   - starterUpgradeNotice (agent method): over a temp project dir with the
//     embedded "fixture" starter, an older manifest version yields the
//     notice; an equal or newer version, no manifest, a corrupt manifest,
//     and an unknown starter id all yield "".
//   - no file changes: the mechanism is a pure reader — a project with a
//     stale manifest and content files is byte-identical after the
//     turn-start hooks run (the direct hook call and a full scripted turn).
//   - a scripted agent turn: the notice reaches the model's context (the
//     "system" message) when the manifest is stale, and an up-to-date
//     manifest leaves the context unchanged.
//
// The version fixtures are pinned against the embedded fixture starter tree
// (pkg/starters/data/fixture, version 0.1.0) — the "embedded version" side
// of the §153d comparison.

package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// The manifest fixtures below name the embedded "fixture" starter at
// different versions: 0.0.1 (older than the embedded 0.1.0 — the stale case),
// 0.1.0 (== embedded — up to date), and 9.9.9 (newer than embedded).
// suUnknownStarterManifestJSON names a starter with no embedded tree;
// suCorruptManifestJSON is not valid JSON at all.
const (
	suOlderManifestJSON          = `{"starter": {"id": "fixture", "version": "0.0.1"}}`
	suSameManifestJSON           = `{"starter": {"id": "fixture", "version": "0.1.0"}}`
	suNewerManifestJSON          = `{"starter": {"id": "fixture", "version": "9.9.9"}}`
	suUnknownStarterManifestJSON = `{"starter": {"id": "nonexistent", "version": "0.0.1"}}`
	suCorruptManifestJSON        = `{"starter": {`
	suNoticeMarker               = "Starter upgrade available"
	suNeverApplySilentlyMarker   = "NEVER apply it silently"
	suUpgradeNoteMarker          = "upgrade note"
	suExplicitApprovalMarker     = "explicitly approves"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// suWriteManifest writes content to .sprout/starter.json under root (creating
// the .sprout directory), so the manifest lives exactly where starterstore
// looks.
func suWriteManifest(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Dir(starterstore.StarterManifestPath(root))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create .sprout dir: %v", err)
	}
	if err := os.WriteFile(starterstore.StarterManifestPath(root), []byte(content), 0o644); err != nil {
		t.Fatalf("write starter manifest: %v", err)
	}
}

// suAgent wires client into a fresh agent whose workspace root is a project
// directory (the .sprout/ starter manifest lives under it). It mirrors the
// e2e agent setup from plan_context_test.go and stack_skill_activation_test.go:
// the full context profile keeps the tools on the roster, and SkipPrompt keeps
// the turn from ever blocking on an interactive prompt (stdin is closed in
// tests).
func suAgent(t *testing.T, client api.ClientInterface, workspaceRoot string) *Agent {
	t.Helper()

	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ContextMode = configuration.ContextModeFull
		cfg.SkipPrompt = true
		return nil
	}); err != nil {
		t.Fatalf("configure test agent: %v", err)
	}

	ag, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	t.Cleanup(ag.Shutdown)
	ag.SetMaxIterations(10)
	ag.SetWorkspaceRoot(workspaceRoot)
	return ag
}

// suFindSystemMessageWith reports whether any "system" message sent to the
// model (across all recorded requests) contains marker. The seed core sends
// the composed system prompt as the first "system" message, so the upgrade
// notice — when injected — is found here.
func suFindSystemMessageWith(client *ScriptedClient, marker string) bool {
	for _, msgs := range client.GetSentRequests() {
		for _, m := range msgs {
			if m.Role == "system" && strings.Contains(m.Content, marker) {
				return true
			}
		}
	}
	return false
}

// suSnapshot returns a sorted line per regular file in the project dir:
// "relative-path sha256-of-contents". Any file added, removed, or modified
// (including the manifest itself) shows up as a line difference, so two
// equal snapshots are proof the directory is byte-identical. Contents are
// read through an os.Root handle (the gosec G122 root-scoped API): the walk
// still enumerates via path, but each read opens the file relative to the
// root pinned when OpenRoot ran, so the path string is not re-resolved
// mid-walk (no symlink TOCTOU).
func suSnapshot(t *testing.T, root string) []string {
	t.Helper()
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("open project root: %v", err)
	}
	defer rootFS.Close()

	var out []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		data, readErr := rootFS.ReadFile(filepath.ToSlash(rel))
		if readErr != nil {
			return readErr
		}
		sum := sha256.Sum256(data)
		out = append(out, rel+" "+hex.EncodeToString(sum[:]))
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot project dir: %v", err)
	}
	sort.Strings(out)
	return out
}

// suAssertSnapshotsEqual fails the test with an added/removed report when the
// project dir changed between before and after.
func suAssertSnapshotsEqual(t *testing.T, label string, before, after []string) {
	t.Helper()
	if reflect.DeepEqual(before, after) {
		return
	}
	onlyBefore, onlyAfter := suDiffLines(before, after)
	t.Errorf("%s changed the project files (§153d: the notice is advisory-only, "+
		"no file changes) — removed/modified: %q; added/modified: %q",
		label, onlyBefore, onlyAfter)
}

// suDiffLines returns the lines present only in a (removed/modified before)
// and the lines present only in b (added/modified after).
func suDiffLines(a, b []string) (onlyA, onlyB []string) {
	inB := make(map[string]bool, len(b))
	for _, s := range b {
		inB[s] = true
	}
	for _, s := range a {
		if !inB[s] {
			onlyA = append(onlyA, s)
		}
	}
	inA := make(map[string]bool, len(a))
	for _, s := range a {
		inA[s] = true
	}
	for _, s := range b {
		if !inA[s] {
			onlyB = append(onlyB, s)
		}
	}
	return onlyA, onlyB
}

// ---------------------------------------------------------------------------
// Pure helper: starterUpgradeNotice
// ---------------------------------------------------------------------------

// TestStarterUpgradeNoticeHelper pins the notice builder: only a strictly
// older project version (semver) yields the notice, and the notice carries
// the starter id, both versions, the upgrade-note pointer, and the load-
// bearing propose/never-apply-silently instruction.
func TestStarterUpgradeNoticeHelper(t *testing.T) {
	tests := []struct {
		name        string
		starterID   string
		project     string
		embedded    string
		wantEmpty   bool
		wantContain []string
	}{
		{
			name:      "project older than embedded",
			starterID: "fixture",
			project:   "0.0.1",
			embedded:  "0.1.0",
			wantEmpty: false,
			wantContain: []string{
				"fixture", "0.0.1", "0.1.0",
				suUpgradeNoteMarker, "propose",
				suNeverApplySilentlyMarker, suExplicitApprovalMarker,
			},
		},
		{
			// semver accepts a leading v; the notice displays the raw
			// manifest string, not the normalized form.
			name:        "v-prefixed project version older than embedded",
			starterID:   "fixture",
			project:     "v0.0.1",
			embedded:    "0.1.0",
			wantEmpty:   false,
			wantContain: []string{"v0.0.1", "0.1.0", suNeverApplySilentlyMarker},
		},
		{
			// A pre-release is strictly older than its release (semver).
			name:        "pre-release older than release",
			starterID:   "fixture",
			project:     "1.0.0-alpha",
			embedded:    "1.0.0",
			wantEmpty:   false,
			wantContain: []string{"1.0.0-alpha", "1.0.0"},
		},
		{
			name:        "project equal to embedded",
			starterID:   "fixture",
			project:     "0.1.0",
			embedded:    "0.1.0",
			wantEmpty:   true,
			wantContain: nil,
		},
		{
			name:        "project newer than embedded",
			starterID:   "fixture",
			project:     "9.9.9",
			embedded:    "0.1.0",
			wantEmpty:   true,
			wantContain: nil,
		},
		{
			name:        "unparseable project version",
			starterID:   "fixture",
			project:     "not-a-version",
			embedded:    "0.1.0",
			wantEmpty:   true,
			wantContain: nil,
		},
		{
			name:        "unparseable embedded version",
			starterID:   "fixture",
			project:     "0.0.1",
			embedded:    "embedded?",
			wantEmpty:   true,
			wantContain: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := starterUpgradeNotice(tt.starterID, tt.project, tt.embedded)
			if tt.wantEmpty {
				if got != "" {
					t.Fatalf("got a notice, want empty:\n%s", got)
				}
				return
			}
			if got == "" {
				t.Fatalf("got empty notice, want the upgrade notice")
			}
			for _, want := range tt.wantContain {
				if !strings.Contains(got, want) {
					t.Errorf("notice missing %q, got:\n%s", want, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Agent method: starterUpgradeNotice
// ---------------------------------------------------------------------------

// TestStarterUpgradeNoticeMethod_OlderVersion is the core acceptance case: a
// manifest naming the "fixture" starter at 0.0.1 (older than the embedded
// 0.1.0) yields the upgrade notice.
func TestStarterUpgradeNoticeMethod_OlderVersion(t *testing.T) {
	root := t.TempDir()
	suWriteManifest(t, root, suOlderManifestJSON)

	ag := NewTestAgent()
	ag.SetWorkspaceRoot(root)

	notice := ag.starterUpgradeNotice()
	for _, want := range []string{
		"fixture", "0.0.1", "0.1.0",
		suUpgradeNoteMarker, suNeverApplySilentlyMarker, suExplicitApprovalMarker,
	} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice missing %q, got:\n%s", want, notice)
		}
	}
}

// TestStarterUpgradeNoticeMethod_SameVersion: a manifest at the embedded
// version has no upgrade to propose.
func TestStarterUpgradeNoticeMethod_SameVersion(t *testing.T) {
	root := t.TempDir()
	suWriteManifest(t, root, suSameManifestJSON)

	ag := NewTestAgent()
	ag.SetWorkspaceRoot(root)

	if got := ag.starterUpgradeNotice(); got != "" {
		t.Errorf("up-to-date manifest -> empty notice, got:\n%s", got)
	}
}

// TestStarterUpgradeNoticeMethod_NewerVersion: a manifest newer than the
// embedded tree has no upgrade to propose either.
func TestStarterUpgradeNoticeMethod_NewerVersion(t *testing.T) {
	root := t.TempDir()
	suWriteManifest(t, root, suNewerManifestJSON)

	ag := NewTestAgent()
	ag.SetWorkspaceRoot(root)

	if got := ag.starterUpgradeNotice(); got != "" {
		t.Errorf("newer manifest -> empty notice, got:\n%s", got)
	}
}

// TestStarterUpgradeNoticeMethod_NoManifest: a project without a starter
// manifest has nothing to compare — no notice, no error.
func TestStarterUpgradeNoticeMethod_NoManifest(t *testing.T) {
	root := t.TempDir() // no .sprout/starter.json

	ag := NewTestAgent()
	ag.SetWorkspaceRoot(root)

	if got := ag.starterUpgradeNotice(); got != "" {
		t.Errorf("no manifest -> empty notice, got:\n%s", got)
	}
}

// TestStarterUpgradeNoticeMethod_UnknownStarter: a manifest naming a starter
// with no embedded tree has no embedded version to compare against — no
// notice.
func TestStarterUpgradeNoticeMethod_UnknownStarter(t *testing.T) {
	root := t.TempDir()
	suWriteManifest(t, root, suUnknownStarterManifestJSON)

	ag := NewTestAgent()
	ag.SetWorkspaceRoot(root)

	if got := ag.starterUpgradeNotice(); got != "" {
		t.Errorf("unknown starter -> empty notice, got:\n%s", got)
	}
}

// TestStarterUpgradeNoticeMethod_CorruptManifest: an unreadable manifest is
// not an upgrade signal (logged, never fails the turn) — no notice.
func TestStarterUpgradeNoticeMethod_CorruptManifest(t *testing.T) {
	root := t.TempDir()
	suWriteManifest(t, root, suCorruptManifestJSON)

	ag := NewTestAgent()
	ag.SetWorkspaceRoot(root)

	if got := ag.starterUpgradeNotice(); got != "" {
		t.Errorf("corrupt manifest -> empty notice, got:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// No file changes: the mechanism is advisory-only
// ---------------------------------------------------------------------------

// suProjectTree writes a small project under root: the stale fixture manifest
// plus two content files (top level and nested).
func suProjectTree(t *testing.T, root string) {
	t.Helper()
	suWriteManifest(t, root, suOlderManifestJSON)
	if err := os.WriteFile(filepath.Join(root, "index.html"),
		[]byte("<html>fixture</html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	nested := filepath.Join(root, "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("create src dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "app.js"),
		[]byte("console.log('fixture');"), 0o644); err != nil {
		t.Fatalf("write src/app.js: %v", err)
	}
}

// TestStarterUpgradeNotice_NoFileChanges_TurnStartHooks drives the exact
// turn-start hooks prepareQueryRun runs for a stale manifest (the stack-skill
// auto-activation and the upgrade-notice computation) and asserts the
// project dir is byte-identical afterwards: the mechanism proposes, it never
// applies.
func TestStarterUpgradeNotice_NoFileChanges_TurnStartHooks(t *testing.T) {
	root := t.TempDir()
	suProjectTree(t, root)

	client := NewScriptedClient(NewScriptedTextResponse("ok."))
	ag := suAgent(t, client, root)

	before := suSnapshot(t, root)

	ag.autoActivateStarterSkill()
	notice := ag.starterUpgradeNotice()
	if notice == "" {
		t.Fatalf("a stale manifest must yield a notice (the no-change assertion " +
			"would be vacuous otherwise)")
	}

	suAssertSnapshotsEqual(t, "the turn-start hooks", before, suSnapshot(t, root))
}

// TestStarterUpgradeNotice_NoFileChanges_ScriptedTurn drives a full (scripted)
// turn over a stale manifest — so prepareQueryRun runs the whole turn-start
// path, including both hooks — and asserts the project dir is byte-identical
// afterwards.
func TestStarterUpgradeNotice_NoFileChanges_ScriptedTurn(t *testing.T) {
	root := t.TempDir()
	suProjectTree(t, root)

	client := NewScriptedClient(NewScriptedTextResponse("The fixture starter has an upgrade available."))
	ag := suAgent(t, client, root)

	before := suSnapshot(t, root)
	if _, err := ag.ProcessQuery("Continue with the fixture project."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	suAssertSnapshotsEqual(t, "a full scripted turn over a stale manifest", before, suSnapshot(t, root))
}

// ---------------------------------------------------------------------------
// Scripted agent turn: the notice reaches the model's context
// ---------------------------------------------------------------------------

// TestStarterUpgradeNotice_InjectedIntoTurnContext drives a real (scripted)
// turn over a stale manifest and asserts the upgrade notice — including the
// propose/never-apply-silently instruction — reaches the model's context
// (the "system" message).
func TestStarterUpgradeNotice_InjectedIntoTurnContext(t *testing.T) {
	root := t.TempDir()
	suWriteManifest(t, root, suOlderManifestJSON)

	client := NewScriptedClient(NewScriptedTextResponse("I see the fixture starter has an upgrade available."))
	ag := suAgent(t, client, root)
	if _, err := ag.ProcessQuery("Continue with the fixture project."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	if !suFindSystemMessageWith(client, suNoticeMarker) {
		t.Errorf("the upgrade notice must reach the model's context (system message)")
	}
	if !suFindSystemMessageWith(client, suNeverApplySilentlyMarker) {
		t.Errorf("the propose/never-apply-silently instruction must reach the model's context (system message)")
	}
	if !suFindSystemMessageWith(client, "0.0.1") {
		t.Errorf("the project version must reach the model's context (system message)")
	}
}

// TestStarterUpgradeNotice_UptodateManifestLeavesContextUnchanged drives a
// real (scripted) turn with a manifest at the embedded version and asserts no
// upgrade notice is injected — the context is unchanged in that respect.
func TestStarterUpgradeNotice_UptodateManifestLeavesContextUnchanged(t *testing.T) {
	root := t.TempDir()
	suWriteManifest(t, root, suSameManifestJSON)

	client := NewScriptedClient(NewScriptedTextResponse("Done."))
	ag := suAgent(t, client, root)
	if _, err := ag.ProcessQuery("Do the thing."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	if suFindSystemMessageWith(client, suNoticeMarker) {
		t.Errorf("an up-to-date manifest must NOT inject an upgrade notice")
	}
}
