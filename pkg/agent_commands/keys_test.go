package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/stretchr/testify/require"
)

func TestKeysCommand_Name(t *testing.T) {
	k := &KeysCommand{}
	if got := k.Name(); got != "keys" {
		t.Errorf("Name() = %q, want %q", got, "keys")
	}
}

func TestKeysCommand_Description(t *testing.T) {
	k := &KeysCommand{}
	desc := k.Description()
	if desc == "" {
		t.Error("Description() returned empty string")
	}
}

func TestKeysCommand_Usage(t *testing.T) {
	k := &KeysCommand{}
	usage := k.Usage()
	for _, want := range []string{"/keys", "/keys list", "/keys set", "/keys remove"} {
		if !strings.Contains(usage, want) {
			t.Errorf("Usage() missing %q\nGot: %s", want, usage)
		}
	}
}

func TestKeysCommand_Complete(t *testing.T) {
	k := &KeysCommand{}
	// No args: should suggest subcommands
	got := k.Complete([]string{}, nil)
	for _, want := range []string{"list", "set", "remove"} {
		found := false
		for _, s := range got {
			if s == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Complete() missing %q, got %v", want, got)
		}
	}
}

func TestGetCredentialStatus_BuiltInProvider(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}
	k := &KeysCommand{}
	// A provider that doesn't require keys (editor)
	status := k.getCredentialStatus(mgr, "editor")
	if status.missing {
		t.Errorf("editor should not require credentials, got missing=true (%s)", status.text)
	}
}

func TestKeysCommand_UsageSubcommandErrors(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}
	k := &KeysCommand{}

	// set without provider
	err := k.setKey(mgr, "", "test-key")
	if err == nil {
		t.Error("Expected error for empty provider")
	}

	// remove without provider
	err = k.removeKey(mgr, "")
	if err == nil {
		t.Error("Expected error for empty provider")
	}
}

func TestKeysCommand_SetUnknownProvider(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}
	k := &KeysCommand{}
	setErr := k.setKey(mgr, "nonexistent-provider-xyz", "test-key")
	if setErr == nil {
		t.Error("Expected error for unknown provider")
	}
	if !strings.Contains(setErr.Error(), "unknown provider") {
		t.Errorf("Error should mention 'unknown provider', got: %v", setErr)
	}
}

func TestKeysCommand_RemoveUnknownProvider(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}
	k := &KeysCommand{}
	rmErr := k.removeKey(mgr, "nonexistent-provider-xyz")
	if rmErr == nil {
		t.Error("Expected error for unknown provider")
	}
}

// SP-161f: the credential store is config-root scoped, and inside a git
// repo the root command repoints that root at <repo>/.sprout — so a key
// set in one repo is invisible from another. The success message must say
// where the key landed rather than let the user discover it by surprise.
func TestKeysCommand_SetReportsStorePath(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}

	var buf bytes.Buffer
	k := &KeysCommand{}
	k.SetOutput(&buf)

	if err := k.setKey(mgr, "openrouter", "test-key-for-unit-tests"); err != nil {
		t.Fatalf("setKey: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Stored in ") {
		t.Errorf("success message does not name the store path:\n%s", out)
	}
	if !strings.Contains(out, "api_keys.json") {
		t.Errorf("success message does not name the credential file:\n%s", out)
	}
}

// The user asked to be able to *choose* the scope, not just be told where
// the key went. --global/--local select the config root explicitly.
func TestParseKeyScope(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      []string
		wantScope keyScope
		wantRest  []string
	}{
		{"default", []string{"openrouter", "sk-x"}, keyScopeDefault, []string{"openrouter", "sk-x"}},
		{"global long", []string{"openrouter", "sk-x", "--global"}, keyScopeGlobal, []string{"openrouter", "sk-x"}},
		{"global short", []string{"-g", "openrouter", "sk-x"}, keyScopeGlobal, []string{"openrouter", "sk-x"}},
		{"local", []string{"openrouter", "sk-x", "--local"}, keyScopeLocal, []string{"openrouter", "sk-x"}},
		{"local short", []string{"-l", "openrouter", "sk-x"}, keyScopeLocal, []string{"openrouter", "sk-x"}},
		{"conflict", []string{"openrouter", "sk-x", "--global", "--local"}, keyScopeConflict, []string{"openrouter", "sk-x"}},
		{"conflict reversed", []string{"--local", "--global", "openrouter"}, keyScopeConflict, []string{"openrouter"}},
		{"repeat same flag is not a conflict", []string{"--global", "--global", "p"}, keyScopeGlobal, []string{"p"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope, rest := parseKeyScope(tc.args)
			require.Equal(t, tc.wantScope, scope)
			require.Equal(t, tc.wantRest, rest)
		})
	}
}

// --global must actually write to the user-level root, not the workspace
// root the process happens to have resolved. DefaultConfigDir() resolves
// XDG_CONFIG_HOME (ignoring SPROUT_CONFIG), so that is what we steer.
func TestKeysCommand_SetGlobalWritesUserLevelStore(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}

	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	var buf bytes.Buffer
	k := &KeysCommand{}
	k.SetOutput(&buf)

	if err := k.setKeyScoped(mgr, "openrouter", "test-key-for-unit-tests", keyScopeGlobal, ""); err != nil {
		t.Fatalf("setKeyScoped(global): %v", err)
	}

	out := buf.String()
	require.Contains(t, out, "Scope: user-level")
	require.Contains(t, out, "Stored in ")

	// The key must be readable from the global root's file store.
	store, err := credentials.LoadFromDir(filepath.Join(xdg, "sprout"))
	require.NoError(t, err)
	require.Equal(t, "test-key-for-unit-tests", store["openrouter"])
}

// --local must write to <workspaceRoot>/.sprout regardless of the resolved
// root, and must honour an explicit workspace root over the raw cwd.
func TestKeysCommand_SetLocalWritesWorkspaceStore(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}

	workspace := t.TempDir()
	// cwd deliberately elsewhere: the workspace root must win.
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	k := &KeysCommand{}
	k.SetOutput(&buf)

	if err := k.setKeyScoped(mgr, "openrouter", "test-key-for-unit-tests", keyScopeLocal, workspace); err != nil {
		t.Fatalf("setKeyScoped(local): %v", err)
	}

	out := buf.String()
	require.Contains(t, out, "Scope: workspace")
	require.Contains(t, out, filepath.Join(".sprout", "credentials", "api_keys.json"))

	store, err := credentials.LoadFromDir(filepath.Join(workspace, ".sprout"))
	require.NoError(t, err)
	require.Equal(t, "test-key-for-unit-tests", store["openrouter"])
}

// From a repo subdirectory the workspace store must be the repo root's
// .sprout, not <subdir>/.sprout — the latter is a store nothing else reads.
func TestWorkspaceConfigDir_WalksUpToRepoRoot(t *testing.T) {
	repo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
	sub := filepath.Join(repo, "a", "b")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Chdir(sub)

	got, err := workspaceConfigDir("")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(repo, ".sprout"), got)
}

// A git worktree or submodule has .git as a regular *file*, not a
// directory. The walk-up must still find the repo root.
func TestWorkspaceConfigDir_AcceptsGitFile(t *testing.T) {
	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: /elsewhere/.git/worktrees/x\n"), 0o644))
	sub := filepath.Join(repo, "pkg")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Chdir(sub)

	got, err := workspaceConfigDir("")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(repo, ".sprout"), got)
}

// The agent's resolved workspace root wins over cwd.
func TestWorkspaceConfigDir_PrefersExplicitRoot(t *testing.T) {
	root := t.TempDir()
	t.Chdir(t.TempDir())

	got, err := workspaceConfigDir(root)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, ".sprout"), got)
}

// $HOME has no workspace layer (a ~/.sprout layer is a documented footgun),
// so --local must refuse rather than create one.
func TestWorkspaceConfigDir_RefusesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := workspaceConfigDir(home)
	require.Error(t, err)
	require.Contains(t, err.Error(), "home directory")
}
