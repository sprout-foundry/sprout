package agent

import (
	"strings"
	"testing"
)

func TestSplitIntoHunks_SingleHunk(t *testing.T) {
	original := "line1\nline2\nline3\nline4\nline5"
	proposed := "line1\nline2\nMODIFIED\nline4\nline5"

	hunks := SplitIntoHunks(original, proposed)

	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}

	h := hunks[0]
	if h.ID != "hunk-0" {
		t.Errorf("expected hunk ID 'hunk-0', got %q", h.ID)
	}

	// The hunk should have context + a remove + an add.
	var hasRemove, hasAdd bool
	for _, dl := range h.Lines {
		if dl.Type == DiffLineRemove && dl.Content == "line3" {
			hasRemove = true
		}
		if dl.Type == DiffLineAdd && dl.Content == "MODIFIED" {
			hasAdd = true
		}
	}
	if !hasRemove {
		t.Error("expected a removed line 'line3'")
	}
	if !hasAdd {
		t.Error("expected an added line 'MODIFIED'")
	}
}

func TestSplitIntoHunks_MultipleHunks(t *testing.T) {
	original := strings.Join([]string{
		"line1", "line2", "line3", "line4", "line5",
		"line6", "line7", "line8", "line9", "line10",
		"line11", "line12",
	}, "\n")
	// Change line2→CHANGED2 and line11→CHANGED11 (far apart = 2 hunks)
	proposed := strings.Join([]string{
		"line1", "CHANGED2", "line3", "line4", "line5",
		"line6", "line7", "line8", "line9", "line10",
		"CHANGED11", "line12",
	}, "\n")

	hunks := SplitIntoHunks(original, proposed)

	if len(hunks) < 2 {
		t.Fatalf("expected at least 2 hunks for two distant changes, got %d", len(hunks))
	}
}

func TestSplitIntoHunks_NoChanges(t *testing.T) {
	original := "line1\nline2\nline3"
	hunks := SplitIntoHunks(original, original)

	if len(hunks) != 0 {
		t.Fatalf("expected 0 hunks for identical content, got %d", len(hunks))
	}
}

func TestSplitIntoHunks_AllNew(t *testing.T) {
	original := ""
	proposed := "new1\nnew2\nnew3"

	hunks := SplitIntoHunks(original, proposed)

	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk for all-new content, got %d", len(hunks))
	}

	var addCount int
	for _, dl := range hunks[0].Lines {
		if dl.Type == DiffLineAdd {
			addCount++
		}
	}
	if addCount != 3 {
		t.Errorf("expected 3 added lines, got %d", addCount)
	}
}

func TestApplyHunks_AcceptAll(t *testing.T) {
	original := "line1\nline2\nline3\nline4\nline5"
	proposed := "line1\nline2\nMODIFIED\nline4\nline5"

	hunks := SplitIntoHunks(original, proposed)
	allIDs := hunkIDs(hunks)

	result := ApplyHunks(original, hunks, allIDs)

	if result != proposed {
		t.Errorf("accept-all should reproduce proposed content.\ngot:  %q\nwant: %q", result, proposed)
	}
}

func TestApplyHunks_RejectAll(t *testing.T) {
	original := "line1\nline2\nline3\nline4\nline5"
	proposed := "line1\nline2\nMODIFIED\nline4\nline5"

	hunks := SplitIntoHunks(original, proposed)

	// Reject all by passing no accepted IDs.
	result := ApplyHunks(original, hunks, []string{})

	if result != original {
		t.Errorf("reject-all should preserve original content.\ngot:  %q\nwant: %q", result, original)
	}
}

func TestApplyHunks_PartialAccept_MultiHunk(t *testing.T) {
	original := strings.Join([]string{
		"line1", "line2", "line3", "line4", "line5",
		"line6", "line7", "line8", "line9", "line10",
		"line11", "line12",
	}, "\n")
	proposed := strings.Join([]string{
		"line1", "CHANGED2", "line3", "line4", "line5",
		"line6", "line7", "line8", "line9", "line10",
		"CHANGED11", "line12",
	}, "\n")

	hunks := SplitIntoHunks(original, proposed)

	if len(hunks) < 2 {
		t.Fatalf("test requires >=2 hunks, got %d", len(hunks))
	}

	// Accept only the first hunk.
	result := ApplyHunks(original, hunks, []string{hunks[0].ID})

	// Should contain CHANGED2 (accepted) but not CHANGED11 (rejected).
	if !strings.Contains(result, "CHANGED2") {
		t.Error("expected CHANGED2 in result (first hunk accepted)")
	}
	if strings.Contains(result, "CHANGED11") {
		t.Error("expected CHANGED11 to NOT be in result (second hunk rejected)")
	}
	// line2 should not be present since hunk-0 was applied.
	if strings.Contains(result, "line2\n") || result == "line2" || strings.HasPrefix(result, "line2\n") {
		t.Error("expected original line2 to be replaced by CHANGED2")
	}
}

func TestApplyHunks_PartialAccept_SingleHunk(t *testing.T) {
	original := "line1\nline2\nline3\nline4\nline5"
	proposed := "line1\nline2\nMODIFIED\nline4\nline5"

	hunks := SplitIntoHunks(original, proposed)

	// Accept first hunk (only hunk). Result should be the proposed content.
	result := ApplyHunks(original, hunks, []string{hunks[0].ID})
	if result != proposed {
		t.Errorf("accepting the only hunk should give proposed.\ngot:  %q\nwant: %q", result, proposed)
	}
}

func TestApplyHunks_EmptyDiff(t *testing.T) {
	original := "line1\nline2\nline3"
	hunks := SplitIntoHunks(original, original)

	result := ApplyHunks(original, hunks, []string{})
	if result != original {
		t.Errorf("empty diff should return original.\ngot:  %q\nwant: %q", result, original)
	}
}

func TestGenerateUnifiedDiff(t *testing.T) {
	original := "line1\nline2\nline3"
	proposed := "line1\nMODIFIED\nline3"

	diff := GenerateUnifiedDiff("test.go", original, proposed)

	if !strings.Contains(diff, "---") {
		t.Error("expected '---' header in unified diff")
	}
	if !strings.Contains(diff, "+++") {
		t.Error("expected '+++' header in unified diff")
	}
	if !strings.Contains(diff, "-line2") {
		t.Error("expected '-line2' removed line in unified diff")
	}
	if !strings.Contains(diff, "+MODIFIED") {
		t.Error("expected '+MODIFIED' added line in unified diff")
	}
}

func TestGenerateUnifiedDiff_NoChanges(t *testing.T) {
	original := "line1\nline2\nline3"
	diff := GenerateUnifiedDiff("test.go", original, original)

	if diff != "" {
		t.Errorf("expected empty diff for identical content, got %q", diff)
	}
}

func TestHunkIDs(t *testing.T) {
	hunks := []Hunk{
		{ID: "hunk-0"},
		{ID: "hunk-1"},
		{ID: "hunk-2"},
	}
	ids := hunkIDs(hunks)

	if len(ids) != 3 {
		t.Fatalf("expected 3 IDs, got %d", len(ids))
	}
	for i, id := range ids {
		expected := "hunk-" + string(rune('0'+i))
		if id != expected {
			t.Errorf("expected %q, got %q", expected, id)
		}
	}
}

func TestRejectedHunkList(t *testing.T) {
	hunks := []Hunk{
		{ID: "hunk-0", OldStart: 1, OldLines: 3},
		{ID: "hunk-1", OldStart: 10, OldLines: 5},
		{ID: "hunk-2", OldStart: 20, OldLines: 2},
	}

	// Accept hunk-0 and hunk-2; reject hunk-1.
	accepted := []string{"hunk-0", "hunk-2"}
	list := rejectedHunkList(hunks, accepted)

	if !strings.Contains(list, "hunk-1") {
		t.Errorf("expected 'hunk-1' in rejected list, got %q", list)
	}
	if strings.Contains(list, "hunk-0") {
		t.Errorf("did not expect 'hunk-0' in rejected list, got %q", list)
	}
}

func TestRejectedHunkList_NoneRejected(t *testing.T) {
	hunks := []Hunk{
		{ID: "hunk-0", OldStart: 1, OldLines: 3},
	}
	list := rejectedHunkList(hunks, []string{"hunk-0"})
	if list != "none" {
		t.Errorf("expected 'none', got %q", list)
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
	}{
		{"empty", "", 1},
		{"single line", "hello", 1},
		{"two lines", "a\nb", 2},
		{"trailing newline", "a\nb\n", 2},
		{"only newlines", "\n\n", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := splitLines(tt.input)
			if len(lines) != tt.wantLen {
				t.Errorf("splitLines(%q) = %d lines, want %d", tt.input, len(lines), tt.wantLen)
			}
		})
	}
}

func TestApplyHunks_Insertion(t *testing.T) {
	original := "line1\nline2\nline3"
	proposed := "line1\nINSERTED\nline2\nline3"

	hunks := SplitIntoHunks(original, proposed)
	allIDs := hunkIDs(hunks)

	result := ApplyHunks(original, hunks, allIDs)

	if result != proposed {
		t.Errorf("insertion not applied correctly.\ngot:  %q\nwant: %q", result, proposed)
	}
}

func TestApplyHunks_Deletion(t *testing.T) {
	original := "line1\nline2\nline3"
	proposed := "line1\nline3"

	hunks := SplitIntoHunks(original, proposed)
	allIDs := hunkIDs(hunks)

	result := ApplyHunks(original, hunks, allIDs)

	if result != proposed {
		t.Errorf("deletion not applied correctly.\ngot:  %q\nwant: %q", result, proposed)
	}
}

func TestApplyHunks_AcceptHunkByID(t *testing.T) {
	original := strings.Join([]string{
		"a1", "a2", "a3", "a4", "a5",
		"a6", "a7", "a8", "a9", "a10",
		"a11", "a12",
	}, "\n")
	proposed := strings.Join([]string{
		"a1", "B2", "a3", "a4", "a5",
		"a6", "a7", "a8", "a9", "a10",
		"B11", "a12",
	}, "\n")

	hunks := SplitIntoHunks(original, proposed)

	// Reject all by default, accept only a specific hunk.
	var firstHunkID string
	if len(hunks) > 0 {
		firstHunkID = hunks[0].ID
	}

	result := ApplyHunks(original, hunks, []string{firstHunkID})

	// First change should be present.
	if !strings.Contains(result, "B2") {
		t.Error("expected B2 from accepted first hunk")
	}
	// Second change should be absent.
	if strings.Contains(result, "B11") {
		t.Error("expected B11 to be absent (rejected hunk)")
	}
}
