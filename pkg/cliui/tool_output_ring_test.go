package cliui

import (
	"strings"
	"testing"
)

func TestToolOutputRing_KeepsNewestAndEvictsOldest(t *testing.T) {
	ResetToolOutputs()
	t.Cleanup(ResetToolOutputs)

	for i := 1; i <= ToolOutputRingSize+3; i++ {
		RecordToolOutput("read_file", "f.txt", "completed", "body")
	}

	entries := RecentToolOutputs()
	if len(entries) != ToolOutputRingSize {
		t.Fatalf("ring holds %d entries, want %d", len(entries), ToolOutputRingSize)
	}
	if entries[0].Seq != 4 {
		t.Fatalf("oldest entry is seq %d, want 4 (first three evicted)", entries[0].Seq)
	}
	if last := entries[len(entries)-1]; last.Seq != ToolOutputRingSize+3 {
		t.Fatalf("newest entry is seq %d, want %d", last.Seq, ToolOutputRingSize+3)
	}
}

func TestToolOutputRing_RecordsEmptyOutput(t *testing.T) {
	ResetToolOutputs()
	t.Cleanup(ResetToolOutputs)

	RecordToolOutput("grep", "needle", "completed", "")

	entries := RecentToolOutputs()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 — an empty result is still addressable", len(entries))
	}
	if got := FormatToolOutputList(entries); !strings.Contains(got, "(no output)") {
		t.Fatalf("list does not mark the empty result: %q", got)
	}
}

func TestFindToolOutput_BySeqAndDefault(t *testing.T) {
	entries := []ToolOutput{
		{Seq: 1, ToolName: "a", Output: "one"},
		{Seq: 2, ToolName: "b", Output: "two"},
	}

	got, ok := FindToolOutput(entries, "1")
	if !ok || got.Output != "one" {
		t.Fatalf("seq lookup returned %+v ok=%v", got, ok)
	}

	got, ok = FindToolOutput(entries, "")
	if !ok || got.Seq != 2 {
		t.Fatalf("empty query should select the newest entry, got %+v ok=%v", got, ok)
	}

	if _, ok := FindToolOutput(entries, "9"); ok {
		t.Fatal("unknown seq resolved")
	}
	if _, ok := FindToolOutput(nil, ""); ok {
		t.Fatal("empty ring resolved a query")
	}
}

func TestFormatToolOutputList_Empty(t *testing.T) {
	got := FormatToolOutputList(nil)
	if !strings.Contains(got, "No tool output buffered") {
		t.Fatalf("empty list text = %q", got)
	}
}
