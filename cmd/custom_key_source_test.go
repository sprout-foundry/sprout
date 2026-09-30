//go:build !js

package cmd

import (
	"bufio"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/credentials"
)

func stubReadSecret(t *testing.T, value string) *int {
	t.Helper()
	calls := 0
	orig := readSecret
	readSecret = func(string) (string, error) {
		calls++
		return value, nil
	}
	t.Cleanup(func() { readSecret = orig })
	return &calls
}

func isolateCredentials(t *testing.T) {
	t.Helper()
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	credentials.ResetStorageBackend()
	if err := credentials.SetStorageMode("file"); err != nil {
		t.Fatalf("SetStorageMode: %v", err)
	}
	t.Cleanup(credentials.ResetStorageBackend)
}

func TestConfigureKeySource_DefaultsToPastedKey(t *testing.T) {
	calls := stubReadSecret(t, "  sk-pasted  ")
	provider := configuration.CustomProviderConfig{Name: "gw"}

	key, err := configureKeySource(bufio.NewReader(strings.NewReader("\n")), &provider)
	if err != nil {
		t.Fatal(err)
	}
	if key != "sk-pasted" || *calls != 1 {
		t.Fatalf("key = %q after %d hidden reads, want the trimmed pasted key from one hidden read", key, *calls)
	}
	if provider.EnvVar != "" || !provider.RequiresAPIKey {
		t.Fatalf("provider = %+v, want RequiresAPIKey without an env var", provider)
	}
}

func TestConfigureKeySource_EnvVar(t *testing.T) {
	calls := stubReadSecret(t, "unused")
	provider := configuration.CustomProviderConfig{Name: "gw"}

	key, err := configureKeySource(bufio.NewReader(strings.NewReader("2\n\nGW_API_KEY\n")), &provider)
	if err != nil {
		t.Fatal(err)
	}
	if key != "" || *calls != 0 {
		t.Fatalf("env var mode must not read a secret (key=%q, reads=%d)", key, *calls)
	}
	if provider.EnvVar != "GW_API_KEY" || !provider.RequiresAPIKey {
		t.Fatalf("provider = %+v, want env var GW_API_KEY (blank name re-prompted)", provider)
	}
}

func TestConfigureKeySource_NoKey(t *testing.T) {
	stubReadSecret(t, "unused")
	provider := configuration.CustomProviderConfig{Name: "local", EnvVar: "STALE", RequiresAPIKey: true}

	key, err := configureKeySource(bufio.NewReader(strings.NewReader("3\n")), &provider)
	if err != nil {
		t.Fatal(err)
	}
	if key != "" || provider.EnvVar != "" || provider.RequiresAPIKey {
		t.Fatalf("key=%q provider=%+v, want a keyless provider", key, provider)
	}
}

func TestPromptKeySource_RepromptsOnInvalidChoice(t *testing.T) {
	source, err := promptKeySource(bufio.NewReader(strings.NewReader("9\nabc\n2\n")))
	if err != nil {
		t.Fatal(err)
	}
	if source != keySourceEnvVar {
		t.Fatalf("source = %v, want env var after two invalid answers", source)
	}
}

func TestPromptKeySource_EOFIsAnError(t *testing.T) {
	if _, err := promptKeySource(bufio.NewReader(strings.NewReader("9\n"))); err == nil {
		t.Fatal("running out of input must end the prompt with an error, not loop")
	}
}

func TestStorePastedKey_StoresAndDescribes(t *testing.T) {
	isolateCredentials(t)
	provider := configuration.CustomProviderConfig{Name: "gw", RequiresAPIKey: true}

	if got := describeKeySource("gw", provider); !strings.HasPrefix(got, "not set") {
		t.Fatalf("before storing: %q, want not set", got)
	}
	storePastedKey("gw", "sk-stored")
	stored, _, err := credentials.GetFromActiveBackend("gw")
	if err != nil || stored != "sk-stored" {
		t.Fatalf("stored = %q, err = %v", stored, err)
	}
	got := describeKeySource("gw", provider)
	if !strings.HasPrefix(got, "stored") || strings.Contains(got, "sk-stored") {
		t.Fatalf("describeKeySource = %q, want a stored source that never shows the key", got)
	}
}

func TestDescribeKeySource_EnvVarAndNone(t *testing.T) {
	if got := describeKeySource("a", configuration.CustomProviderConfig{EnvVar: "A_KEY", RequiresAPIKey: true}); got != "env var A_KEY" {
		t.Fatalf("env var: %q", got)
	}
	if got := describeKeySource("b", configuration.CustomProviderConfig{}); got != "none" {
		t.Fatalf("keyless: %q", got)
	}
}
