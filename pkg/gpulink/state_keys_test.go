package gpulink

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// The link's keys live in the encrypted file store, which a background
// service can read without the OS keyring.
func TestKeysAreKeptInTheFileStore(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	credentials.ResetStorageBackend()
	t.Cleanup(credentials.ResetStorageBackend)

	where, err := SaveAPIKey("sfg_agent_key")
	if err != nil {
		t.Fatalf("SaveAPIKey: %v", err)
	}
	if where != "encrypted file store" {
		t.Fatalf("stored in %q, want the encrypted file store", where)
	}
	if v, err := credentials.NewFileBackend().Get(credentialKey); err != nil || v != "sfg_agent_key" {
		t.Fatalf("file store holds %q, %v", v, err)
	}
	got, err := LoadAPIKey()
	if err != nil || got != "sfg_agent_key" {
		t.Fatalf("LoadAPIKey = %q, %v", got, err)
	}
	if err := MoveKeysToFileStore(); err != nil {
		t.Fatalf("MoveKeysToFileStore with the key already in the file store: %v", err)
	}
}
