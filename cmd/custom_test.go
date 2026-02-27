package cmd

import "testing"

func TestParseToolCallList(t *testing.T) {
	got := parseToolCallList(" read_file,write_file, read_file , ,search_files ")
	if len(got) != 3 {
		t.Fatalf("expected 3 tool names, got %d", len(got))
	}
	if got[0] != "read_file" {
		t.Fatalf("expected first tool to be read_file, got %q", got[0])
	}
	if got[1] != "write_file" {
		t.Fatalf("expected second tool to be write_file, got %q", got[1])
	}
	if got[2] != "search_files" {
		t.Fatalf("expected third tool to be search_files, got %q", got[2])
	}
}
