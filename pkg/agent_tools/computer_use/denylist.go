// Package computer_use denylist loader.
//
// This file defines the thread-safe Loader that parses the embedded default
// denylist (denylist.json) plus an optional per-user override file, then
// classifies a ForegroundInfo against the merged effective list.
//
// Override semantics: an override entry with the same BundleID or
// WindowClassRegex as a default entry REPLACES the default (override wins).
// A new override entry with no matching default is ADDED. Overrides keep
// FromOverride=true so callers can distinguish source.
package computer_use

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

//go:embed denylist.json
var defaultDenylistJSON []byte

// Category classifies WHY an app is on the denylist.
type Category string

const (
	CategoryFinancial       Category = "financial"
	CategorySystem          Category = "system"
	CategoryDestructive     Category = "destructive"
	CategoryPasswordManager Category = "password_manager"
)

// DenylistEntry is one entry in the effective denylist.
type DenylistEntry struct {
	// BundleID is the macOS bundle identifier (e.g., "com.apple.Safari").
	BundleID string

	// WindowClassRegex is the X11 WM_CLASS[Name] pattern.
	WindowClassRegex string

	// WindowTitleRegex is an optional title pattern that must ALSO match.
	WindowTitleRegex string

	// Category is one of the Category constants.
	Category Category

	// Reason is a human-readable explanation.
	Reason string

	// FromOverride is true when the entry came from the user override file.
	// Populated at load time, not serialized.
	FromOverride bool

	// classRegex and titleRegex are compiled at load time.
	classRegex *regexp.Regexp
	titleRegex *regexp.Regexp
}

// Classification is the result of matching a ForegroundInfo against the
// effective denylist.
type Classification struct {
	Category     Category
	Reason       string
	MatchedEntry DenylistEntry
	FromOverride bool
}

// IsBlocked returns true when the classification indicates a match.
func (c Classification) IsBlocked() bool {
	return c.Category != ""
}

// DefaultOverridePath is the default per-user override file path.
const DefaultOverridePath = "~/.config/sprout/computer_use_denylist_overrides.json"

// Loader is a thread-safe cache of the parsed default + override denylist.
type Loader struct {
	mu           sync.RWMutex
	list         []DenylistEntry
	custom       map[string]bool // reserved for v2: per-session "always allow this app" tracking
	overridePath string
}

var (
	defaultLoaderOnce sync.Once
	defaultLoader     *Loader
)

// DefaultLoader returns the singleton denylist loader.
func DefaultLoader() *Loader {
	defaultLoaderOnce.Do(func() {
		l := &Loader{
			overridePath: expandPath(DefaultOverridePath),
			custom:       map[string]bool{},
		}
		if err := l.Reload(); err != nil {
			panic(fmt.Sprintf("default denylist load failed (build error): %v", err))
		}
	})
	return defaultLoader
}

// Reload reloads the default + override lists. Tests call this after
// modifying the override file via SetOverridePath.
func (l *Loader) Reload() error {
	defaults, err := loadDefaultList()
	if err != nil {
		return fmt.Errorf("load default denylist: %w", err)
	}
	overrides, err := loadOverrideFile(l.overridePath)
	if err != nil {
		return fmt.Errorf("load override file %q: %w", l.overridePath, err)
	}
	merged := mergeLists(defaults, overrides)
	compiled, err := compileEntries(merged)
	if err != nil {
		return fmt.Errorf("compile regexes: %w", err)
	}
	l.mu.Lock()
	l.list = compiled
	l.mu.Unlock()
	return nil
}

// SetOverridePath sets the override file path. Next Reload() reads from new path.
func (l *Loader) SetOverridePath(path string) {
	l.mu.Lock()
	l.overridePath = expandPath(path)
	l.mu.Unlock()
}

// OverridePath returns the current override file path.
func (l *Loader) OverridePath() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.overridePath
}

// IsDestructiveApp classifies a foreground-app tuple against the effective denylist.
func (l *Loader) IsDestructiveApp(fg ForegroundInfo) Classification {
	l.mu.RLock()
	list := l.list
	l.mu.RUnlock()

	for i := range list {
		entry := list[i]
		if !matchesEntry(entry, fg) {
			continue
		}
		return Classification{
			Category:     entry.Category,
			Reason:       entry.Reason,
			MatchedEntry: entry,
			FromOverride: entry.FromOverride,
		}
	}
	return Classification{}
}

// matchesEntry reports whether entry matches fg. Pure function — no locks.
func matchesEntry(entry DenylistEntry, fg ForegroundInfo) bool {
	if entry.BundleID != "" {
		if fg.BundleID != entry.BundleID {
			return false
		}
	}
	if entry.classRegex != nil {
		if !entry.classRegex.MatchString(fg.WindowClass) {
			return false
		}
	}
	if entry.titleRegex != nil {
		if !entry.titleRegex.MatchString(fg.WindowTitle) {
			return false
		}
	}
	return true
}

// jsonEntry is the JSON shape for both macOS and Linux entries.
type jsonEntry struct {
	BundleID         string `json:"bundle_id,omitempty"`
	WindowClassRegex string `json:"window_class_regex,omitempty"`
	WindowTitleRegex string `json:"window_title_regex,omitempty"`
	Category         string `json:"category"`
	Reason           string `json:"reason"`
}

type denylistJSON struct {
	Version int         `json:"version"`
	Macos   []jsonEntry `json:"macos"`
	Linux   []jsonEntry `json:"linux"`
}

// loadDefaultList parses the embedded denylist.json.
func loadDefaultList() ([]DenylistEntry, error) {
	var raw denylistJSON
	if err := json.Unmarshal(defaultDenylistJSON, &raw); err != nil {
		return nil, fmt.Errorf("parse embedded denylist.json: %w", err)
	}
	out := make([]DenylistEntry, 0, len(raw.Macos)+len(raw.Linux))
	for _, e := range raw.Macos {
		out = append(out, DenylistEntry{
			BundleID:         e.BundleID,
			WindowClassRegex: e.WindowClassRegex,
			WindowTitleRegex: e.WindowTitleRegex,
			Category:         Category(e.Category),
			Reason:           e.Reason,
		})
	}
	for _, e := range raw.Linux {
		out = append(out, DenylistEntry{
			BundleID:         e.BundleID,
			WindowClassRegex: e.WindowClassRegex,
			WindowTitleRegex: e.WindowTitleRegex,
			Category:         Category(e.Category),
			Reason:           e.Reason,
		})
	}
	return out, nil
}

// loadOverrideFile reads the override file at path. Missing file → no error,
// returns empty list. Invalid JSON → error.
func loadOverrideFile(path string) ([]DenylistEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read override file: %w", err)
	}
	var raw denylistJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse override JSON: %w", err)
	}
	out := make([]DenylistEntry, 0, len(raw.Macos)+len(raw.Linux))
	for _, e := range raw.Macos {
		out = append(out, DenylistEntry{
			BundleID:         e.BundleID,
			WindowClassRegex: e.WindowClassRegex,
			WindowTitleRegex: e.WindowTitleRegex,
			Category:         Category(e.Category),
			Reason:           e.Reason,
			FromOverride:     true,
		})
	}
	for _, e := range raw.Linux {
		out = append(out, DenylistEntry{
			BundleID:         e.BundleID,
			WindowClassRegex: e.WindowClassRegex,
			WindowTitleRegex: e.WindowTitleRegex,
			Category:         Category(e.Category),
			Reason:           e.Reason,
			FromOverride:     true,
		})
	}
	return out, nil
}

// mergeLists applies override semantics:
//   - Override entry with same BundleID or WindowClassRegex REPLACES default entry.
//   - Override entry with new key ADDS to effective list.
//   - Override entries retain FromOverride=true; defaults retain FromOverride=false.
func mergeLists(defaults, overrides []DenylistEntry) []DenylistEntry {
	if len(overrides) == 0 {
		return defaults
	}
	out := make([]DenylistEntry, len(defaults))
	copy(out, defaults)
	for _, ov := range overrides {
		replaced := false
		for i := range out {
			if entriesMatch(out[i], ov) {
				ovCopy := ov
				if ovCopy.BundleID == "" {
					ovCopy.BundleID = out[i].BundleID
				}
				if ovCopy.WindowClassRegex == "" {
					ovCopy.WindowClassRegex = out[i].WindowClassRegex
				}
				if ovCopy.WindowTitleRegex == "" {
					ovCopy.WindowTitleRegex = out[i].WindowTitleRegex
				}
				out[i] = ovCopy
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, ov)
		}
	}
	return out
}

// entriesMatch reports whether two entries target the same app.
func entriesMatch(a, b DenylistEntry) bool {
	if a.BundleID != "" && b.BundleID != "" {
		return a.BundleID == b.BundleID
	}
	if a.WindowClassRegex != "" && b.WindowClassRegex != "" {
		return a.WindowClassRegex == b.WindowClassRegex
	}
	return false
}

// compileEntries compiles all regex patterns in entries. Invalid regex → error.
func compileEntries(entries []DenylistEntry) ([]DenylistEntry, error) {
	for i, e := range entries {
		if e.BundleID == "" && e.WindowClassRegex == "" {
			return nil, fmt.Errorf("denylist entry %d must have bundle_id or window_class_regex (reason=%q)", i, e.Reason)
		}
	}
	out := make([]DenylistEntry, len(entries))
	for i, e := range entries {
		out[i] = e
		if e.WindowClassRegex != "" {
			re, err := regexp.Compile(e.WindowClassRegex)
			if err != nil {
				return nil, fmt.Errorf("compile window_class_regex %q: %w", e.WindowClassRegex, err)
			}
			out[i].classRegex = re
		}
		if e.WindowTitleRegex != "" {
			re, err := regexp.Compile(e.WindowTitleRegex)
			if err != nil {
				return nil, fmt.Errorf("compile window_title_regex %q: %w", e.WindowTitleRegex, err)
			}
			out[i].titleRegex = re
		}
	}
	return out, nil
}

// expandPath expands ~ to the user's home directory.
func expandPath(path string) string {
	if path == "" || !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
