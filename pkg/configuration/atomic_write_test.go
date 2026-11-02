package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestWorkspaceSave_OverlappingManagersNeverFailRename reproduces two config
// managers for one workspace saving at the same time (the daemon builds more
// than one agent per workspace at startup, and each persists its provider
// selection). With a shared fixed temp name, one writer's rename moved the
// other's temp file away and the second save failed with "rename
// workspace.json.tmp workspace.json: no such file or directory".
func TestWorkspaceSave_OverlappingManagersNeverFailRename(t *testing.T) {
	ws := filepath.Join(t.TempDir(), ConfigDirName)
	global := t.TempDir()

	const writers = 8
	const saves = 25
	managers := make([]*Manager, writers)
	for i := range managers {
		m, err := NewManagerWithLayers(global, ws)
		if err != nil {
			t.Fatal(err)
		}
		managers[i] = m
	}

	var wg sync.WaitGroup
	errs := make(chan error, writers*saves)
	start := make(chan struct{})
	for i, m := range managers {
		wg.Add(1)
		go func(i int, m *Manager) {
			defer wg.Done()
			<-start
			for j := 0; j < saves; j++ {
				label := fmt.Sprintf("provider-%d-%d", i, j)
				if err := m.UpdateConfig(func(c *Config) error {
					c.LastUsedProvider = label
					return nil
				}); err != nil {
					errs <- err
					return
				}
			}
		}(i, m)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("overlapping save failed: %v", err)
	}

	got := readJSONFile(t, filepath.Join(ws, WorkspaceConfigFileName))
	if p, _ := got["last_used_provider"].(string); !strings.HasPrefix(p, "provider-") {
		t.Errorf("workspace config lost the provider selection: %v", got["last_used_provider"])
	}
	assertNoTempFiles(t, ws)
}

// TestWriteFileAtomic_IgnoresStaleFixedTempFile pins that a leftover
// "<file>.tmp" from an interrupted writer neither blocks nor is consumed by
// a save.
func TestWriteFileAtomic_IgnoresStaleFixedTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, WorkspaceConfigFileName)
	stale := path + ".tmp"
	if err := os.WriteFile(stale, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte(`{"version":1}`), 0600); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != `{"version":1}` {
		t.Fatalf("destination = %q, %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("perm = %o, want 600", perm)
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("temp file left behind after save: %s", e.Name())
		}
	}
}
