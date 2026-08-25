package computer_use

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ============================================================================
// allow=true schema — JSON unmarshaling
// ============================================================================

func TestJsonEntry_AllowUnmarshal(t *testing.T) {
	raw := `{"bundle_id":"com.example.app","allow":true,"category":"test","reason":"test allow"}`
	var entry jsonEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !entry.Allow {
		t.Error("expected Allow=true after unmarshal")
	}
	if entry.BundleID != "com.example.app" {
		t.Errorf("bundle_id: got %q, want %q", entry.BundleID, "com.example.app")
	}
}

func TestJsonEntry_AllowDefaultFalse(t *testing.T) {
	raw := `{"bundle_id":"com.example.app","category":"test","reason":"test no allow"}`
	var entry jsonEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if entry.Allow {
		t.Error("expected Allow=false when omitted")
	}
}

// ============================================================================
// mergeLists — allow=true removes default entries
// ============================================================================

func TestMergeLists_AllowRemovesDefault_BundleID(t *testing.T) {
	defaults := []DenylistEntry{
		{BundleID: "com.apple.mail", Category: CategoryDestructive, Reason: "default destructive"},
		{BundleID: "com.apple.Safari", Category: CategoryFinancial, Reason: "safari"},
	}
	overrides := []DenylistEntry{
		{BundleID: "com.apple.mail", Allow: true, FromOverride: true},
	}

	merged := mergeLists(defaults, overrides)

	// The mail entry should have been removed; only Safari remains.
	if len(merged) != 1 {
		t.Fatalf("expected 1 entry after allow-override, got %d", len(merged))
	}
	if merged[0].BundleID != "com.apple.Safari" {
		t.Errorf("remaining entry: got %q, want %q", merged[0].BundleID, "com.apple.Safari")
	}
}

func TestMergeLists_AllowRemovesDefault_WindowClassRegex(t *testing.T) {
	defaults := []DenylistEntry{
		{WindowClassRegex: "Thunderbird", Category: CategoryDestructive, Reason: "default"},
		{WindowClassRegex: "Firefox", Category: CategoryFinancial, Reason: "browser"},
	}
	overrides := []DenylistEntry{
		{WindowClassRegex: "Thunderbird", Allow: true, FromOverride: true},
	}

	merged := mergeLists(defaults, overrides)

	if len(merged) != 1 {
		t.Fatalf("expected 1 entry after allow-override, got %d", len(merged))
	}
	if merged[0].WindowClassRegex != "Firefox" {
		t.Errorf("remaining entry: got %q, want %q", merged[0].WindowClassRegex, "Firefox")
	}
}

func TestMergeLists_AllowOnNonDefault_AddsEntry(t *testing.T) {
	defaults := []DenylistEntry{
		{BundleID: "com.apple.mail", Category: CategoryDestructive, Reason: "default"},
	}
	// Allow entry for an app NOT in defaults — should just be added.
	overrides := []DenylistEntry{
		{BundleID: "com.example.myapp", Allow: true, FromOverride: true},
	}

	merged := mergeLists(defaults, overrides)
	if len(merged) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(merged))
	}
}

// ============================================================================
// IsDestructiveApp with allow=true override
// ============================================================================

func TestIsDestructiveApp_AllowOverride_RemovesDefault(t *testing.T) {
	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "overrides.json")
	// Override with allow:true for com.apple.mail (which is in defaults as destructive).
	data := []byte(`{
		"version": 1,
		"macos": [
			{"bundle_id": "com.apple.mail", "allow": true}
		]
	}`)
	if err := os.WriteFile(overridePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Loader{overridePath: overridePath, custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// com.apple.mail should no longer be classified as destructive.
	c := l.IsDestructiveApp(ForegroundInfo{BundleID: "com.apple.mail"})
	if c.IsBlocked() {
		t.Errorf("expected com.apple.mail to NOT be blocked after allow:true override, got category=%q", c.Category)
	}

	// Another default entry should still be blocked.
	c2 := l.IsDestructiveApp(ForegroundInfo{BundleID: "com.apple.diskutility"})
	if !c2.IsBlocked() {
		t.Error("expected com.apple.diskutility to still be blocked")
	}
}

func TestIsDestructiveApp_AllowOverride_WindowClass(t *testing.T) {
	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "overrides.json")
	// Override with allow:true for Thunderbird (which is in defaults as destructive).
	data := []byte(`{
		"version": 1,
		"linux": [
			{"window_class_regex": "Thunderbird", "allow": true}
		]
	}`)
	if err := os.WriteFile(overridePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Loader{overridePath: overridePath, custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// Thunderbird with Compose title should no longer be blocked.
	c := l.IsDestructiveApp(ForegroundInfo{
		WindowClass: "Thunderbird",
		WindowTitle: "Compose Message",
	})
	if c.IsBlocked() {
		t.Errorf("expected Thunderbird to NOT be blocked after allow:true override, got category=%q", c.Category)
	}
}

// ============================================================================
// AddAllowEntry helper
// ============================================================================

func TestAddAllowEntry_BundleID(t *testing.T) {
	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "overrides.json")

	l := &Loader{overridePath: overridePath, custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatalf("initial Reload: %v", err)
	}

	// com.apple.mail is destructive by default.
	c1 := l.IsDestructiveApp(ForegroundInfo{BundleID: "com.apple.mail"})
	if !c1.IsBlocked() {
		t.Fatal("com.apple.mail should be blocked before allow")
	}

	// Add allow entry.
	err := AddAllowEntry(l, "com.apple.mail", "")
	if err != nil {
		t.Fatalf("AddAllowEntry: %v", err)
	}

	// After allow, it should no longer be blocked.
	c2 := l.IsDestructiveApp(ForegroundInfo{BundleID: "com.apple.mail"})
	if c2.IsBlocked() {
		t.Errorf("expected com.apple.mail to NOT be blocked after AddAllowEntry, got category=%q", c2.Category)
	}

	// Verify the override file was written with valid JSON.
	data, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatalf("read override file: %v", err)
	}
	var raw denylistJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("override file is not valid JSON: %v", err)
	}
	// Check the entry exists.
	found := false
	for _, e := range raw.Macos {
		if e.BundleID == "com.apple.mail" && e.Allow {
			found = true
			break
		}
	}
	if !found {
		t.Error("override file missing allow entry for com.apple.mail")
	}
}

func TestAddAllowEntry_WindowClassRegex(t *testing.T) {
	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "overrides.json")

	l := &Loader{overridePath: overridePath, custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatalf("initial Reload: %v", err)
	}

	// Thunderbird is destructive by default.
	c1 := l.IsDestructiveApp(ForegroundInfo{
		WindowClass: "Thunderbird",
		WindowTitle: "Compose Message",
	})
	if !c1.IsBlocked() {
		t.Fatal("Thunderbird should be blocked before allow")
	}

	// Add allow entry by window class regex.
	err := AddAllowEntry(l, "", "Thunderbird")
	if err != nil {
		t.Fatalf("AddAllowEntry: %v", err)
	}

	// After allow, it should no longer be blocked.
	c2 := l.IsDestructiveApp(ForegroundInfo{
		WindowClass: "Thunderbird",
		WindowTitle: "Compose Message",
	})
	if c2.IsBlocked() {
		t.Errorf("expected Thunderbird to NOT be blocked after AddAllowEntry, got category=%q", c2.Category)
	}
}

func TestAddAllowEntry_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "overrides.json")

	l := &Loader{overridePath: overridePath, custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatalf("initial Reload: %v", err)
	}

	// Call AddAllowEntry twice for the same bundle ID.
	err1 := AddAllowEntry(l, "com.apple.mail", "")
	if err1 != nil {
		t.Fatalf("first AddAllowEntry: %v", err1)
	}
	err2 := AddAllowEntry(l, "com.apple.mail", "")
	if err2 != nil {
		t.Fatalf("second AddAllowEntry: %v", err2)
	}

	// Verify only one entry exists in the override file.
	data, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatalf("read override file: %v", err)
	}
	var raw denylistJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("override file is not valid JSON: %v", err)
	}
	count := 0
	for _, e := range raw.Macos {
		if e.BundleID == "com.apple.mail" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 entry for com.apple.mail after idempotent AddAllowEntry, got %d", count)
	}
}

func TestAddAllowEntry_NilLoader(t *testing.T) {
	err := AddAllowEntry(nil, "com.example.app", "")
	if err == nil {
		t.Error("expected error for nil loader")
	}
}

func TestAddAllowEntry_NoKey(t *testing.T) {
	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "overrides.json")
	l := &Loader{overridePath: overridePath, custom: map[string]bool{}}

	err := AddAllowEntry(l, "", "")
	if err == nil {
		t.Error("expected error when both bundleID and windowClassRegex are empty")
	}
}

func TestAddAllowEntry_NoOverridePath(t *testing.T) {
	l := &Loader{custom: map[string]bool{}} // no overridePath set
	err := AddAllowEntry(l, "com.example.app", "")
	if err == nil {
		t.Error("expected error when no override file path is configured")
	}
}

func TestAddAllowEntry_InvalidRegexInOverride(t *testing.T) {
	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "overrides.json")

	// Pre-write an override file with an invalid regex in another entry.
	data := []byte(`{
		"version": 1,
		"linux": [
			{"window_class_regex": "[invalid", "category": "destructive", "reason": "bad regex"}
		]
	}`)
	if err := os.WriteFile(overridePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Loader{overridePath: overridePath, custom: map[string]bool{}}
	// The initial reload should fail because of the bad regex.
	err := l.Reload()
	if err == nil {
		t.Fatal("expected error for invalid regex in override, got nil")
	}
}

func TestAddAllowEntry_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "nested", "deep", "overrides.json")

	l := &Loader{overridePath: overridePath, custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatalf("initial Reload: %v", err)
	}

	err := AddAllowEntry(l, "com.example.app", "")
	if err != nil {
		t.Fatalf("AddAllowEntry: %v", err)
	}

	// Verify the file was created with nested directories.
	data, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatalf("read override file: %v", err)
	}
	var raw denylistJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("override file is not valid JSON: %v", err)
	}
	found := false
	for _, e := range raw.Macos {
		if e.BundleID == "com.example.app" && e.Allow {
			found = true
			break
		}
	}
	if !found {
		t.Error("override file missing allow entry for com.example.app")
	}
}

// ============================================================================
// AddAllowEntry with existing override file — valid JSON after write
// ============================================================================

func TestAddAllowEntry_ExistingOverrideFile(t *testing.T) {
	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "overrides.json")

	// Start with an existing override file that has a different entry.
	data := []byte(`{
		"version": 1,
		"macos": [
			{"bundle_id": "com.existing.app", "allow": true}
		],
		"linux": []
	}`)
	if err := os.WriteFile(overridePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Loader{overridePath: overridePath, custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatalf("initial Reload: %v", err)
	}

	// Add another allow entry.
	err := AddAllowEntry(l, "com.apple.mail", "")
	if err != nil {
		t.Fatalf("AddAllowEntry: %v", err)
	}

	// Verify both entries exist and the file is valid JSON.
	fileData, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatalf("read override file: %v", err)
	}
	var raw denylistJSON
	if err := json.Unmarshal(fileData, &raw); err != nil {
		t.Fatalf("override file is not valid JSON after AddAllowEntry: %v", err)
	}

	// Count entries.
	mailFound := false
	existingFound := false
	for _, e := range raw.Macos {
		if e.BundleID == "com.apple.mail" && e.Allow {
			mailFound = true
		}
		if e.BundleID == "com.existing.app" && e.Allow {
			existingFound = true
		}
	}
	if !mailFound {
		t.Error("missing com.apple.mail allow entry")
	}
	if !existingFound {
		t.Error("missing com.existing.app allow entry (should be preserved)")
	}
}
