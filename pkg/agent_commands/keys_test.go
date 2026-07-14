package commands

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
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