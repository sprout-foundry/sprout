package gpulink

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// withTestConfig points sprout's config at a fresh temp dir for the test.
func withTestConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", dir)
	t.Setenv("SPROUT_CONFIG_DIR", dir)
	credentials.ResetStorageBackend()
	t.Cleanup(credentials.ResetStorageBackend)
	return dir
}

// The link's keys live in the encrypted file store, which a background
// service can read without the OS keyring.
func TestKeysAreKeptInTheFileStore(t *testing.T) {
	withTestConfig(t)

	where, err := SaveAPIKeyFor("box", "sfg_agent_key")
	if err != nil {
		t.Fatalf("SaveAPIKeyFor: %v", err)
	}
	if where != "encrypted file store" {
		t.Fatalf("stored in %q, want the encrypted file store", where)
	}
	if v, err := credentials.NewFileBackend().Get(AgentKeyName("box")); err != nil || v != "sfg_agent_key" {
		t.Fatalf("file store holds %q, %v", v, err)
	}
	if got, err := LoadAPIKeyFor("box"); err != nil || got != "sfg_agent_key" {
		t.Fatalf("LoadAPIKeyFor = %q, %v", got, err)
	}
	if err := MoveKeysToFileStore(); err != nil {
		t.Fatalf("MoveKeysToFileStore with the key already in the file store: %v", err)
	}
}

// Two links keep separate keys: one link's key never answers for another.
func TestKeysArePerLink(t *testing.T) {
	withTestConfig(t)

	if _, err := SaveAPIKeyFor("box", "key-box"); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveAPIKeyFor("box-8021", "key-8021"); err != nil {
		t.Fatal(err)
	}
	if err := SaveEngineKeyFor("box", "engine-box"); err != nil {
		t.Fatal(err)
	}
	if got, _ := LoadAPIKeyFor("box"); got != "key-box" {
		t.Fatalf("box key = %q", got)
	}
	if got, _ := LoadAPIKeyFor("box-8021"); got != "key-8021" {
		t.Fatalf("box-8021 key = %q", got)
	}
	if got := LoadEngineKeyFor("box"); got != "engine-box" {
		t.Fatalf("box engine key = %q", got)
	}
	if got := LoadEngineKeyFor("box-8021"); got != "" {
		t.Fatalf("box-8021 engine key = %q, want none", got)
	}
	if err := RemoveLink("box"); err != nil {
		t.Fatal(err)
	}
	if key, _ := LoadAPIKeyFor("box"); key != "" {
		t.Fatalf("box key survived RemoveLink (%q)", key)
	}
	if got, err := LoadAPIKeyFor("box-8021"); err != nil || got != "key-8021" {
		t.Fatalf("RemoveLink removed the other link's key: %q, %v", got, err)
	}
}

// A link saved by an older sprout (single gpu.json, single-named keys)
// migrates to the per-link layout on the first state read, and its service
// keys follow.
func TestLegacyStateMigrates(t *testing.T) {
	dir := withTestConfig(t)
	gpuDir := filepath.Join(dir, "gpu")
	if err := os.MkdirAll(gpuDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"platform_url": "https://platform.example", "link_id": "link-1",
		"name": "Inicion1", "engine": "127.0.0.1:8006", "fleet": true}`)
	if err := os.WriteFile(filepath.Join(gpuDir, "gpu.json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := saveSecret(legacyAgentKey, "sfg_agent_key"); err != nil {
		t.Fatal(err)
	}
	if _, err := saveSecret(legacyEngineKey, "engine-key"); err != nil {
		t.Fatal(err)
	}

	links, err := LoadLinks()
	if err != nil {
		t.Fatalf("LoadLinks: %v", err)
	}
	if len(links) != 1 || links[0].LinkID != "link-1" || !links[0].Fleet {
		t.Fatalf("links = %+v, %v", links, err)
	}
	if _, err := os.Stat(filepath.Join(gpuDir, "gpu.json")); !os.IsNotExist(err) {
		t.Fatalf("the legacy file is still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(gpuDir, "links", "Inicion1.json")); err != nil {
		t.Fatalf("the migrated state file is missing: %v", err)
	}
	if key, err := LoadAPIKeyFor("Inicion1"); err != nil || key != "sfg_agent_key" {
		t.Fatalf("migrated key = %q, %v", key, err)
	}
	if key := LoadEngineKeyFor("Inicion1"); key != "engine-key" {
		t.Fatalf("migrated engine key = %q", key)
	}
	// The legacy credential entries go once the keys have landed under the
	// link's own names.
	if v, err := credentials.NewFileBackend().Get(legacyAgentKey); err == nil && v != "" {
		t.Fatal("the legacy agent key entry is still in the file store")
	}
}

// LoadLink finds one link among several; an unknown slot comes back empty.
func TestLoadLinkAmongSeveral(t *testing.T) {
	withTestConfig(t)
	for _, name := range []string{"box", "box-8021"} {
		st := &State{LinkID: "link-" + name, Name: name, Engine: "127.0.0.1:8000"}
		if err := st.Save(); err != nil {
			t.Fatal(err)
		}
	}
	st, err := LoadLink("box-8021")
	if err != nil || st.LinkID != "link-box-8021" {
		t.Fatalf("LoadLink = %+v, %v", st, err)
	}
	st, err = LoadLink("nope")
	if err != nil || st.Linked() {
		t.Fatalf("LoadLink(nope) = %+v, %v; want unlinked", st, err)
	}
}

// Slot reduces names to file-safe forms and rejects nothing usable.
func TestSlot(t *testing.T) {
	cases := map[string]string{
		"Inicion1":      "Inicion1",
		"My Mac.pro":    "My-Mac.pro",
		"  pad  ":       "pad",
		"a/b\\c:d":      "a-b-c-d",
		"con":           "con-link",
		"COM1.json":     "COM1.json-link",
		"…":             "",
		"":              "",
		"gpu.box_2":     "gpu.box_2",
		"trailing--..-": "trailing",
	}
	for in, want := range cases {
		if got := Slot(in); got != want {
			t.Errorf("Slot(%q) = %q, want %q", in, got, want)
		}
	}
}

// DefaultLinkName stays the hostname, adds the engine's port for a second
// link on the same machine, and keeps counting rather than colliding.
func TestDefaultLinkName(t *testing.T) {
	host, err := os.Hostname()
	if err != nil || host == "" {
		t.Skip("no hostname on this machine")
	}
	links := func(names ...string) []*State {
		var out []*State
		for _, n := range names {
			out = append(out, &State{Name: n, LinkID: "x"})
		}
		return out
	}
	if got := DefaultLinkName(nil, "127.0.0.1:8021"); got != host {
		t.Fatalf("first link = %q, want the hostname %q", got, host)
	}
	if got := DefaultLinkName(links(host), "127.0.0.1:8021"); got != host+"-8021" {
		t.Fatalf("second link = %q, want %q", got, host+"-8021")
	}
	if got := DefaultLinkName(links(host, host+"-8021"), "sinter"); got != host+"-2" {
		t.Fatalf("third link = %q, want %q", got, host+"-2")
	}
	if got := DefaultLinkName(links(host), "127.0.0.1:11434"); got != host+"-11434" {
		t.Fatalf("ollama second = %q, want %q", got, host+"-11434")
	}
}
