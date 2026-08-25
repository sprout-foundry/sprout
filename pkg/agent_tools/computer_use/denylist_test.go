package computer_use

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// TestLoadDefaultList_Embedded
// ---------------------------------------------------------------------------

func TestLoadDefaultList_Embedded(t *testing.T) {
	entries, err := loadDefaultList()
	if err != nil {
		t.Fatalf("loadDefaultList() returned error: %v", err)
	}

	// Count macos vs linux by scanning the raw JSON structure.
	// macOS entries have BundleID set; Linux entries have WindowClassRegex set.
	macosCount := 0
	linuxCount := 0
	for _, e := range entries {
		if e.BundleID != "" {
			macosCount++
		} else if e.WindowClassRegex != "" {
			linuxCount++
		}
	}

	if macosCount < 10 {
		t.Errorf("expected >= 10 macOS entries, got %d", macosCount)
	}
	if linuxCount < 10 {
		t.Errorf("expected >= 10 Linux entries, got %d", linuxCount)
	}

	// Spot-check a well-known entry.
	found := false
	for _, e := range entries {
		if e.BundleID == "com.apple.mail" && e.Category == CategoryDestructive {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected com.apple.mail entry with destructive category in defaults")
	}
}

// ---------------------------------------------------------------------------
// TestLoadOverrideFile_Missing
// ---------------------------------------------------------------------------

func TestLoadOverrideFile_Missing(t *testing.T) {
	entries, err := loadOverrideFile("/nonexistent/path/overrides.json")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil slice for missing file, got %d entries", len(entries))
	}
}

// ---------------------------------------------------------------------------
// TestLoadOverrideFile_Malformed
// ---------------------------------------------------------------------------

func TestLoadOverrideFile_Malformed(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bad.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loadOverrideFile(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestLoadOverrideFile_Valid
// ---------------------------------------------------------------------------

func TestLoadOverrideFile_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "overrides.json")
	data := []byte(`{
		"version": 1,
		"macos": [
			{"bundle_id": "com.example.app", "category": "financial", "reason": "test macos"}
		],
		"linux": [
			{"window_class_regex": "TestApp", "category": "system", "reason": "test linux"}
		]
	}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := loadOverrideFile(path)
	if err != nil {
		t.Fatalf("loadOverrideFile() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Check macOS entry.
	macos := entries[0]
	if macos.BundleID != "com.example.app" {
		t.Errorf("macOS bundle_id: got %q, want %q", macos.BundleID, "com.example.app")
	}
	if macos.Category != CategoryFinancial {
		t.Errorf("macOS category: got %q, want %q", macos.Category, CategoryFinancial)
	}
	if !macos.FromOverride {
		t.Error("macOS entry should have FromOverride=true")
	}

	// Check Linux entry.
	linux := entries[1]
	if linux.WindowClassRegex != "TestApp" {
		t.Errorf("Linux window_class_regex: got %q, want %q", linux.WindowClassRegex, "TestApp")
	}
	if linux.Category != CategorySystem {
		t.Errorf("Linux category: got %q, want %q", linux.Category, CategorySystem)
	}
	if !linux.FromOverride {
		t.Error("Linux entry should have FromOverride=true")
	}
}

// ---------------------------------------------------------------------------
// TestMergeLists_Replace
// ---------------------------------------------------------------------------

func TestMergeLists_Replace(t *testing.T) {
	defaults := []DenylistEntry{
		{BundleID: "com.apple.mail", Category: CategoryDestructive, Reason: "default reason"},
		{BundleID: "com.example.other", Category: CategorySystem, Reason: "keep this"},
	}
	overrides := []DenylistEntry{
		{BundleID: "com.apple.mail", Category: CategorySystem, Reason: "override reason", FromOverride: true},
	}

	merged := mergeLists(defaults, overrides)
	if len(merged) != 2 {
		t.Fatalf("expected 2 entries after merge, got %d", len(merged))
	}

	// The mail entry should be replaced.
	mailEntry := merged[0]
	if mailEntry.Category != CategorySystem {
		t.Errorf("replaced category: got %q, want %q", mailEntry.Category, CategorySystem)
	}
	if mailEntry.Reason != "override reason" {
		t.Errorf("replaced reason: got %q, want %q", mailEntry.Reason, "override reason")
	}
	if !mailEntry.FromOverride {
		t.Error("replaced entry should have FromOverride=true")
	}

	// The other entry should be unchanged.
	otherEntry := merged[1]
	if otherEntry.BundleID != "com.example.other" {
		t.Errorf("unchanged entry bundle_id: got %q", otherEntry.BundleID)
	}
	if otherEntry.FromOverride {
		t.Error("default entry should NOT have FromOverride=true")
	}
}

// ---------------------------------------------------------------------------
// TestMergeLists_Allow (override replaces with different category)
// ---------------------------------------------------------------------------

func TestMergeLists_Allow(t *testing.T) {
	defaults := []DenylistEntry{
		{BundleID: "com.apple.mail", Category: CategoryDestructive, Reason: "default destructive"},
	}
	// Override changes the category — still replaces, doesn't "remove" the entry.
	overrides := []DenylistEntry{
		{BundleID: "com.apple.mail", Category: CategoryFinancial, Reason: "now financial", FromOverride: true},
	}

	merged := mergeLists(defaults, overrides)
	if len(merged) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(merged))
	}

	entry := merged[0]
	if entry.Category != CategoryFinancial {
		t.Errorf("category: got %q, want %q", entry.Category, CategoryFinancial)
	}
	if !entry.FromOverride {
		t.Error("entry should have FromOverride=true after replacement")
	}
}

// ---------------------------------------------------------------------------
// TestMergeLists_Add
// ---------------------------------------------------------------------------

func TestMergeLists_Add(t *testing.T) {
	defaults := []DenylistEntry{
		{BundleID: "com.apple.mail", Category: CategoryDestructive, Reason: "default"},
	}
	overrides := []DenylistEntry{
		{BundleID: "com.new.app", Category: CategoryFinancial, Reason: "new entry", FromOverride: true},
	}

	merged := mergeLists(defaults, overrides)
	if len(merged) != 2 {
		t.Fatalf("expected 2 entries after adding override, got %d", len(merged))
	}

	// The new entry should be appended.
	newEntry := merged[1]
	if newEntry.BundleID != "com.new.app" {
		t.Errorf("new bundle_id: got %q, want %q", newEntry.BundleID, "com.new.app")
	}
	if newEntry.Category != CategoryFinancial {
		t.Errorf("new category: got %q, want %q", newEntry.Category, CategoryFinancial)
	}
	if !newEntry.FromOverride {
		t.Error("new entry should have FromOverride=true")
	}
}

// ---------------------------------------------------------------------------
// TestMergeLists_Empty
// ---------------------------------------------------------------------------

func TestMergeLists_Empty(t *testing.T) {
	defaults := []DenylistEntry{
		{BundleID: "com.apple.mail", Category: CategoryDestructive, Reason: "default"},
		{BundleID: "com.apple.Safari", Category: CategoryFinancial, Reason: "safari"},
	}

	merged := mergeLists(defaults, nil)
	if len(merged) != len(defaults) {
		t.Fatalf("expected %d entries with empty override, got %d", len(defaults), len(merged))
	}
	if merged[0].BundleID != defaults[0].BundleID {
		t.Errorf("first entry changed: got %q, want %q", merged[0].BundleID, defaults[0].BundleID)
	}
}

// ---------------------------------------------------------------------------
// TestIsDestructiveApp_BundleIDMatch
// ---------------------------------------------------------------------------

func TestIsDestructiveApp_BundleIDMatch(t *testing.T) {
	l := &Loader{custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatal(err)
	}

	c := l.IsDestructiveApp(ForegroundInfo{BundleID: "com.apple.mail"})
	if c.Category != CategoryDestructive {
		t.Errorf("expected CategoryDestructive, got %q", c.Category)
	}
	if !c.IsBlocked() {
		t.Error("expected IsBlocked() to be true")
	}
}

// ---------------------------------------------------------------------------
// TestIsDestructiveApp_BundleIDNoMatch
// ---------------------------------------------------------------------------

func TestIsDestructiveApp_BundleIDNoMatch(t *testing.T) {
	l := &Loader{custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatal(err)
	}

	c := l.IsDestructiveApp(ForegroundInfo{BundleID: "com.apple.notepad"})
	if c.Category != "" {
		t.Errorf("expected no match (empty category), got %q", c.Category)
	}
	if c.IsBlocked() {
		t.Error("expected IsBlocked() to be false for unknown app")
	}
}

// ---------------------------------------------------------------------------
// TestIsDestructiveApp_WindowClassMatch
// ---------------------------------------------------------------------------

func TestIsDestructiveApp_WindowClassMatch(t *testing.T) {
	l := &Loader{custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatal(err)
	}

	// Thunderbird with Compose title should match the linux entry.
	c := l.IsDestructiveApp(ForegroundInfo{
		WindowClass: "Thunderbird",
		WindowTitle: "Compose Message",
	})
	if c.Category != CategoryDestructive {
		t.Errorf("expected CategoryDestructive for Thunderbird Compose, got %q", c.Category)
	}
	if !c.IsBlocked() {
		t.Error("expected IsBlocked() to be true for Thunderbird Compose")
	}
}

// ---------------------------------------------------------------------------
// TestIsDestructiveApp_WindowClassNoMatch
// ---------------------------------------------------------------------------

func TestIsDestructiveApp_WindowClassNoMatch(t *testing.T) {
	l := &Loader{custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatal(err)
	}

	// "Notepad" is not in the denylist.
	c := l.IsDestructiveApp(ForegroundInfo{
		WindowClass: "Notepad",
		WindowTitle: "x",
	})
	if c.Category != "" {
		t.Errorf("expected no match for Notepad, got %q", c.Category)
	}
}

// ---------------------------------------------------------------------------
// TestIsDestructiveApp_TitleRegexMatch
// ---------------------------------------------------------------------------

func TestIsDestructiveApp_TitleRegexMatch(t *testing.T) {
	l := &Loader{custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatal(err)
	}

	// Safari with "Incognito" in the title should match the financial entry.
	c := l.IsDestructiveApp(ForegroundInfo{
		BundleID:    "com.apple.Safari",
		WindowTitle: "Incognito - New Tab",
	})
	if c.Category != CategoryFinancial {
		t.Errorf("expected CategoryFinancial for Safari Incognito, got %q", c.Category)
	}
	if !c.IsBlocked() {
		t.Error("expected IsBlocked() to be true for Safari Incognito")
	}
}

// ---------------------------------------------------------------------------
// TestIsDestructiveApp_TitleRegexNoMatch
// ---------------------------------------------------------------------------

func TestIsDestructiveApp_TitleRegexNoMatch(t *testing.T) {
	l := &Loader{custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatal(err)
	}

	// Safari with a normal title (no incognito/private) should NOT match.
	c := l.IsDestructiveApp(ForegroundInfo{
		BundleID:    "com.apple.Safari",
		WindowTitle: "My Normal Tab - Google",
	})
	if c.Category != "" {
		t.Errorf("expected no match for Safari with normal title, got %q", c.Category)
	}
}

// ---------------------------------------------------------------------------
// TestIsDestructiveApp_AllConditionsRequired
// ---------------------------------------------------------------------------

func TestIsDestructiveApp_AllConditionsRequired(t *testing.T) {
	l := &Loader{custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatal(err)
	}

	// com.apple.Terminal has a title_regex requiring sudo/rm -rf etc.
	// Providing the BundleID but a benign title should NOT match.
	c := l.IsDestructiveApp(ForegroundInfo{
		BundleID:    "com.apple.Terminal",
		WindowTitle: "my-user@host: ~",
	})
	if c.Category != "" {
		t.Errorf("expected no match for Terminal with benign title, got %q", c.Category)
	}

	// But with a destructive title it SHOULD match.
	c2 := l.IsDestructiveApp(ForegroundInfo{
		BundleID:    "com.apple.Terminal",
		WindowTitle: "sudo rm -rf /tmp",
	})
	if c2.Category != CategoryDestructive {
		t.Errorf("expected CategoryDestructive for Terminal with sudo, got %q", c2.Category)
	}
}

// ---------------------------------------------------------------------------
// TestIsDestructiveApp_CategoryMapping
// ---------------------------------------------------------------------------

func TestIsDestructiveApp_CategoryMapping(t *testing.T) {
	l := &Loader{custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		fg      ForegroundInfo
		wantCat Category
	}{
		{
			name:    "financial",
			fg:      ForegroundInfo{BundleID: "com.bankofamerica.BankofAmerica"},
			wantCat: CategoryFinancial,
		},
		{
			name:    "system",
			fg:      ForegroundInfo{BundleID: "com.apple.systempreferences"},
			wantCat: CategorySystem,
		},
		{
			name:    "destructive",
			fg:      ForegroundInfo{BundleID: "com.apple.mail"},
			wantCat: CategoryDestructive,
		},
		{
			name:    "password_manager",
			fg:      ForegroundInfo{BundleID: "com.agilebits.onepassword7"},
			wantCat: CategoryPasswordManager,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := l.IsDestructiveApp(tt.fg)
			if c.Category != tt.wantCat {
				t.Errorf("expected %q, got %q", tt.wantCat, c.Category)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestIsDestructiveApp_FromOverrideFlag
// ---------------------------------------------------------------------------

func TestIsDestructiveApp_FromOverrideFlag(t *testing.T) {
	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "overrides.json")
	data := []byte(`{
		"version": 1,
		"macos": [
			{"bundle_id": "com.apple.mail", "category": "system", "reason": "Override"}
		]
	}`)
	if err := os.WriteFile(overridePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Loader{overridePath: overridePath, custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatal(err)
	}

	c := l.IsDestructiveApp(ForegroundInfo{BundleID: "com.apple.mail"})
	if !c.FromOverride {
		t.Error("expected FromOverride=true for match from override file")
	}
	if c.Category != CategorySystem {
		t.Errorf("expected CategorySystem from override, got %q", c.Category)
	}

	// A default entry (not overridden) should have FromOverride=false.
	c2 := l.IsDestructiveApp(ForegroundInfo{BundleID: "com.apple.diskutility"})
	if c2.FromOverride {
		t.Error("expected FromOverride=false for default entry")
	}
	if c2.Category != CategoryDestructive {
		t.Errorf("expected CategoryDestructive for default diskutility, got %q", c2.Category)
	}
}

// ---------------------------------------------------------------------------
// TestLoader_ThreadSafe
// ---------------------------------------------------------------------------

func TestLoader_ThreadSafe(t *testing.T) {
	l := &Loader{custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatal(err)
	}

	const goroutines = 10
	const iterations = 100

	// Test concurrent reads (IsDestructiveApp).
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = l.IsDestructiveApp(ForegroundInfo{BundleID: "com.apple.mail"})
				_ = l.IsDestructiveApp(ForegroundInfo{WindowClass: "Thunderbird", WindowTitle: "Compose"})
				_ = l.IsDestructiveApp(ForegroundInfo{BundleID: "com.unknown"})
			}
		}()
	}
	wg.Wait()

	// Test concurrent reads + Reload (read + write lock).
	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "overrides.json")
	l.SetOverridePath(overridePath)

	for g := 0; g < goroutines; g++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = l.IsDestructiveApp(ForegroundInfo{BundleID: "com.apple.mail"})
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				// Write an empty valid override and reload — no error expected.
				os.WriteFile(overridePath, []byte(`{"version":1,"macos":[],"linux":[]}`), 0o644)
				_ = l.Reload()
			}
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// TestLoader_Reload
// ---------------------------------------------------------------------------

func TestLoader_Reload(t *testing.T) {
	l := &Loader{overridePath: "/nonexistent", custom: map[string]bool{}}
	if err := l.Reload(); err != nil {
		t.Fatal(err)
	}

	// Initially com.apple.mail is destructive from defaults.
	c := l.IsDestructiveApp(ForegroundInfo{BundleID: "com.apple.mail"})
	if c.Category != CategoryDestructive {
		t.Errorf("initial: expected CategoryDestructive, got %q", c.Category)
	}

	// Write a temp override file that changes com.apple.mail to system.
	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "overrides.json")
	data := []byte(`{"macos": [{"bundle_id": "com.apple.mail", "category": "system", "reason": "Override"}]}`)
	if err := os.WriteFile(overridePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	l.SetOverridePath(overridePath)
	if err := l.Reload(); err != nil {
		t.Fatal(err)
	}

	c = l.IsDestructiveApp(ForegroundInfo{BundleID: "com.apple.mail"})
	if c.Category != CategorySystem {
		t.Errorf("after reload: expected CategorySystem, got %q", c.Category)
	}
	if !c.FromOverride {
		t.Error("after reload: expected FromOverride=true")
	}
}

// ---------------------------------------------------------------------------
// TestLoader_OverridePath
// ---------------------------------------------------------------------------

func TestLoader_OverridePath(t *testing.T) {
	l := &Loader{custom: map[string]bool{}}

	// Set and get should round-trip.
	l.SetOverridePath("~/custom/overrides.json")
	got := l.OverridePath()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, "custom", "overrides.json")
	if got != want {
		t.Errorf("OverridePath: got %q, want %q", got, want)
	}

	// Non-tilde path should pass through unchanged.
	l.SetOverridePath("/absolute/path/overrides.json")
	got = l.OverridePath()
	if got != "/absolute/path/overrides.json" {
		t.Errorf("OverridePath with absolute path: got %q, want %q", got, "/absolute/path/overrides.json")
	}
}

// ---------------------------------------------------------------------------
// TestExpandPath
// ---------------------------------------------------------------------------

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("could not determine home directory")
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "tilde_slash_foo",
			in:   "~/foo",
			want: filepath.Join(home, "foo"),
		},
		{
			name: "tilde_alone",
			in:   "~",
			want: home,
		},
		{
			name: "tilde_deep_path",
			in:   "~/a/b/c",
			want: filepath.Join(home, "a", "b", "c"),
		},
		{
			name: "no_tilde_unchanged",
			in:   "/absolute/path",
			want: "/absolute/path",
		},
		{
			name: "empty_string",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandPath(tt.in)
			if got != tt.want {
				t.Errorf("expandPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestClassification_IsBlocked
// ---------------------------------------------------------------------------

func TestClassification_IsBlocked(t *testing.T) {
	// Empty category → not blocked.
	empty := Classification{}
	if empty.IsBlocked() {
		t.Error("empty Classification should not be blocked")
	}

	// Non-empty category → blocked.
	blocked := Classification{Category: CategoryDestructive}
	if !blocked.IsBlocked() {
		t.Error("Classification with CategoryDestructive should be blocked")
	}

	// Each category should be blocked.
	for _, cat := range []Category{
		CategoryFinancial,
		CategorySystem,
		CategoryDestructive,
		CategoryPasswordManager,
	} {
		c := Classification{Category: cat}
		if !c.IsBlocked() {
			t.Errorf("Classification with %q should be blocked", cat)
		}
	}
}

// ---------------------------------------------------------------------------
// TestInvalidRegex
// ---------------------------------------------------------------------------

func TestInvalidRegex(t *testing.T) {
	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "overrides.json")
	// Malformed regex: "[" is incomplete.
	data := []byte(`{
		"version": 1,
		"macos": [
			{"bundle_id": "com.bad.app", "window_class_regex": "[", "category": "destructive", "reason": "bad regex"}
		]
	}`)
	if err := os.WriteFile(overridePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Loader{overridePath: overridePath, custom: map[string]bool{}}
	err := l.Reload()
	if err == nil {
		t.Fatal("expected error for malformed regex, got nil")
	}

	// Also test with window_title_regex.
	overridePath2 := filepath.Join(tmpDir, "overrides2.json")
	data2 := []byte(`{
		"version": 1,
		"linux": [
			{"window_class_regex": "SomeApp", "window_title_regex": "[invalid", "category": "system", "reason": "bad title regex"}
		]
	}`)
	if err := os.WriteFile(overridePath2, data2, 0o644); err != nil {
		t.Fatal(err)
	}

	l2 := &Loader{overridePath: overridePath2, custom: map[string]bool{}}
	err2 := l2.Reload()
	if err2 == nil {
		t.Fatal("expected error for malformed title regex, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestMergeLists_InheritsTitleRegex
// ---------------------------------------------------------------------------

func TestMergeLists_InheritsTitleRegex(t *testing.T) {
	defaults := []DenylistEntry{
		{BundleID: "com.apple.Terminal", WindowTitleRegex: "(?i)sudo", Category: CategoryDestructive, Reason: "default"},
	}
	// Override replaces category but does NOT specify WindowTitleRegex.
	overrides := []DenylistEntry{
		{BundleID: "com.apple.Terminal", Category: CategorySystem, Reason: "override", FromOverride: true},
	}

	merged := mergeLists(defaults, overrides)
	if len(merged) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(merged))
	}

	entry := merged[0]
	if entry.Category != CategorySystem {
		t.Errorf("category: got %q, want %q", entry.Category, CategorySystem)
	}
	if entry.WindowTitleRegex != "(?i)sudo" {
		t.Errorf("WindowTitleRegex: got %q, want %q", entry.WindowTitleRegex, "(?i)sudo")
	}
	if !entry.FromOverride {
		t.Error("entry should have FromOverride=true after replacement")
	}
}

// ---------------------------------------------------------------------------
// TestInvalidEntry_NoMatchingCriteria
// ---------------------------------------------------------------------------

func TestInvalidEntry_NoMatchingCriteria(t *testing.T) {
	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "overrides.json")
	// Entry has neither bundle_id nor window_class_regex — should be rejected.
	data := []byte(`{
		"version": 1,
		"macos": [
			{"window_title_regex": "(?i)danger", "category": "destructive", "reason": "no matching criteria"}
		]
	}`)
	if err := os.WriteFile(overridePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Loader{overridePath: overridePath, custom: map[string]bool{}}
	err := l.Reload()
	if err == nil {
		t.Fatal("expected error for entry with no matching criteria, got nil")
	}

	// Also test compileEntries directly with a zero-criteria entry.
	_, err2 := compileEntries([]DenylistEntry{
		{WindowTitleRegex: ".*", Category: CategoryDestructive, Reason: "bare title regex"},
	})
	if err2 == nil {
		t.Fatal("compileEntries should reject entry with no BundleID and no WindowClassRegex")
	}
}
