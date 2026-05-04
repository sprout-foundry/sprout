package webui

import (
	"testing"
)

func TestBrowseSSHDirectoryEmptyHost(t *testing.T) {
	_, _, _, err := browseSSHDirectory("", "$HOME")
	if err == nil {
		t.Fatal("expected error for empty host")
	}
	if err.Error() != "SSH host alias is required" {
		t.Fatalf("expected 'SSH host alias is required', got %q", err.Error())
	}
}

func TestBrowseSSHDirectoryWhitespaceHost(t *testing.T) {
	_, _, _, err := browseSSHDirectory("   ", "$HOME")
	if err == nil {
		t.Fatal("expected error for whitespace host")
	}
}
