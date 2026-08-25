package computer_use

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRateLimiter_CapsActions(t *testing.T) {
	m := &MockBackend{}
	rl := NewRateLimitedBackend(m, 3)
	base := time.Unix(1000, 0)
	rl.now = func() time.Time { return base }

	for i := 0; i < 3; i++ {
		if err := rl.MouseClick(0, 0, MouseLeft, false); err != nil {
			t.Fatalf("click %d should pass: %v", i, err)
		}
	}
	if err := rl.MouseClick(0, 0, MouseLeft, false); err != ErrRateLimited {
		t.Fatalf("4th action should be rate-limited, got %v", err)
	}
}

func TestRateLimiter_WindowExpiry(t *testing.T) {
	m := &MockBackend{}
	rl := NewRateLimitedBackend(m, 1)
	now := time.Unix(2000, 0)
	rl.now = func() time.Time { return now }

	if err := rl.KeyboardPress("a"); err != nil {
		t.Fatalf("first should pass: %v", err)
	}
	if err := rl.KeyboardPress("b"); err != ErrRateLimited {
		t.Fatalf("second within window should fail, got %v", err)
	}
	now = now.Add(61 * time.Second) // slide past the window
	if err := rl.KeyboardPress("c"); err != nil {
		t.Fatalf("after window should pass: %v", err)
	}
}

func TestRateLimiter_DisabledWhenZero(t *testing.T) {
	rl := NewRateLimitedBackend(&MockBackend{}, 0)
	for i := 0; i < 100; i++ {
		if err := rl.Scroll(ScrollDown, 1, nil); err != nil {
			t.Fatalf("uncapped scroll %d failed: %v", i, err)
		}
	}
}

func TestAuditLog_WritesRecords(t *testing.T) {
	dir := t.TempDir()
	ab, err := NewAuditingBackend(&MockBackend{}, dir, "sess-1")
	if err != nil {
		t.Fatalf("NewAuditingBackend: %v", err)
	}
	_ = ab.MouseClick(5, 6, MouseLeft, false)
	_ = ab.KeyboardType("secret-password")
	if err := ab.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	path := filepath.Join(dir, "sess-1.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	var recs []AuditRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r AuditRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("bad jsonl line: %v", err)
		}
		recs = append(recs, r)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
	if recs[0].Action != "mouse_click" {
		t.Errorf("rec0 action = %q", recs[0].Action)
	}
	// keyboard_type must record length, never the secret text.
	if recs[1].Action != "keyboard_type" {
		t.Errorf("rec1 action = %q", recs[1].Action)
	}
	line := mustJSON(t, recs[1])
	if strings.Contains(line, "secret-password") {
		t.Error("audit log leaked typed text — must record length only")
	}
}

func TestAuditLog_SanitizesSessionID(t *testing.T) {
	dir := t.TempDir()
	ab, err := NewAuditingBackend(&MockBackend{}, dir, "../evil/../id")
	if err != nil {
		t.Fatalf("NewAuditingBackend: %v", err)
	}
	_ = ab.Close()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	if strings.ContainsAny(entries[0].Name(), "/.") && !strings.HasSuffix(entries[0].Name(), ".jsonl") {
		t.Errorf("unsanitized session filename: %q", entries[0].Name())
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
