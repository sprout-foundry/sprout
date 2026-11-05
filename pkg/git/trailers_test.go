package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatTrailerLine(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Assisted-by: Claude Code (anthropic/claude-opus-5-5)", "Assisted-by: Claude Code (anthropic/claude-opus-5-5)", true},
		{"Signed-off-by:   Dev   ", "Signed-off-by: Dev", true},
		{"key:value", "key: value", true},
		{"NO-COLON", "", false},
		{"bad key: value", "", false},
		{": value", "", false},
		{"Key:", "", false},
		{"Key: line1\nline2", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := FormatTrailerLine(tt.in)
		assert.Equal(t, tt.ok, ok, "FormatTrailerLine(%q) ok", tt.in)
		if ok {
			assert.Equal(t, tt.want, got, "FormatTrailerLine(%q)", tt.in)
		}
	}
}

func TestDedupTrailers(t *testing.T) {
	out := DedupTrailers([]string{
		"Assisted-by:  Sprout (dev/model)",
		"assisted-by: Sprout (dev/model)", // duplicate: key case-insensitive, value trimmed
		"Signed-off-by: Dev",
		"malformed entry",    // dropped
		"signed-off-by: Dev", // duplicate
		"Refs: #12",
	})
	assert.Equal(t, []string{
		"Assisted-by: Sprout (dev/model)",
		"Signed-off-by: Dev",
		"Refs: #12",
	}, out)
	assert.Empty(t, DedupTrailers(nil))
}

func TestAppendTrailers(t *testing.T) {
	tr := "Assisted-by: Sprout (ollama/llama3:8b)"
	sob := "Signed-off-by: Dev <dev@example.com>"

	tests := []struct {
		name string
		msg  string
		in   []string
		want string
	}{
		{
			name: "subject only starts a new block",
			msg:  "fix: x",
			in:   []string{tr},
			want: "fix: x\n\n" + tr + "\n",
		},
		{
			name: "body paragraph starts a new block",
			msg:  "fix: x\n\nBody text.\n",
			in:   []string{tr},
			want: "fix: x\n\nBody text.\n\n" + tr + "\n",
		},
		{
			name: "trailing newlines leave exactly one blank line",
			msg:  "fix: x\n\nBody text.\n\n\n",
			in:   []string{tr},
			want: "fix: x\n\nBody text.\n\n" + tr + "\n",
		},
		{
			name: "joins an existing trailer block without a blank line",
			msg:  "fix: x\n\nBody.\n\n" + sob + "\n",
			in:   []string{tr},
			want: "fix: x\n\nBody.\n\n" + sob + "\n" + tr + "\n",
		},
		{
			name: "identical trailer already present is not added",
			msg:  "fix: x\n\n" + tr + "\n",
			in:   []string{tr},
			want: "fix: x\n\n" + tr + "\n",
		},
		{
			name: "duplicate against existing ignores key case",
			msg:  "fix: x\n\n" + tr + "\n",
			in:   []string{"assisted-by: Sprout (ollama/llama3:8b)"},
			want: "fix: x\n\n" + tr + "\n",
		},
		{
			name: "duplicates among the new trailers collapse",
			msg:  "fix: x",
			in:   []string{tr, tr, "assisted-BY: Sprout (ollama/llama3:8b)"},
			want: "fix: x\n\n" + tr + "\n",
		},
		{
			name: "malformed entries are skipped",
			msg:  "fix: x",
			in:   []string{"not a trailer", tr},
			want: "fix: x\n\n" + tr + "\n",
		},
		{
			name: "all malformed leaves the message unchanged",
			msg:  "fix: x\n\nBody.",
			in:   []string{"nope"},
			want: "fix: x\n\nBody.",
		},
		{
			name: "empty message unchanged",
			msg:  "",
			in:   []string{tr},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AppendTrailers(tt.msg, tt.in...)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestAppendTrailers_GitParsesTrailers checks the output is a trailer block
// git itself recognises alongside existing trailers.
func TestAppendTrailers_GitParsesTrailers(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	msg := AppendTrailers(
		"fix: x\n\nBody.\n\nSigned-off-by: A <a@example.com>\n",
		"Assisted-by: Sprout (openai/gpt-5)", "Refs: #12")
	cmd := exec.Command("git", "interpret-trailers", "--parse") //nolint:gosec // G204: fixed git invocation
	cmd.Stdin = strings.NewReader(msg)
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t,
		"Signed-off-by: A <a@example.com>\nAssisted-by: Sprout (openai/gpt-5)\nRefs: #12\n",
		string(out))
}

func TestAgentsMdCommitTrailers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	content := `# Repo notes

## Commit Trailers

Trailer lines to append to every commit:
- Assisted-by: Sprout (ollama/llama3:8b)
* Refs: #12

## Other section

- Not-a-trailer: x
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	got := AgentsMdCommitTrailers(path)
	assert.Equal(t, []string{
		"Assisted-by: Sprout (ollama/llama3:8b)",
		"Refs: #12",
	}, got)

	// No section → nil; missing file → nil.
	require.NoError(t, os.WriteFile(path, []byte("# Repo notes\n\nNo trailers here.\n"), 0o644))
	assert.Nil(t, AgentsMdCommitTrailers(path))
	assert.Nil(t, AgentsMdCommitTrailers(filepath.Join(dir, "absent.md")))
}

// TestAgentsMdCommitTrailers_HeadingOnlySection pins that the trailer
// section must be a heading (a plain "Commit Trailers" paragraph is not one).
func TestAgentsMdCommitTrailers_HeadingOnlySection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	require.NoError(t, os.WriteFile(path, []byte("Commit Trailers\n- Refs: #1\n"), 0o644))
	assert.Nil(t, AgentsMdCommitTrailers(path))
}

func TestDefaultCommitTrailers(t *testing.T) {
	// No repo, no config → nil.
	assert.Nil(t, DefaultCommitTrailers(nil, t.TempDir()))

	// Config entries flow through, normalised and deduplicated.
	cfg := &configuration.Config{CommitTrailers: []string{"Assisted-by: Sprout (dev/m)", "assisted-by: Sprout (dev/m)"}}
	assert.Equal(t, []string{"Assisted-by: Sprout (dev/m)"}, DefaultCommitTrailers(cfg, t.TempDir()))
}

// TestDefaultCommitTrailers_AgentsMdFromSubdir checks the AGENTS.md is
// resolved at the repository top level even when the commit runs from a
// subdirectory (and from a worktree root).
func TestDefaultCommitTrailers_AgentsMdFromSubdir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := newAttributionTestRepo(t)
	sub := filepath.Join(root, "src")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"),
		[]byte("## Commit Trailers\n- Assisted-by: Sprout (ollama/qwen3:8b)\n"), 0o644))

	got := DefaultCommitTrailers(nil, sub)
	assert.Equal(t, []string{"Assisted-by: Sprout (ollama/qwen3:8b)"}, got)
}

// TestDefaultCommitTrailers_ConfigPlusAgentsMdUnion checks config and
// AGENTS.md entries union, with duplicates collapsing.
func TestDefaultCommitTrailers_ConfigPlusAgentsMdUnion(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := newAttributionTestRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"),
		[]byte("## Commit Trailers\n- Refs: #42\n"), 0o644))
	cfg := &configuration.Config{CommitTrailers: []string{"Assisted-by: Sprout (dev/m)", "Refs: #42"}}

	got := DefaultCommitTrailers(cfg, root)
	assert.Equal(t, []string{"Assisted-by: Sprout (dev/m)", "Refs: #42"}, got)
}
