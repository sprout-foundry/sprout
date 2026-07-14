package commands

import (
	"strings"
	"testing"
)

func TestCustomCommand_Name(t *testing.T) {
	c := &CustomCommand{}
	if got := c.Name(); got != "custom" {
		t.Errorf("Name() = %q, want %q", got, "custom")
	}
}

func TestCustomCommand_Description(t *testing.T) {
	c := &CustomCommand{}
	if c.Description() == "" {
		t.Error("Description() returned empty string")
	}
}

func TestCustomCommand_Usage(t *testing.T) {
	c := &CustomCommand{}
	usage := c.Usage()
	for _, want := range []string{"/custom", "/custom list", "/custom add", "/custom remove"} {
		if !strings.Contains(usage, want) {
			t.Errorf("Usage() missing %q\nGot: %s", want, usage)
		}
	}
}

func TestCustomCommand_Complete(t *testing.T) {
	c := &CustomCommand{}
	// No args: should suggest subcommands
	got := c.Complete([]string{}, nil)
	for _, want := range []string{"list", "add", "remove"} {
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

func TestCustomCommand_Complete_RemoveName(t *testing.T) {
	c := &CustomCommand{}
	// `remove <TAB>` should return configured custom provider names
	// (or nil if config can't be loaded). We don't assert the names
	// themselves because they depend on the test config; we just want
	// to confirm Complete doesn't panic and returns a sensible type.
	got := c.Complete([]string{"remove", ""}, nil)
	if got == nil {
		// OK — could mean no providers configured or config load failed
		return
	}
	for _, name := range got {
		if name == "" {
			t.Error("Complete() returned an empty string in provider names")
		}
	}
}