package sandbox

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestDefaultDenyReadCoversCredentialStores(t *testing.T) {
	home := filepath.FromSlash("/home/someone")
	got := DefaultDenyRead(home)
	for _, rel := range []string{
		".ssh", ".aws", ".config/gh", ".config/gcloud", ".azure", ".kube",
		".docker/config.json", ".netrc", ".gnupg", ".password-store",
		"Library/Keychains", "Library/Cookies", "Library/Messages", "Library/Mail",
		"Library/Application Support/Google/Chrome", "Library/Application Support/Firefox",
		"Library/Safari", ".local/share/keyrings", ".mozilla",
		".config/google-chrome", ".config/chromium", ".config/sprout/credentials",
	} {
		want := filepath.Join(home, filepath.FromSlash(rel))
		if !slices.Contains(got, want) {
			t.Errorf("DefaultDenyRead missing %s", want)
		}
	}
}

func TestDefaultDenyReadKeepsNpmrcReadableForRegistryConfig(t *testing.T) {
	home := filepath.FromSlash("/home/someone")
	if slices.Contains(DefaultDenyRead(home), filepath.Join(home, ".npmrc")) {
		t.Fatal("~/.npmrc must stay readable: npm resolves registries from it")
	}
}

func TestDefaultDenyReadEmptyHome(t *testing.T) {
	if got := DefaultDenyRead(""); got != nil {
		t.Fatalf("DefaultDenyRead(\"\") = %v, want nil", got)
	}
}
