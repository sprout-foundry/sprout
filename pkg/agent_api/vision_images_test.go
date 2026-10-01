package api

import (
	"strings"
	"testing"
)

func TestCountImages(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "tool", Content: "a", Images: []ImageData{{URL: "https://x/1.png"}, {URL: "https://x/2.png"}}},
		{Role: "user", Content: "yo", Images: []ImageData{{Base64: "AAA", Type: "image/png"}}},
	}
	if got := CountImages(msgs); got != 3 {
		t.Errorf("CountImages() = %d, want 3", got)
	}
	if got := CountImages(nil); got != 0 {
		t.Errorf("CountImages(nil) = %d, want 0", got)
	}
}

func TestTrimImagesBeyondLatestWithinBudget(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "q", Images: []ImageData{{URL: "https://x/1.png"}}},
		{Role: "tool", Content: "r", Images: []ImageData{{URL: "https://x/2.png"}}},
	}
	got := TrimImagesBeyondLatest(msgs, 3)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if CountImages(got) != 2 {
		t.Errorf("CountImages() = %d, want 2 (budget not binding)", CountImages(got))
	}
	// Not binding: the input slice must come back as-is (stable wire
	// prefix for the provider's prompt cache).
	if &got[0] != &msgs[0] {
		t.Error("non-binding budget must return the input slice unchanged")
	}
}

func TestTrimImagesBeyondLatestShedsOldest(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "q1", Images: []ImageData{{URL: "https://x/1.png"}}},
		{Role: "tool", Content: "r1", Images: []ImageData{{URL: "https://x/2.png"}, {URL: "https://x/3.png"}}},
		{Role: "user", Content: "q2", Images: []ImageData{{URL: "https://x/4.png"}}},
		{Role: "tool", Content: "r2", Images: []ImageData{{URL: "https://x/5.png"}}},
	}
	got := TrimImagesBeyondLatest(msgs, 2)
	if got := CountImages(got); got != 2 {
		t.Fatalf("CountImages() = %d, want 2", got)
	}
	// Most recent two kept (messages 3 and 4); the older two shed with a note.
	if len(got[0].Images) != 0 || len(got[1].Images) != 0 {
		t.Errorf("oldest images not shed: %d, %d", len(got[0].Images), len(got[1].Images))
	}
	if len(got[2].Images) != 1 || len(got[3].Images) != 1 {
		t.Errorf("recent images shed: %d, %d", len(got[2].Images), len(got[3].Images))
	}
	if !strings.Contains(got[0].Content, "withheld") || !strings.Contains(got[1].Content, "withheld") {
		t.Errorf("withhold note missing: %q / %q", got[0].Content, got[1].Content)
	}
	if strings.Contains(got[2].Content, "withheld") || strings.Contains(got[3].Content, "withheld") {
		t.Errorf("note on unshed message")
	}
	// Input unmodified.
	if len(msgs[0].Images) != 1 || len(msgs[1].Images) != 2 {
		t.Errorf("input slice mutated")
	}
}

func TestTrimImagesBeyondLatestPartialMessage(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "q", Images: []ImageData{{URL: "https://x/1.png"}, {URL: "https://x/2.png"}, {URL: "https://x/3.png"}}},
		{Role: "user", Content: "q2", Images: []ImageData{{URL: "https://x/4.png"}}},
	}
	got := TrimImagesBeyondLatest(msgs, 2)
	if CountImages(got) != 2 {
		t.Fatalf("CountImages() = %d, want 2", CountImages(got))
	}
	// Budget 2: keep the last image of message 0 plus message 1's image.
	if len(got[0].Images) != 1 || got[0].Images[0].URL != "https://x/3.png" {
		t.Errorf("message 0 kept wrong tail: %+v", got[0].Images)
	}
	if !strings.Contains(got[0].Content, "withheld") {
		t.Errorf("note missing on partially shed message")
	}
}

func TestTrimImagesBeyondLatestZeroBudgetStripsAll(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "q", Images: []ImageData{{URL: "https://x/1.png"}}},
	}
	got := TrimImagesBeyondLatest(msgs, 0)
	if CountImages(got) != 0 {
		t.Errorf("CountImages() = %d, want 0", CountImages(got))
	}
	// The 413 cascade's terminal strip is a server-side rejection — the
	// note must say the body limit withheld the pixels, not that the model
	// declined image input.
	if !strings.Contains(got[0].Content, imageWithholdNote) {
		t.Errorf("note should be the body-limit withhold note, got %q", got[0].Content)
	}
}

func TestTrimImagesBeyondLatestIdempotentNote(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "q", Images: []ImageData{{URL: "https://x/1.png"}}},
		{Role: "user", Content: "q2", Images: []ImageData{{URL: "https://x/2.png"}}},
	}
	once := TrimImagesBeyondLatest(msgs, 0)
	twice := TrimImagesBeyondLatest(once, 0)
	if n := strings.Count(twice[0].Content, "withheld"); n > 1 {
		t.Errorf("note duplicated across calls (%d): %q", n, twice[0].Content)
	}
}
