//go:build !js

package webui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExpandHomeVar(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no home var", "/usr/local/bin", "/usr/local/bin"},
		{"$HOME prefix", "$HOME/projects", "/home/testuser/projects"},
		{"${HOME} prefix", "${HOME}/projects", "/home/testuser/projects"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandHomeVar(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExpandHomeVar_NoHome(t *testing.T) {
	t.Setenv("HOME", "")
	result := expandHomeVar("$HOME/test")
	assert.Equal(t, "$HOME/test", result, "should return unchanged if HOME is empty")
}

func TestIsExpectedServerCloseError(t *testing.T) {
	assert.False(t, isExpectedServerCloseError(nil))
}
