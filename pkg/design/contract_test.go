package design

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubdirsContract(t *testing.T) {
	want := []string{"tokens", "brand", "icons", "wireframes", "components", "screens", "flows", "feedback"}

	assert.Len(t, Subdirs, 8, "Subdirs must declare exactly eight directories")
	assert.Equal(t, want, Subdirs, "Subdirs contents and contract order mismatch")
}

func TestSubdirsJoined(t *testing.T) {
	joined := SubdirsJoined()

	assert.Len(t, joined, len(Subdirs), "SubdirsJoined length mismatch")
	assert.NotNil(t, joined, "SubdirsJoined must never return nil")
	for i, sub := range Subdirs {
		assert.Equal(t, filepath.Join(DirName, sub), joined[i], "SubdirsJoined[%d] mismatch", i)
	}
}

func TestSubdirByName(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		{name: "first contract entry", in: "tokens", want: filepath.Join(DirName, "tokens"), wantOK: true},
		{name: "later contract entry", in: "flows", want: filepath.Join(DirName, "flows"), wantOK: true},
		{name: "unknown name", in: "assets", want: "", wantOK: false},
		{name: "empty name", in: "", want: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := SubdirByName(tt.in)
			assert.Equal(t, tt.wantOK, ok, "SubdirByName(%q) ok mismatch", tt.in)
			assert.Equal(t, tt.want, got, "SubdirByName(%q) path mismatch", tt.in)
		})
	}

	for _, sub := range Subdirs {
		got, ok := SubdirByName(sub)
		assert.True(t, ok, "every canonical subdir %q must resolve", sub)
		assert.Equal(t, filepath.Join(DirName, sub), got,
			"SubdirByName must match SubdirsJoined semantics for %q", sub)
	}
}

func TestContractConstants(t *testing.T) {
	assert.Equal(t, "design", DirName)
	assert.Equal(t, "README.md", ManifestName)
}

func TestSlugPattern(t *testing.T) {
	re := regexp.MustCompile(SlugPattern)

	tests := []struct {
		name  string
		slug  string
		match bool
	}{
		{"single word", "login", true},
		{"multi word", "sign-up-flow", true},
		{"digits and hyphens", "a1-b2", true},
		{"single character", "a", true},
		{"uppercase rejected", "Login", false},
		{"underscore rejected", "sign_up", false},
		{"empty rejected", "", false},
		{"leading hyphen rejected", "-lead", false},
		{"trailing hyphen rejected", "trail-", false},
		{"double hyphen rejected", "bad--slug", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.match, re.MatchString(tt.slug), "slug %q match mismatch", tt.slug)
		})
	}
}
