//go:build !js

package proxy

import (
	"testing"

	config "github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestMergeServersNoOverrides(t *testing.T) {
	defaults := DefaultLanguageServers()
	defaultLen := len(defaults)

	// nil overrides
	result := MergeServers(defaults, nil)
	if len(result) != defaultLen {
		t.Fatalf("expected %d servers with nil overrides, got %d", defaultLen, len(result))
	}
	for i, s := range result {
		if s.ID != defaults[i].ID {
			t.Errorf("server[%d].ID = %q, want %q", i, s.ID, defaults[i].ID)
		}
	}

	// empty overrides
	result = MergeServers(defaults, []config.LanguageServerOverride{})
	if len(result) != defaultLen {
		t.Fatalf("expected %d servers with empty overrides, got %d", defaultLen, len(result))
	}
}

func TestMergeServersOverrideExisting(t *testing.T) {
	defaults := DefaultLanguageServers()

	overrides := []config.LanguageServerOverride{
		{
			ID:          "go",
			Binary:      "custom-gopls",
			Args:        []string{"--custom-flag"},
			LanguageIDs: []string{"go"},
			InstallHint: "custom install instructions",
		},
	}

	result := MergeServers(defaults, overrides)

	// Length should be unchanged
	if len(result) != len(defaults) {
		t.Fatalf("expected %d servers, got %d", len(defaults), len(result))
	}

	// Find the overridden go server
	var found *LanguageServerConfig
	for i, s := range result {
		if s.ID == "go" {
			found = &result[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected 'go' server to be present after override")
	}

	if found.Binary != "custom-gopls" {
		t.Errorf("expected binary 'custom-gopls', got %q", found.Binary)
	}
	if len(found.Args) != 1 || found.Args[0] != "--custom-flag" {
		t.Errorf("expected args ['--custom-flag'], got %v", found.Args)
	}
	if found.InstallHint != "custom install instructions" {
		t.Errorf("expected InstallHint 'custom install instructions', got %q", found.InstallHint)
	}
}

func TestMergeServersOverrideExistingEmptyInstallHint(t *testing.T) {
	defaults := DefaultLanguageServers()

	// Override without InstallHint should keep the default's
	overrides := []config.LanguageServerOverride{
		{
			ID:          "go",
			Binary:      "custom-gopls",
			Args:        []string{},
			LanguageIDs: []string{"go"},
			// InstallHint left empty
		},
	}

	result := MergeServers(defaults, overrides)

	// Find the overridden go server
	var found *LanguageServerConfig
	for i, s := range result {
		if s.ID == "go" {
			found = &result[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected 'go' server to be present after override")
	}

	// Should keep the default's install hint
	for _, d := range defaults {
		if d.ID == "go" {
			if found.InstallHint != d.InstallHint {
				t.Errorf("expected default InstallHint %q when override is empty, got %q", d.InstallHint, found.InstallHint)
			}
			break
		}
	}
}

func TestMergeServersAddNew(t *testing.T) {
	defaults := DefaultLanguageServers()
	defaultLen := len(defaults)

	// Add a brand-new server
	overrides := []config.LanguageServerOverride{
		{
			ID:          "my-custom-lang",
			Binary:      "my-lang-server",
			Args:        []string{"--stdio"},
			LanguageIDs: []string{"my-lang"},
		},
	}

	result := MergeServers(defaults, overrides)

	// Should have one more than defaults
	if len(result) != defaultLen+1 {
		t.Fatalf("expected %d servers, got %d", defaultLen+1, len(result))
	}

	// Find the new server
	var found *LanguageServerConfig
	for i, s := range result {
		if s.ID == "my-custom-lang" {
			found = &result[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected 'my-custom-lang' server to be appended")
	}

	if found.Binary != "my-lang-server" {
		t.Errorf("expected binary 'my-lang-server', got %q", found.Binary)
	}
	if found.InstallHint != "" {
		t.Errorf("new server should have empty InstallHint, got %q", found.InstallHint)
	}
}

func TestMergeServersMixed(t *testing.T) {
	defaults := DefaultLanguageServers()
	defaultLen := len(defaults)

	// Override "go" AND add "new-lang"
	overrides := []config.LanguageServerOverride{
		{
			ID:          "go",
			Binary:      "custom-gopls",
			Args:        []string{},
			LanguageIDs: []string{"go"},
		},
		{
			ID:          "new-lang",
			Binary:      "new-lang-server",
			Args:        []string{"--stdio"},
			LanguageIDs: []string{"new-lang"},
		},
	}

	result := MergeServers(defaults, overrides)

	// Should have one more than defaults (go is replaced, new-lang is added)
	if len(result) != defaultLen+1 {
		t.Fatalf("expected %d servers, got %d", defaultLen+1, len(result))
	}

	// Check go was overridden
	var goFound *LanguageServerConfig
	for i, s := range result {
		if s.ID == "go" {
			goFound = &result[i]
			break
		}
	}
	if goFound == nil {
		t.Fatal("expected 'go' server after override")
	}
	if goFound.Binary != "custom-gopls" {
		t.Errorf("expected overridden binary 'custom-gopls', got %q", goFound.Binary)
	}

	// Check new-lang was added
	var newFound *LanguageServerConfig
	for i, s := range result {
		if s.ID == "new-lang" {
			newFound = &result[i]
			break
		}
	}
	if newFound == nil {
		t.Fatal("expected 'new-lang' server to be appended")
	}
	if newFound.Binary != "new-lang-server" {
		t.Errorf("expected binary 'new-lang-server', got %q", newFound.Binary)
	}
}

func TestMergeServersNilDefaults(t *testing.T) {
	overrides := []config.LanguageServerOverride{
		{
			ID:          "custom",
			Binary:      "custom-server",
			Args:        []string{"--flag"},
			LanguageIDs: []string{"custom-lang"},
		},
	}

	result := MergeServers(nil, overrides)

	if len(result) != 1 {
		t.Fatalf("expected 1 server, got %d", len(result))
	}
	if result[0].ID != "custom" {
		t.Errorf("expected ID 'custom', got %q", result[0].ID)
	}
	if result[0].Binary != "custom-server" {
		t.Errorf("expected binary 'custom-server', got %q", result[0].Binary)
	}
}
