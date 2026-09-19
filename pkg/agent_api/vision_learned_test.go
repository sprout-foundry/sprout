package api

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

func visionLearnedTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())
	globalVisionCapCache.mu.Lock()
	globalVisionCapCache.loaded = false
	globalVisionCapCache.data = nil
	globalVisionCapCache.mu.Unlock()
	t.Cleanup(func() {
		globalVisionCapCache.mu.Lock()
		globalVisionCapCache.loaded = false
		globalVisionCapCache.data = nil
		globalVisionCapCache.mu.Unlock()
	})
}

func TestLearnedVisionAcceptance_RoundTrip(t *testing.T) {
	visionLearnedTestEnv(t)

	if got := LearnedVisionAcceptance("zai-coding", "glm-5v-turbo"); got != nil {
		t.Fatalf("expected no learning initially, got %v", *got)
	}

	RecordVisionAcceptance("zai-coding", "glm-5v-turbo", true)
	got := LearnedVisionAcceptance("zai-coding", "GLM-5V-TURBO") // key is case-insensitive
	if got == nil || !*got {
		t.Fatalf("expected learned true, got %v", got)
	}

	RecordVisionAcceptance("zai-coding", "glm-5v-turbo", false)
	got = LearnedVisionAcceptance("zai-coding", "glm-5v-turbo")
	if got == nil || *got {
		t.Fatalf("expected learned false after negative record, got %v", got)
	}
}

func TestLearnedVisionAcceptance_TTLExpiry(t *testing.T) {
	visionLearnedTestEnv(t)

	RecordVisionAcceptance("prov", "stale-true", true)
	RecordVisionAcceptance("prov", "stale-false", false)

	// Age both entries past their TTLs by rewriting the file directly.
	path, err := visionCapPath()
	if err != nil {
		t.Fatalf("visionCapPath: %v", err)
	}
	file := visionCapFile{
		"prov/stale-true":  {AcceptsImages: true, VerifiedAt: time.Now().Add(-31 * 24 * time.Hour)},
		"prov/stale-false": {AcceptsImages: false, VerifiedAt: time.Now().Add(-8 * 24 * time.Hour)},
	}
	raw, err := marshalVisionCap(file)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Filesystems with coarse mtime granularity (some Android/FUSE layers)
	// can give this write the same timestamp as the preceding
	// RecordVisionAcceptance write, and the cache's mtime check would then
	// serve the fresh data instead of the stale fixture. Force a distinct
	// mtime so the expiry assertions hold everywhere.
	stale := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if got := LearnedVisionAcceptance("prov", "stale-true"); got != nil {
		t.Errorf("positive entry past 30d TTL should expire, got %v", *got)
	}
	if got := LearnedVisionAcceptance("prov", "stale-false"); got != nil {
		t.Errorf("negative entry past 7d TTL should expire, got %v", *got)
	}
}

func marshalVisionCap(f visionCapFile) ([]byte, error) {
	return json.MarshalIndent(f, "", "  ")
}

func TestIsVisionCapabilityRejection(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"capability 400", errors.New("HTTP 400: image input is not supported by this model"), true},
		{"modality 422", errors.New("HTTP 422: invalid modality: image_url parts not accepted"), true},
		{"vision keyword", errors.New("HTTP 400: model does not support vision"), true},
		{"transient 429", errors.New("HTTP 429: rate limited"), false},
		{"server 503", errors.New("HTTP 503: service unavailable"), false},
		{"network", errors.New("connection reset by peer"), false},
		{"auth 401", errors.New("HTTP 401: unauthorized"), false},
		{"400 without keywords", errors.New("HTTP 400: bad request"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsVisionCapabilityRejection(tc.err); got != tc.want {
				t.Errorf("IsVisionCapabilityRejection(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestStripImagesWithNote(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "look", Images: []ImageData{{Base64: "x"}}},
	}
	out := StripImagesWithNote(msgs)

	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	if len(out[1].Images) != 0 {
		t.Error("images should be stripped")
	}
	if !contains(out[1].Content, "image content withheld") {
		t.Errorf("note should be appended, got %q", out[1].Content)
	}
	// Input untouched.
	if len(msgs[1].Images) != 1 || msgs[1].Content != "look" {
		t.Error("input messages must not be mutated")
	}
	// Idempotent-ish: stripping a stripped message doesn't double the note.
	out2 := StripImagesWithNote(out)
	if countOccurrences(out2[1].Content, "image content withheld") != 1 {
		t.Errorf("note should not duplicate, got %q", out2[1].Content)
	}
}

func TestMessagesHaveImages(t *testing.T) {
	if MessagesHaveImages([]Message{{Role: "user", Content: "hi"}}) {
		t.Error("no images expected")
	}
	if !MessagesHaveImages([]Message{{Role: "user", Content: "hi", Images: []ImageData{{Base64: "x"}}}}) {
		t.Error("images expected")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func countOccurrences(s, sub string) int {
	count := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			count++
		}
	}
	return count
}
