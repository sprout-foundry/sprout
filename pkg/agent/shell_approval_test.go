package agent

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// SplitShellIntoParts
// ---------------------------------------------------------------------------

func TestSplitShellIntoParts_Single(t *testing.T) {
	parts := SplitShellIntoParts("ls -la")
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Text != "ls -la" {
		t.Errorf("expected text 'ls -la', got %q", parts[0].Text)
	}
}

func TestSplitShellIntoParts_AndAnd(t *testing.T) {
	parts := SplitShellIntoParts("rm -rf foo && git status")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Text != "rm -rf foo" {
		t.Errorf("part 0: expected 'rm -rf foo', got %q", parts[0].Text)
	}
	if parts[1].Text != "git status" {
		t.Errorf("part 1: expected 'git status', got %q", parts[1].Text)
	}
}

func TestSplitShellIntoParts_OrOr(t *testing.T) {
	parts := SplitShellIntoParts("false || echo done")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Text != "false" {
		t.Errorf("part 0: expected 'false', got %q", parts[0].Text)
	}
	if parts[1].Text != "echo done" {
		t.Errorf("part 1: expected 'echo done', got %q", parts[1].Text)
	}
}

func TestSplitShellIntoParts_Semicolon(t *testing.T) {
	parts := SplitShellIntoParts("echo a; echo b")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Text != "echo a" {
		t.Errorf("part 0: expected 'echo a', got %q", parts[0].Text)
	}
	if parts[1].Text != "echo b" {
		t.Errorf("part 1: expected 'echo b', got %q", parts[1].Text)
	}
}

func TestSplitShellIntoParts_Pipe(t *testing.T) {
	parts := SplitShellIntoParts("cat foo | grep bar")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Text != "cat foo" {
		t.Errorf("part 0: expected 'cat foo', got %q", parts[0].Text)
	}
	if parts[1].Text != "grep bar" {
		t.Errorf("part 1: expected 'grep bar', got %q", parts[1].Text)
	}
}

func TestSplitShellIntoParts_NestedParens(t *testing.T) {
	parts := SplitShellIntoParts("((rm -rf foo))")
	if len(parts) != 1 {
		t.Fatalf("expected 1 part (parens wrap the command), got %d", len(parts))
	}
	if parts[0].Text != "((rm -rf foo))" {
		t.Errorf("expected '((rm -rf foo))', got %q", parts[0].Text)
	}
}

func TestSplitShellIntoParts_QuotedDoubleQuote(t *testing.T) {
	parts := SplitShellIntoParts(`echo "rm && fake"`)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part (&& inside quotes is literal), got %d", len(parts))
	}
	if parts[0].Text != `echo "rm && fake"` {
		t.Errorf("unexpected text: %q", parts[0].Text)
	}
}

func TestSplitShellIntoParts_QuotedSingleQuote(t *testing.T) {
	parts := SplitShellIntoParts("echo 'a; b'")
	if len(parts) != 1 {
		t.Fatalf("expected 1 part (; inside quotes is literal), got %d", len(parts))
	}
	if parts[0].Text != "echo 'a; b'" {
		t.Errorf("unexpected text: %q", parts[0].Text)
	}
}

func TestSplitShellIntoParts_Empty(t *testing.T) {
	parts := SplitShellIntoParts("")
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts for empty input, got %d", len(parts))
	}
}

func TestSplitShellIntoParts_TrimsWhitespace(t *testing.T) {
	parts := SplitShellIntoParts("  ls -la  &&   echo done  ")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	for i, part := range parts {
		if part.Text != strings.TrimSpace(part.Text) {
			t.Errorf("part %d text is not trimmed: %q", i, part.Text)
		}
	}
}

func TestSplitShellIntoParts_PipeInsideParens(t *testing.T) {
	// Pipe inside parens should NOT split.
	parts := SplitShellIntoParts("(cat foo | grep bar)")
	if len(parts) != 1 {
		t.Fatalf("expected 1 part (pipe inside parens), got %d", len(parts))
	}
	if parts[0].Text != "(cat foo | grep bar)" {
		t.Errorf("expected '(cat foo | grep bar)', got %q", parts[0].Text)
	}
}

func TestSplitShellIntoParts_ConsecutiveSeparators(t *testing.T) {
	// Consecutive separators with no content should be skipped.
	parts := SplitShellIntoParts("ls &&&& echo")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d: %v", len(parts), parts)
	}
}

func TestSplitShellIntoParts_SequentialIDs(t *testing.T) {
	// Input with empty parts in between should produce sequential IDs (no gaps).
	parts := SplitShellIntoParts("ls &&&& echo")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d: %v", len(parts), parts)
	}
	if parts[0].ID != "part-0" {
		t.Errorf("part 0 ID: expected 'part-0', got %q", parts[0].ID)
	}
	if parts[1].ID != "part-1" {
		t.Errorf("part 1 ID: expected 'part-1', got %q", parts[1].ID)
	}
}

func TestSplitShellIntoParts_ThreeParts(t *testing.T) {
	parts := SplitShellIntoParts("echo a && echo b; echo c")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	if parts[0].Text != "echo a" {
		t.Errorf("part 0: expected 'echo a', got %q", parts[0].Text)
	}
	if parts[1].Text != "echo b" {
		t.Errorf("part 1: expected 'echo b', got %q", parts[1].Text)
	}
	if parts[2].Text != "echo c" {
		t.Errorf("part 2: expected 'echo c', got %q", parts[2].Text)
	}
}

// ---------------------------------------------------------------------------
// ClassifyShellSegment
// ---------------------------------------------------------------------------

func TestClassifyShellSegment_rm_rf(t *testing.T) {
	kind := ClassifyShellSegment("rm -rf foo")
	if kind != CommandKindRm {
		t.Errorf("expected %s, got %s", CommandKindRm, kind)
	}
}

func TestClassifyShellSegment_rm_no_rf(t *testing.T) {
	// CRITICAL: bare rm without flags must NOT match.
	kind := ClassifyShellSegment("rm foo")
	if kind != CommandKindUnknown {
		t.Errorf("expected %s for 'rm foo', got %s", CommandKindUnknown, kind)
	}
}

func TestClassifyShellSegment_rm_with_R(t *testing.T) {
	kind := ClassifyShellSegment("rm -R foo")
	if kind != CommandKindRm {
		t.Errorf("expected %s, got %s", CommandKindRm, kind)
	}
}

func TestClassifyShellSegment_rm_with_f_only(t *testing.T) {
	// -f alone (no r/R) should match because the regex checks for [rRfF].
	kind := ClassifyShellSegment("rm -f foo")
	if kind != CommandKindRm {
		t.Errorf("expected %s, got %s", CommandKindRm, kind)
	}
}

func TestClassifyShellSegment_rm_multiple_flags(t *testing.T) {
	// Multiple separate flag groups should still match.
	kind := ClassifyShellSegment("rm -r -f /tmp/foo")
	if kind != CommandKindRm {
		t.Errorf("expected %s, got %s", CommandKindRm, kind)
	}
}

func TestClassifyShellSegment_git_push_force(t *testing.T) {
	kind := ClassifyShellSegment("git push --force origin main")
	if kind != CommandKindGitPush {
		t.Errorf("expected %s, got %s", CommandKindGitPush, kind)
	}
}

func TestClassifyShellSegment_git_push_f(t *testing.T) {
	kind := ClassifyShellSegment("git push -f origin main")
	if kind != CommandKindGitPush {
		t.Errorf("expected %s, got %s", CommandKindGitPush, kind)
	}
}

func TestClassifyShellSegment_git_push_no_force(t *testing.T) {
	kind := ClassifyShellSegment("git push origin main")
	if kind != CommandKindUnknown {
		t.Errorf("expected %s for normal push, got %s", CommandKindUnknown, kind)
	}
}

func TestClassifyShellSegment_git_reset_hard(t *testing.T) {
	kind := ClassifyShellSegment("git reset --hard HEAD~5")
	if kind != CommandKindGitReset {
		t.Errorf("expected %s, got %s", CommandKindGitReset, kind)
	}
}

func TestClassifyShellSegment_git_reset_soft(t *testing.T) {
	kind := ClassifyShellSegment("git reset --soft HEAD~5")
	if kind != CommandKindUnknown {
		t.Errorf("expected %s for soft reset, got %s", CommandKindUnknown, kind)
	}
}

func TestClassifyShellSegment_kubectl_delete(t *testing.T) {
	kind := ClassifyShellSegment("kubectl delete pod foo")
	if kind != CommandKindKubectl {
		t.Errorf("expected %s, got %s", CommandKindKubectl, kind)
	}
}

func TestClassifyShellSegment_kubectl_get(t *testing.T) {
	kind := ClassifyShellSegment("kubectl get pods")
	if kind != CommandKindUnknown {
		t.Errorf("expected %s for kubectl get, got %s", CommandKindUnknown, kind)
	}
}

func TestClassifyShellSegment_docker_rm(t *testing.T) {
	kind := ClassifyShellSegment("docker rm $(docker ps -aq)")
	if kind != CommandKindDocker {
		t.Errorf("expected %s, got %s", CommandKindDocker, kind)
	}
}

func TestClassifyShellSegment_docker_system_prune(t *testing.T) {
	kind := ClassifyShellSegment("docker system prune -af")
	if kind != CommandKindDocker {
		t.Errorf("expected %s, got %s", CommandKindDocker, kind)
	}
}

func TestClassifyShellSegment_docker_ps(t *testing.T) {
	kind := ClassifyShellSegment("docker ps")
	if kind != CommandKindUnknown {
		t.Errorf("expected %s for docker ps, got %s", CommandKindUnknown, kind)
	}
}

func TestClassifyShellSegment_chmod_777(t *testing.T) {
	kind := ClassifyShellSegment("chmod 777 /etc/passwd")
	if kind != CommandKindChmod {
		t.Errorf("expected %s, got %s", CommandKindChmod, kind)
	}
}

func TestClassifyShellSegment_chmod_644(t *testing.T) {
	kind := ClassifyShellSegment("chmod 644 file.txt")
	if kind != CommandKindChmod {
		t.Errorf("expected %s, got %s", CommandKindChmod, kind)
	}
}

func TestClassifyShellSegment_chmod_R_755(t *testing.T) {
	kind := ClassifyShellSegment("chmod -R 755 /opt/app")
	if kind != CommandKindChmod {
		t.Errorf("expected %s, got %s", CommandKindChmod, kind)
	}
}

func TestClassifyShellSegment_chown_root(t *testing.T) {
	kind := ClassifyShellSegment("chown root:root /etc/foo")
	if kind != CommandKindChown {
		t.Errorf("expected %s, got %s", CommandKindChown, kind)
	}
}

func TestClassifyShellSegment_chown_non_root(t *testing.T) {
	kind := ClassifyShellSegment("chown www-data:www-data /var/www")
	if kind != CommandKindUnknown {
		t.Errorf("expected %s for non-root chown, got %s", CommandKindUnknown, kind)
	}
}

func TestClassifyShellSegment_chown_path_with_root(t *testing.T) {
	// "root" in the path should NOT trigger chown classification.
	kind := ClassifyShellSegment("chown www-data /var/root")
	if kind != CommandKindUnknown {
		t.Errorf("expected %s for chown with root in path, got %s", CommandKindUnknown, kind)
	}
}

func TestClassifyShellSegment_chown_root_group(t *testing.T) {
	// "alice:root" — group is root, should match.
	kind := ClassifyShellSegment("chown alice:root /etc/foo")
	if kind != CommandKindChown {
		t.Errorf("expected %s, got %s", CommandKindChown, kind)
	}
}

func TestClassifyShellSegment_write_redirect(t *testing.T) {
	kind := ClassifyShellSegment("echo hi > /etc/foo")
	if kind != CommandKindWriteRedirect {
		t.Errorf("expected %s, got %s", CommandKindWriteRedirect, kind)
	}
}

func TestClassifyShellSegment_append_redirect_not_write(t *testing.T) {
	// >> is append, not destructive write.
	kind := ClassifyShellSegment("echo hi >> /tmp/log")
	if kind != CommandKindUnknown {
		t.Errorf("expected %s for append redirect, got %s", CommandKindUnknown, kind)
	}
}

func TestClassifyShellSegment_curl_post(t *testing.T) {
	kind := ClassifyShellSegment("curl -X POST https://example.com")
	if kind != CommandKindHttpPost {
		t.Errorf("expected %s, got %s", CommandKindHttpPost, kind)
	}
}

func TestClassifyShellSegment_wget_post(t *testing.T) {
	kind := ClassifyShellSegment("wget --post-data='x' https://example.com")
	if kind != CommandKindHttpPost {
		t.Errorf("expected %s, got %s", CommandKindHttpPost, kind)
	}
}

func TestClassifyShellSegment_ls(t *testing.T) {
	kind := ClassifyShellSegment("ls -la")
	if kind != CommandKindUnknown {
		t.Errorf("expected %s for ls, got %s", CommandKindUnknown, kind)
	}
}

// ---------------------------------------------------------------------------
// ClassifyShellSegmentWithSemantic
// ---------------------------------------------------------------------------

func TestClassifyShellSegmentWithSemantic(t *testing.T) {
	tests := []struct {
		name       string
		segment    string
		wantKind   CommandKind
		wantSubstr string
	}{
		{"rm -rf", "rm -rf /tmp/foo", CommandKindRm, "Recursively delete"},
		{"git push --force", "git push --force", CommandKindGitPush, "Force-push"},
		{"git reset --hard", "git reset --hard", CommandKindGitReset, "Hard reset"},
		{"kubectl delete", "kubectl delete pod foo", CommandKindKubectl, "Delete Kubernetes"},
		{"docker rm", "docker rm abc", CommandKindDocker, "Docker"},
		{"chmod 777", "chmod 777 file", CommandKindChmod, "permissions"},
		{"chown root", "chown root:root /etc", CommandKindChown, "ownership to root"},
		{"write redirect", "echo x > file", CommandKindWriteRedirect, "overwrite"},
		{"curl POST", "curl -X POST https://x.com", CommandKindHttpPost, "POST"},
		{"unknown", "ls -la", CommandKindUnknown, "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, semantic := ClassifyShellSegmentWithSemantic(tt.segment)
			if kind != tt.wantKind {
				t.Errorf("kind: expected %s, got %s", tt.wantKind, kind)
			}
			if !strings.Contains(semantic, tt.wantSubstr) {
				t.Errorf("semantic %q should contain %q", semantic, tt.wantSubstr)
			}
		})
	}
}

func TestClassifyShellSegmentWithSemantic_rm_multiple_flags(t *testing.T) {
	// "rm -r -f /tmp/foo" should extract target "/tmp/foo" without leftover flags.
	kind, semantic := ClassifyShellSegmentWithSemantic("rm -r -f /tmp/foo")
	if kind != CommandKindRm {
		t.Errorf("kind: expected %s, got %s", CommandKindRm, kind)
	}
	if !strings.Contains(semantic, "/tmp/foo") {
		t.Errorf("semantic %q should contain '/tmp/foo'", semantic)
	}
	if strings.Contains(semantic, "-f") {
		t.Errorf("semantic %q should NOT contain '-f' (leftover flag)", semantic)
	}
}

// ---------------------------------------------------------------------------
// NewShellProposal
// ---------------------------------------------------------------------------

func TestNewShellProposal_RmChain(t *testing.T) {
	p := NewShellProposal("rm -rf foo && git push --force")
	if len(p.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(p.Parts))
	}
	if p.Parts[0].Kind != CommandKindRm {
		t.Errorf("part 0 kind: expected %s, got %s", CommandKindRm, p.Parts[0].Kind)
	}
	if p.Parts[1].Kind != CommandKindGitPush {
		t.Errorf("part 1 kind: expected %s, got %s", CommandKindGitPush, p.Parts[1].Kind)
	}
	// Critical (rm) > High (git push), so overall is Critical.
	if p.RiskLevel != configuration.RiskLevelCritical {
		t.Errorf("risk level: expected Critical, got %s", p.RiskLevel)
	}
}

func TestNewShellProposal_AllSafe(t *testing.T) {
	p := NewShellProposal("ls && echo done")
	if len(p.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(p.Parts))
	}
	if p.RiskLevel != configuration.RiskLevelLow {
		t.Errorf("risk level: expected Low, got %s", p.RiskLevel)
	}
}

func TestNewShellProposal_Empty(t *testing.T) {
	p := NewShellProposal("")
	if len(p.Parts) != 0 {
		t.Fatalf("expected 0 parts, got %d", len(p.Parts))
	}
	if p.RiskLevel != configuration.RiskLevelLow {
		t.Errorf("risk level: expected Low, got %s", p.RiskLevel)
	}
}

func TestNewShellProposal_SingleChmod(t *testing.T) {
	p := NewShellProposal("chmod 777 /etc/passwd")
	if len(p.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(p.Parts))
	}
	if p.Parts[0].Kind != CommandKindChmod {
		t.Errorf("kind: expected %s, got %s", CommandKindChmod, p.Parts[0].Kind)
	}
	if p.RiskLevel != configuration.RiskLevelMedium {
		t.Errorf("risk level: expected Medium, got %s", p.RiskLevel)
	}
}

// ---------------------------------------------------------------------------
// MostDestructivePart
// ---------------------------------------------------------------------------

func TestMostDestructivePart_MixedChain(t *testing.T) {
	p := NewShellProposal("ls && rm -rf / && echo done")
	part := p.MostDestructivePart()
	if part == nil {
		t.Fatal("expected non-nil part")
	}
	if part.Kind != CommandKindRm {
		t.Errorf("expected %s part, got %s", CommandKindRm, part.Kind)
	}
	if part.Text != "rm -rf /" {
		t.Errorf("expected text 'rm -rf /', got %q", part.Text)
	}
}

func TestMostDestructivePart_Empty(t *testing.T) {
	p := NewShellProposal("")
	part := p.MostDestructivePart()
	if part != nil {
		t.Errorf("expected nil for empty proposal, got %v", part)
	}
}

func TestMostDestructivePart_AllSame(t *testing.T) {
	p := NewShellProposal("ls && echo hi")
	part := p.MostDestructivePart()
	if part == nil {
		t.Fatal("expected non-nil part")
	}
	if part.Text != "ls" {
		t.Errorf("expected first part 'ls' (tie-break), got %q", part.Text)
	}
}

// ---------------------------------------------------------------------------
// HighRiskParts
// ---------------------------------------------------------------------------

func TestHighRiskParts_FromMixedChain(t *testing.T) {
	p := NewShellProposal("ls && rm -rf / && echo done")
	high := p.HighRiskParts()
	if len(high) != 1 {
		t.Fatalf("expected 1 high-risk part, got %d", len(high))
	}
	if high[0].Kind != CommandKindRm {
		t.Errorf("expected %s, got %s", CommandKindRm, high[0].Kind)
	}
}

func TestHighRiskParts_NoRisky(t *testing.T) {
	p := NewShellProposal("ls && echo")
	high := p.HighRiskParts()
	if len(high) != 0 {
		t.Errorf("expected 0 high-risk parts, got %d", len(high))
	}
}

func TestHighRiskParts_MultipleHigh(t *testing.T) {
	p := NewShellProposal("rm -rf / && docker rm all && git push --force")
	high := p.HighRiskParts()
	if len(high) != 3 {
		t.Fatalf("expected 3 high-risk parts, got %d", len(high))
	}
	if high[0].Kind != CommandKindRm {
		t.Errorf("part 0: expected %s, got %s", CommandKindRm, high[0].Kind)
	}
	if high[1].Kind != CommandKindDocker {
		t.Errorf("part 1: expected %s, got %s", CommandKindDocker, high[1].Kind)
	}
	if high[2].Kind != CommandKindGitPush {
		t.Errorf("part 2: expected %s, got %s", CommandKindGitPush, high[2].Kind)
	}
}

// ---------------------------------------------------------------------------
// Integration: IDs are sequential
// ---------------------------------------------------------------------------

func TestShellProposal_PartIDs(t *testing.T) {
	p := NewShellProposal("ls && echo a; echo b")
	if len(p.Parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(p.Parts))
	}
	if p.Parts[0].ID != "part-0" {
		t.Errorf("part 0 ID: expected 'part-0', got %q", p.Parts[0].ID)
	}
	if p.Parts[1].ID != "part-1" {
		t.Errorf("part 1 ID: expected 'part-1', got %q", p.Parts[1].ID)
	}
	if p.Parts[2].ID != "part-2" {
		t.Errorf("part 2 ID: expected 'part-2', got %q", p.Parts[2].ID)
	}
}
