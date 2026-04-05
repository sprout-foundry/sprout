package cmd

import (
	"bufio"
	"os"
	"testing"
)

func TestPromptLine(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		prompt  string
		want    string
		wantErr bool
	}{
		{name: "simple input", input: "hello\n", prompt: "> ", want: "hello"},
		{name: "trims whitespace", input: "  hello world  \n", prompt: "> ", want: "hello world"},
		{name: "empty input", input: "\n", prompt: "> ", want: ""},
		{name: "spaces trimmed to empty", input: "   \n", prompt: "> ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				w.WriteString(tt.input)
				w.Close()
			}()
			reader := bufio.NewReader(r)
			got, err := promptLine(reader, tt.prompt)
			if (err != nil) != tt.wantErr {
				t.Fatalf("promptLine() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("promptLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPromptLineEOF(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	reader := bufio.NewReader(r)
	_, err = promptLine(reader, "> ")
	if err == nil {
		t.Fatal("expected error on EOF, got nil")
	}
}

func TestIsYes(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"y", true},
		{"Y", true},
		{"yes", true},
		{"YES", true},
		{"Yes", true},
		{"n", false},
		{"no", false},
		{"N", false},
		{"", false},
		{"  yes  ", true},
		{"maybe", false},
		{"1", false},
		{"nope", false},
		{" y ", true},
		{" YES ", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isYes(tt.input)
			if got != tt.want {
				t.Errorf("isYes(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestContainsString(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		target string
		want   bool
	}{
		{name: "found first", values: []string{"a", "b", "c"}, target: "a", want: true},
		{name: "found middle", values: []string{"a", "b", "c"}, target: "b", want: true},
		{name: "found last", values: []string{"a", "b", "c"}, target: "c", want: true},
		{name: "not found", values: []string{"a", "b", "c"}, target: "d", want: false},
		{name: "empty slice", values: []string{}, target: "a", want: false},
		{name: "empty target", values: []string{"a", "", "c"}, target: "", want: true},
		{name: "single match", values: []string{"only"}, target: "only", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsString(tt.values, tt.target)
			if got != tt.want {
				t.Errorf("containsString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRemoveString(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		target string
		want   []string
	}{
		{name: "remove first", values: []string{"a", "b", "c"}, target: "a", want: []string{"b", "c"}},
		{name: "remove last", values: []string{"a", "b", "c"}, target: "c", want: []string{"a", "b"}},
		{name: "remove middle", values: []string{"a", "b", "c"}, target: "b", want: []string{"a", "c"}},
		{name: "remove all occurrences", values: []string{"a", "b", "a", "a"}, target: "a", want: []string{"b"}},
		{name: "not found", values: []string{"a", "b", "c"}, target: "d", want: []string{"a", "b", "c"}},
		{name: "empty slice", values: []string{}, target: "a", want: []string{}},
		{name: "single element match", values: []string{"x"}, target: "x", want: []string{}},
		{name: "single element no match", values: []string{"x"}, target: "y", want: []string{"x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeString(tt.values, tt.target)
			if len(got) != len(tt.want) {
				t.Errorf("removeString() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("removeString()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseToolCallList(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty string", raw: "", want: nil},
		{name: "single tool", raw: "read_file", want: []string{"read_file"}},
		{name: "multiple tools", raw: "read_file,write_file,edit_file", want: []string{"read_file", "write_file", "edit_file"}},
		{name: "deduplicates", raw: "read_file,read_file,write_file", want: []string{"read_file", "write_file"}},
		{name: "trims whitespace", raw: " read_file , write_file ", want: []string{"read_file", "write_file"}},
		{name: "trailing comma", raw: "read_file,", want: []string{"read_file"}},
		{name: "leading comma", raw: ",read_file", want: []string{"read_file"}},
		{name: "only commas", raw: ",,,", want: nil},
		{name: "whitespace entries", raw: "  , read_file ,  , write_file , ", want: []string{"read_file", "write_file"}},
		{name: "case sensitive dedup", raw: "Read_File,read_file", want: []string{"Read_File", "read_file"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseToolCallList(tt.raw)
			if len(got) != len(tt.want) {
				t.Errorf("parseToolCallList(%q) = %v, want %v", tt.raw, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseToolCallList(%q)[%d] = %q, want %q", tt.raw, i, got[i], tt.want[i])
				}
			}
		})
	}
}
