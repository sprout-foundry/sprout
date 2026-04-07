package configuration_test

import (
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/credentials"
)

func TestResolvedString_FullCredential_MasksValue(t *testing.T) {
	r := credentials.Resolved{
		Provider: "openai",
		EnvVar:   "OPENAI_API_KEY",
		Value:    "sk-openai-super-secret-key",
		Source:   "stored",
	}

	s := r.String()

	// The real secret must never appear in the string output
	if strings.Contains(s, "sk-openai-super-secret-key") {
		t.Errorf("Resolved.String() leaked the real credential value: %s", s)
	}

	// Provider and source should be present unmasked
	if !strings.Contains(s, `"openai"`) {
		t.Errorf("Resolved.String() missing provider: %s", s)
	}
	if !strings.Contains(s, `"stored"`) {
		t.Errorf("Resolved.String() missing source: %s", s)
	}

	// Value should be masked: first 4 chars + **** (>= 8 chars)
	if !strings.Contains(s, `"sk-o****"`) {
		t.Errorf("Resolved.String() expected masked value %q in output: %s", "sk-o****", s)
	}
}

func TestResolvedString_EmptyValue_ShowsEmptyNotMasked(t *testing.T) {
	r := credentials.Resolved{
		Provider: "ollama",
		EnvVar:   "",
		Value:    "",
		Source:   "none",
	}

	s := r.String()

	// Empty value should not produce a mask
	if strings.Contains(s, "****") {
		t.Errorf("Resolved.String() should not show mask for empty value: %s", s)
	}

	// The empty value should appear as ""
	if !strings.Contains(s, `Value: ""`) {
		t.Errorf("Resolved.String() expected empty Value field: %s", s)
	}
}
