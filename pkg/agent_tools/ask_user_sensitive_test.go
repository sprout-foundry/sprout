package tools

// Tests for the sensitive (credential) ask_user flow: the user's value must
// reach the credential store and the pending channel must carry only a
// masked confirmation — never the secret.

import (
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

func TestRespondToAskUser_SensitiveDivertsToCredentialStore(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir()+"/.sprout")
	credentials.ResetStorageBackend()
	t.Cleanup(func() {
		_ = credentials.DeleteFromActiveBackend("mcp/figma/FIGMA_TOKEN")
		credentials.ResetStorageBackend()
	})

	m := NewAskUserManager()
	m.SetTimeout(5 * time.Second)

	// Prime a sensitive pending entry directly: RespondToAskUser is the
	// unit under test (RequestAskUser's blocking/waiting machinery has its
	// own coverage).
	m.mu.Lock()
	ch := make(chan string, 1)
	m.pending["pending"] = ch
	m.sensitive = map[string]string{"pending": "mcp/figma/FIGMA_TOKEN"}
	m.mu.Unlock()

	if !m.RespondToAskUser("pending", "supersecret-token") {
		t.Fatal("expected respond to succeed")
	}

	got := <-ch
	if strings.Contains(got, "supersecret-token") {
		t.Fatalf("confirmation must not contain the secret, got %q", got)
	}
	if !strings.Contains(got, "mcp/figma/FIGMA_TOKEN") || !strings.Contains(got, "stored") {
		t.Errorf("confirmation should name the key and confirm storage, got %q", got)
	}

	stored, _, err := credentials.GetFromActiveBackend("mcp/figma/FIGMA_TOKEN")
	if err != nil {
		t.Fatalf("credential should be retrievable: %v", err)
	}
	if stored != "supersecret-token" {
		t.Errorf("stored value mismatch: got %q", stored)
	}
}

func TestRespondToAskUser_SensitiveEmptyValueStoresNothing(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir()+"/.sprout")
	credentials.ResetStorageBackend()
	t.Cleanup(func() {
		_ = credentials.DeleteFromActiveBackend("mcp/figma/FIGMA_TOKEN")
		credentials.ResetStorageBackend()
	})

	m := NewAskUserManager()
	ch := make(chan string, 1)
	m.mu.Lock()
	m.pending["r1"] = ch
	m.sensitive = map[string]string{"r1": "mcp/figma/FIGMA_TOKEN"}
	m.mu.Unlock()

	if !m.RespondToAskUser("r1", "   ") {
		t.Fatal("expected respond to succeed")
	}
	got := <-ch
	if !strings.Contains(got, "nothing was stored") {
		t.Errorf("empty value should report nothing stored, got %q", got)
	}
	if _, _, err := credentials.GetFromActiveBackend("mcp/figma/FIGMA_TOKEN"); err == nil {
		stored, _, _ := credentials.GetFromActiveBackend("mcp/figma/FIGMA_TOKEN")
		if stored != "" {
			t.Error("empty value must not store a credential")
		}
	}
}

func TestRespondToAskUser_NonSensitivePassesThrough(t *testing.T) {
	m := NewAskUserManager()
	ch := make(chan string, 1)
	m.mu.Lock()
	m.pending["r2"] = ch
	m.mu.Unlock()

	if !m.RespondToAskUser("r2", "the blue button") {
		t.Fatal("expected respond to succeed")
	}
	if got := <-ch; got != "the blue button" {
		t.Errorf("non-sensitive response must pass through verbatim, got %q", got)
	}
}
