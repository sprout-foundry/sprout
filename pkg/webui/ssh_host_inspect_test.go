//go:build !js

package webui

import (
	"testing"
)

func TestNormalizeRemotePlatform(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Linux", "linux"},
		{"linux", "linux"},
		{"Darwin", "darwin"},
		{"darwin", "darwin"},
		{"Windows", ""},
		{"FreeBSD", ""},
		{"", ""},
		{"  linux  ", "linux"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeRemotePlatform(tt.input)
			if got != tt.want {
				t.Errorf("normalizeRemotePlatform(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeRemoteArch(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"x86_64", "amd64"},
		{"amd64", "amd64"},
		{"arm64", "arm64"},
		{"aarch64", "arm64"},
		{"i386", ""},
		{"", ""},
		{"  ARM64  ", "arm64"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeRemoteArch(tt.input)
			if got != tt.want {
				t.Errorf("normalizeRemoteArch(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
