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

// imageMsg returns a user message carrying n base64 image parts of pLen bytes
// each (pLen == 0 models URL parts, which cost nothing on the body budget).
func imageMsg(n, pLen int) Message {
	imgs := make([]ImageData, n)
	for i := range imgs {
		imgs[i] = ImageData{Base64: strings.Repeat("x", pLen), Type: "image/png"}
	}
	return Message{Role: "user", Content: "img", Images: imgs}
}

func TestMessageBodyImageBytes(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hi", Images: []ImageData{{URL: "https://x/1.png"}}},                                               // URL: 0
		{Role: "user", Content: "yo", Images: []ImageData{{Base64: "AAAAA", Type: "image/png"}}},                                   // 5
		{Role: "tool", Content: "t", Images: []ImageData{{Base64: "BBBB", Type: "image/png"}, {Base64: "CCC", Type: "image/png"}}}, // 4+3
	}
	if got := MessageBodyImageBytes(msgs[0]); got != 0 {
		t.Errorf("URL part cost = %d, want 0", got)
	}
	if got := MessageBodyImageBytes(msgs[1]); got != 5 {
		t.Errorf("5-byte base64 cost = %d, want 5", got)
	}
	if got := MessageBodyImageBytes(msgs[2]); got != 7 {
		t.Errorf("4+3 base64 cost = %d, want 7", got)
	}
	if got := MessagesBodyImageBytes(msgs); got != 12 {
		t.Errorf("total = %d, want 12", got)
	}
}

func TestTrimImageBudgetWithinBudget(t *testing.T) {
	// 3 x 100 bytes = 300, well under a 1000-byte budget: untouched, and the
	// input slice is returned as-is (stable wire prefix).
	msgs := []Message{imageMsg(1, 100), imageMsg(1, 100), imageMsg(1, 100)}
	got := TrimImageBudget(msgs, 1000)
	// Within budget: the input slice is returned as-is (stable wire prefix),
	// so it aliases the same backing array.
	if len(got) != len(msgs) || &got[0] != &msgs[0] {
		t.Fatalf("within budget: expected the input slice back (same backing array), got len=%d", len(got))
	}
	if got := MessagesBodyImageBytes(got); got != 300 {
		t.Errorf("image mass = %d, want 300", got)
	}
}

func TestTrimImageBudgetShedsOldestKeepsMostRecent(t *testing.T) {
	// 5 messages of 1000 bytes each (5000 total); a 1500-byte budget keeps
	// the 3 newest (3000 -> wait, 1500 keeps only 1... use 3000 to keep 3).
	// Budget of 3000 over 5000 keeps the 3 newest, sheds the 2 oldest.
	msgs := []Message{
		imageMsg(1, 1000), imageMsg(1, 1000), imageMsg(1, 1000),
		imageMsg(1, 1000), imageMsg(1, 1000),
	}
	got := TrimImageBudget(msgs, 3000)
	// Newest kept, oldest two shed with a note.
	if len(got[0].Images) != 0 || !strings.Contains(got[0].Content, "withheld") {
		t.Errorf("oldest not withheld: imgs=%v content=%q", got[0].Images, got[0].Content)
	}
	if len(got[1].Images) != 0 || !strings.Contains(got[1].Content, "withheld") {
		t.Errorf("2nd-oldest not withheld: imgs=%v content=%q", got[1].Images, got[1].Content)
	}
	for i := 2; i < 5; i++ {
		if len(got[i].Images) != 1 {
			t.Errorf("message %d lost its image (kept=%d)", i, len(got[i].Images))
		}
	}
	if got := MessagesBodyImageBytes(got); got != 3000 {
		t.Errorf("kept image mass = %d, want 3000", got)
	}
}

func TestTrimImageBudgetKeepsCurrentTurnImageOnPartialFit(t *testing.T) {
	// The fix's core case: a message whose image overflows the remaining
	// budget keeps its NEWEST part (the current turn's attached image) and
	// sheds the oldest part within that message. One message, 2 parts: the
	// older is 200 bytes, the newer is 500 bytes. Budget 500 keeps only the
	// newer (500), sheds the older (200) -> total 500.
	msg := Message{Role: "user", Content: "attach", Images: []ImageData{
		{Base64: strings.Repeat("a", 200), Type: "image/png"}, // older
		{Base64: strings.Repeat("b", 500), Type: "image/png"}, // newer (current turn)
	}}
	got := TrimImageBudget([]Message{msg}, 500)
	if len(got[0].Images) != 1 {
		t.Fatalf("kept %d parts, want 1 (the newest)", len(got[0].Images))
	}
	// The kept part is the 500-byte newer one, not the 200-byte older one.
	if len(got[0].Images[0].Base64) != 500 {
		t.Errorf("kept %d-byte part, want the 500-byte current-turn image", len(got[0].Images[0].Base64))
	}
	if got := MessagesBodyImageBytes(got); got != 500 {
		t.Errorf("kept image mass = %d, want 500", got)
	}
	if !strings.Contains(got[0].Content, "withheld") {
		t.Errorf("sholder part should carry a withhold note: %q", got[0].Content)
	}
}

func TestTrimImageBudgetZeroStripsAll(t *testing.T) {
	msgs := []Message{imageMsg(2, 100), imageMsg(1, 100)}
	got := TrimImageBudget(msgs, 0)
	if got := MessagesBodyImageBytes(got); got != 0 {
		t.Errorf("budget 0 still carries %d bytes of images", got)
	}
	if !strings.Contains(got[0].Content, "withheld") || !strings.Contains(got[1].Content, "withheld") {
		t.Errorf("both messages should carry a withhold note")
	}
}
