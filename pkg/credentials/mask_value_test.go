package credentials_test

import (
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/credentials"
)

// ---------------------------------------------------------------------------
// MaskValue tests
// ---------------------------------------------------------------------------

func TestMaskValue_EmptyString_ReturnsEmpty(t *testing.T) {
	got := credentials.MaskValue("")
	if got != "" {
		t.Errorf("MaskValue(%q) = %q, want %q", "", got, "")
	}
}

func TestMaskValue_SingleChar_ReturnsMask(t *testing.T) {
	got := credentials.MaskValue("a")
	if got != "****" {
		t.Errorf("MaskValue(%q) = %q, want %q", "a", got, "****")
	}
}

func TestMaskValue_ThreeChars_ReturnsMask(t *testing.T) {
	got := credentials.MaskValue("abc")
	if got != "****" {
		t.Errorf("MaskValue(%q) = %q, want %q", "abc", got, "****")
	}
}

func TestMaskValue_FourChars_ReturnsFirstTwoPlusMask(t *testing.T) {
	got := credentials.MaskValue("abcd")
	want := "ab****"
	if got != want {
		t.Errorf("MaskValue(%q) = %q, want %q", "abcd", got, want)
	}
}

func TestMaskValue_SevenChars_ReturnsFirstTwoPlusMask(t *testing.T) {
	got := credentials.MaskValue("abcdefg")
	want := "ab****"
	if got != want {
		t.Errorf("MaskValue(%q) = %q, want %q", "abcdefg", got, want)
	}
}

func TestMaskValue_EightChars_ReturnsFirstFourPlusMask(t *testing.T) {
	got := credentials.MaskValue("abcdefgh")
	want := "abcd****"
	if got != want {
		t.Errorf("MaskValue(%q) = %q, want %q", "abcdefgh", got, want)
	}
}

func TestMaskValue_TwentyChars_ReturnsFirstFourPlusMask(t *testing.T) {
	value := "abcdefghijklmnopqrst"
	got := credentials.MaskValue(value)
	want := "abcd****"
	if got != want {
		t.Errorf("MaskValue(%q) = %q, want %q", value, got, want)
	}
}

func TestMaskValue_VeryLongValue_ReturnsFirstFourPlusMask(t *testing.T) {
	// 100+ character value
	value := strings.Repeat("x", 150)
	got := credentials.MaskValue(value)
	want := "xxxx****"
	if got != want {
		t.Errorf("MaskValue(<150 chars>) = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Resolved.String() tests
// ---------------------------------------------------------------------------

func TestResolvedString_FullCredential_MasksValue(t *testing.T) {
	r := credentials.Resolved{
		Provider: "openai",
		EnvVar:   "OPENAI_API_KEY",
		Value:    "sk-abcdefghijklmnop",
		Source:   "environment",
	}

	s := r.String()

	// The full value must NOT appear in the string output
	if strings.Contains(s, "sk-abcdefghijklmnop") {
		t.Errorf("Resolved.String() leaked the real credential value: %s", s)
	}

	// Provider should be present unmasked
	if !strings.Contains(s, `"openai"`) {
		t.Errorf("Resolved.String() missing provider %q: %s", "openai", s)
	}

	// Source should be present
	if !strings.Contains(s, `"environment"`) {
		t.Errorf("Resolved.String() missing source %q: %s", "environment", s)
	}

	// Value should be masked with the first-4-chars prefix
	if !strings.Contains(s, `"sk-a****"`) {
		t.Errorf("Resolved.String() expected masked value %q in output: %s", "sk-a****", s)
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

	// Empty value should show empty string (no "****" for empty)
	if strings.Contains(s, "****") {
		t.Errorf("Resolved.String() should not show mask for empty value: %s", s)
	}

	// Verify the empty value is represented as ""
	if !strings.Contains(s, `Value: ""`) {
		t.Errorf("Resolved.String() expected empty Value field: %s", s)
	}
}

func TestResolvedString_KeyringSource_ShowsCorrectly(t *testing.T) {
	r := credentials.Resolved{
		Provider: "anthropic",
		EnvVar:   "ANTHROPIC_API_KEY",
		Value:    "sk-ant-key",
		Source:   "keyring",
	}

	s := r.String()

	// Source should reflect "keyring"
	if !strings.Contains(s, `"keyring"`) {
		t.Errorf("Resolved.String() missing source %q: %s", "keyring", s)
	}

	// Value must be masked (8+ chars → first 4 + ****)
	if !strings.Contains(s, `"sk-a****"`) {
		t.Errorf("Resolved.String() expected masked value %q in output: %s", "sk-a****", s)
	}

	// Real value should never appear
	if strings.Contains(s, "sk-ant-key") {
		t.Errorf("Resolved.String() leaked the real credential value: %s", s)
	}
}
