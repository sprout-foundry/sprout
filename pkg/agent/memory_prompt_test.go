package agent

import (
	"fmt"
	"strings"
	"testing"
)

func TestNormalizeMemoryPromptMode(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"", "index", true},
		{"full", "full", true},
		{"INDEX", "index", true},
		{"  off  ", "off", true},
		{"ful", "full", false},
		{"body", "full", false},
	}
	for _, tc := range cases {
		got, ok := normalizeMemoryPromptMode(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("normalizeMemoryPromptMode(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestLoadMemoriesForPromptMode(t *testing.T) {
	setupMemoryDirForTest(t)
	if err := SaveMemory("alpha", "# Alpha\n\nPrefer table-driven tests.\nSecond line of the body."); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}
	if err := SaveMemory("beta", "---\ntitle: beta\n---\n# Beta\nUse merge, never rebase."); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}

	unset := LoadMemoriesForPromptMode("", 0)
	full := LoadMemoriesForPromptMode("full", 0)
	if !strings.Contains(full, "### alpha\n") || !strings.Contains(full, "Second line of the body.") {
		t.Fatalf("full mode lacks a body:\n%s", full)
	}
	if full != LoadMemoriesForPrompt() {
		t.Fatalf("explicit full mode differs from LoadMemoriesForPrompt")
	}

	index := LoadMemoriesForPromptMode("index", 0)
	for _, want := range []string{"- `alpha`: Prefer table-driven tests.", "- `beta`: Use merge, never rebase.", "manage_memory"} {
		if !strings.Contains(index, want) {
			t.Fatalf("index mode lacks %q:\n%s", want, index)
		}
	}
	if strings.Contains(index, "Second line of the body.") {
		t.Fatalf("index mode carries a body:\n%s", index)
	}
	if unset != index {
		t.Fatalf("unset mode should normalize to index:\nunset:\n%s\nindex:\n%s", unset, index)
	}
	if len(index) >= len(full) {
		t.Fatalf("index (%d bytes) not smaller than full (%d)", len(index), len(full))
	}

	if got := LoadMemoriesForPromptMode("off", 0); got != "" {
		t.Fatalf("off mode = %q", got)
	}

	capped := LoadMemoriesForPromptMode("full", 60)
	if !strings.Contains(capped, "omitted") || strings.Contains(capped, "### beta") {
		t.Fatalf("byte cap not applied:\n%s", capped)
	}
}

func TestMemorySummary(t *testing.T) {
	long := strings.Repeat("x", memoryIndexSummaryChars+10)
	cases := map[string]string{
		"# Title\n\nfirst line\nsecond": "first line",
		"---\na: b\n---\n## H\nbody":    "body",
		"":                              "(no summary)",
		long:                            strings.Repeat("x", memoryIndexSummaryChars) + "…",
	}
	for in, want := range cases {
		if got := memorySummary(in); got != want {
			t.Errorf("memorySummary(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRenderStableUnderEdit covers the prefix-stability contract: rendering a set of
// memories, editing one body (and separately adding one), must leave every other entry
// byte-identical between the two renderings — so a session's prefix still matches after
// an edit.
func TestRenderStableUnderEdit(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	seed := map[string]string{
		"alpha":   "# Alpha\nAlpha body line one.\nAlpha body line two.",
		"bravo":   "# Bravo\nBravo body line one.\nBravo body line two.",
		"charlie": "# Charlie\nCharlie body line one.\nCharlie body line two.",
	}
	for name, body := range seed {
		if err := SaveMemory(name, body); err != nil {
			t.Fatalf("SaveMemory(%s): %v", name, err)
		}
	}

	for _, mode := range []string{"full", "index"} {
		t.Run(mode, func(t *testing.T) {
			// Edit one body.
			before := LoadMemoriesForPromptMode(mode, 0)
			if err := SaveMemory("charlie", "# Charlie\nCharlie body line one.\nCharlie body line EDITED."); err != nil {
				t.Fatalf("SaveMemory edit: %v", err)
			}
			afterEdit := LoadMemoriesForPromptMode(mode, 0)
			assertEntriesIdenticalOutside(t, mode, "charlie", before, afterEdit)

			// Add one.
			beforeAdd := LoadMemoriesForPromptMode(mode, 0)
			if err := SaveMemory("delta", "# Delta\nDelta body."); err != nil {
				t.Fatalf("SaveMemory add: %v", err)
			}
			afterAdd := LoadMemoriesForPromptMode(mode, 0)
			assertEntriesIdenticalOutside(t, mode, "delta", beforeAdd, afterAdd)
		})
	}
}

// TestRenderStableUnderOversizedNeighbor proves a memory far over the per-memory cap is
// omitted on its own and cannot change which other memories are included.
func TestRenderStableUnderOversizedNeighbor(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	if err := SaveMemory("alpha", "# Alpha\nAlpha body."); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}
	if err := SaveMemory("bravo", "# Bravo\nBravo body."); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}
	if err := SaveMemory("giant", strings.Repeat("x", maxMemoryEntryBytes+10)); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}

	withGiant := LoadMemoriesForPromptMode("full", 0)
	withoutGiant := func() string {
		if err := DeleteMemory("giant"); err != nil {
			t.Fatalf("DeleteMemory: %v", err)
		}
		defer func() {
			// Re-add the giant so the shared temp-dir state reflects the seed again.
			if err := SaveMemory("giant", strings.Repeat("x", maxMemoryEntryBytes+10)); err != nil {
				t.Errorf("SaveMemory restore: %v", err)
			}
		}()
		return LoadMemoriesForPromptMode("full", 0)
	}()

	// The giant is omitted on its own; the small entries render either way.
	if !strings.Contains(withGiant, "1 memory file(s) omitted") {
		t.Errorf("expected omission notice naming 1 omitted memory:\n%s", withGiant)
	}
	if strings.Contains(withGiant, "### giant") {
		t.Errorf("giant must not render above the per-memory cap:\n%s", withGiant)
	}
	for _, name := range []string{"alpha", "bravo"} {
		before := entryFor(t, "full", withGiant, name)
		after := entryFor(t, "full", withoutGiant, name)
		if before != after {
			t.Errorf("entry %s changed when giant was added:\nwith giant: %s\nwithout giant: %s", name, before, after)
		}
	}
	if strings.Contains(withoutGiant, "omitted") {
		t.Errorf("without giant, no omission notice expected:\n%s", withoutGiant)
	}
}

// TestLeadInDeterministicAndVolatileFree covers the requirement that neither the lead-in
// nor the per-entry wrapper carries timestamps, counts or other volatile text; consecutive
// renderings must be byte-identical.
func TestLeadInDeterministicAndVolatileFree(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	if err := SaveMemory("alpha", "# Alpha\nBody alpha."); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}
	if err := SaveMemory("bravo", "# Bravo\nBody bravo."); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}

	for _, mode := range []string{"full", "index"} {
		t.Run(mode, func(t *testing.T) {
			first := LoadMemoriesForPromptMode(mode, 0)
			second := LoadMemoriesForPromptMode(mode, 0)
			if first != second {
				t.Errorf("two consecutive renderings differ (%s):\nfirst:\n%s\nsecond:\n%s", mode, first, second)
			}
			// Lead-in is everything before the first entry marker.
			marker := "### "
			if mode == "index" {
				marker = "- `"
			}
			i := strings.Index(first, marker)
			if i == -1 {
				t.Fatalf("no entry found in %s rendering:\n%s", mode, first)
			}
			leadIn := first[:i]
			// The lead-in must not carry a date, byte total, count, or timestamp.
			for _, r := range leadIn {
				if r >= '0' && r <= '9' {
					t.Errorf("digit %q in lead-in (%s):\n%s", r, mode, leadIn)
					break
				}
			}
			if strings.Contains(leadIn, "count") || strings.Contains(leadIn, "timestamp") {
				t.Errorf("volatile text in lead-in (%s):\n%s", mode, leadIn)
			}
			// The per-entry wrapper must be name + content only — no byte totals.
			for _, part := range strings.Split(first[i:], "\n\n") {
				if !strings.Contains(part, "alpha") && !strings.Contains(part, "bravo") && !strings.Contains(part, "###") {
					continue
				}
				if strings.Contains(part, "bytes") || strings.Contains(part, "count") {
					t.Errorf("volatile text in per-entry wrapper (%s):\n%s", mode, part)
				}
			}
		})
	}
}

// assertEntriesIdenticalOutside asserts that, for mode, every entry except `changedName`
// is byte-identical between before and after.
func assertEntriesIdenticalOutside(t *testing.T, mode, changedName, before, after string) {
	t.Helper()
	beforeParts := splitEntries(mode, before)
	afterParts := splitEntries(mode, after)

	for name, entry := range beforeParts {
		if name == changedName {
			continue
		}
		afterEntry, ok := afterParts[name]
		if !ok {
			t.Errorf("entry %q disappeared (%s):\nbefore: %s", name, mode, entry)
			continue
		}
		if afterEntry != entry {
			t.Errorf("entry %q changed (%s):\nbefore: %s\nafter: %s", name, mode, entry, afterEntry)
		}
	}
}

// entryFor extracts the rendered block for one memory name from the rendering.
func entryFor(t *testing.T, mode, rendering, name string) string {
	t.Helper()
	parts := splitEntries(mode, rendering)
	entry, ok := parts[name]
	if !ok {
		t.Fatalf("entry %q missing from rendering:\n%s", name, rendering)
	}
	return entry
}

// splitEntries extracts each memory's rendered block keyed by memory name. It ignores
// the shared lead-in and the omission notice.
func splitEntries(mode, rendering string) map[string]string {
	entries := map[string]string{}
	// Drop the omission notice — it is not part of any entry.
	noticeIdx := strings.Index(rendering, "*[")
	if noticeIdx != -1 {
		rendering = strings.TrimSuffix(strings.TrimSpace(rendering[:noticeIdx]), "\n\n")
	}
	if mode == "full" {
		idx := strings.Index(rendering, "### ")
		if idx == -1 {
			return entries
		}
		rest := rendering[idx:]
		for _, part := range strings.Split(rest, "### ") {
			if part == "" {
				continue
			}
			nl := strings.Index(part, "\n")
			if nl == -1 {
				continue
			}
			name := strings.TrimSpace(part[:nl])
			// Strip the trailing "\n\n" before the next header.
			body := strings.TrimSuffix(part, "\n\n")
			body = strings.TrimSuffix(body, "\n")
			entries[name] = "### " + body
		}
	} else {
		for _, line := range strings.Split(rendering, "\n") {
			if !strings.HasPrefix(line, "- `") {
				continue
			}
			closeIdx := strings.Index(line, "`")
			if closeIdx == -1 {
				continue
			}
			close2 := strings.Index(line[closeIdx+1:], "`")
			if close2 == -1 {
				continue
			}
			name := line[closeIdx+1 : closeIdx+1+close2]
			entries[name] = line
		}
	}
	return entries
}

// TestOuterGuardCountsRemaining covers the total-bytes outer guard: memories that each
// fit under the per-memory cap but together exceed memory_prompt_max_bytes render only
// the leading ones, and the notice reports how many were omitted.
func TestOuterGuardCountsRemaining(t *testing.T) {
	cleanup := setupMemoryDirForTest(t)
	defer cleanup()

	// Each entry is ~4KB: under the 10KB per-memory cap, but 14 of them exceed 50KB.
	smallBody := strings.Repeat("data ", 1000) // ~5KB
	for i := 0; i < 14; i++ {
		name := fmt.Sprintf("mem%02d", i)
		if err := SaveMemory(name, "# "+name+"\n"+smallBody); err != nil {
			t.Fatalf("SaveMemory: %v", err)
		}
	}

	result := LoadMemoriesForPromptMode("full", 0) // outer guard 50KB default
	if !strings.Contains(result, "total guard") {
		t.Errorf("expected total-guard omission notice:\n%s", result)
	}
	// mem00 and mem01 must render; mem13 must not.
	if !strings.Contains(result, "### mem00") || !strings.Contains(result, "### mem01") {
		t.Errorf("leading entries missing:\n%s", result)
	}
	if strings.Contains(result, "### mem13") {
		t.Errorf("trailing entry rendered beyond the outer guard:\n%s", result)
	}
}
