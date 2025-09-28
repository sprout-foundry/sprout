package components

import (
    "testing"
    "github.com/alantheprice/ledit/pkg/ui"
)

func TestBuildQuickPromptLine(t *testing.T) {
    opts := []ui.QuickOption{
        {Label: "Proceed"},
        {Label: "Edit"},
        {Label: "Retry"},
        {Label: "Cancel"},
    }
    line := BuildQuickPromptLine("Proceed with commit?", opts, true)
    if line == "" {
        t.Fatal("expected non-empty prompt line")
    }
    // Should contain labels and numeric hotkeys [1].. etc
    expectedSubs := []string{"Proceed with commit?", "[1] Proceed", "[2] Edit", "[3] Retry", "[4] Cancel"}
    for _, sub := range expectedSubs {
        if !contains(line, sub) {
            t.Fatalf("prompt line missing substring: %q in %q", sub, line)
        }
    }
}

func TestBuildQuickPromptLinesAutoSwitch(t *testing.T) {
    opts := []ui.QuickOption{
        {Label: "Proceed"},
        {Label: "Edit"},
        {Label: "Retry"},
        {Label: "Cancel"},
    }
    // Wide width -> single line
    linesWide := BuildQuickPromptLines("Proceed with commit?", opts, 120, true)
    if len(linesWide) != 1 {
        t.Fatalf("expected single line for wide width, got %d", len(linesWide))
    }
    // Narrow width -> vertical lines (prompt + N options)
    linesNarrow := BuildQuickPromptLines("Proceed with commit?", opts, 20, true)
    if len(linesNarrow) != 1+len(opts) {
        t.Fatalf("expected vertical block %d lines, got %d", 1+len(opts), len(linesNarrow))
    }
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (s == sub || (len(s) > len(sub) && (indexOf(s, sub) >= 0))) }

func indexOf(s, sub string) int {
    for i := 0; i+len(sub) <= len(s); i++ {
        if s[i:i+len(sub)] == sub { return i }
    }
    return -1
}
