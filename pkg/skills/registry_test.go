//go:build !js

package skills

import (
	"errors"
	"testing"
)

func TestLoadRegistry_Embedded(t *testing.T) {
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if reg == nil {
		t.Fatal("LoadRegistry returned nil")
	}
	if reg.Version != 1 {
		t.Errorf("Version = %d, want 1", reg.Version)
	}
	if len(reg.Skills) != 5 {
		t.Errorf("len(Skills) = %d, want 5", len(reg.Skills))
	}
}

func TestRegistry_LookupByID_Valid(t *testing.T) {
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	entry, err := reg.LookupByID("security-review")
	if err != nil {
		t.Fatalf("LookupByID(security-review): %v", err)
	}
	if entry.ID != "security-review" {
		t.Errorf("ID = %q, want %q", entry.ID, "security-review")
	}
	if entry.Name != "Security Review" {
		t.Errorf("Name = %q, want %q", entry.Name, "Security Review")
	}
}

func TestRegistry_LookupByID_Invalid(t *testing.T) {
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	_, err = reg.LookupByID("does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown ID")
	}
	if !errors.Is(err, ErrRegistryNotFound) {
		t.Errorf("expected ErrRegistryNotFound, got: %v", err)
	}
}

func TestRegistry_LookupByID_AllEntriesPresent(t *testing.T) {
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	for i := range reg.Skills {
		id := reg.Skills[i].ID
		entry, err := reg.LookupByID(id)
		if err != nil {
			t.Errorf("LookupByID(%q): %v", id, err)
			continue
		}
		if entry.ID != reg.Skills[i].ID {
			t.Errorf("LookupByID(%q).ID = %q, want %q", id, entry.ID, reg.Skills[i].ID)
		}
		if entry.Name != reg.Skills[i].Name {
			t.Errorf("LookupByID(%q).Name = %q, want %q", id, entry.Name, reg.Skills[i].Name)
		}
		if entry.Description != reg.Skills[i].Description {
			t.Errorf("LookupByID(%q).Description mismatch", id)
		}
	}
}
