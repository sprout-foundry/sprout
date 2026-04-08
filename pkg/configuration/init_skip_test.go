package configuration

import (
	"testing"
)

func TestValidateProviderSetup_EditorSentinel(t *testing.T) {
	err := validateProviderSetup("editor")
	if err != nil {
		t.Errorf("validateProviderSetup('editor') should return nil, got: %v", err)
	}
}
