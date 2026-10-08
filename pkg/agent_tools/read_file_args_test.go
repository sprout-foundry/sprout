package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// stubReadCallGuard is a test double for ReadCallGuard that counts calls by
// key, letting a test drive the repeat-guard thresholds directly.
type stubReadCallGuard struct {
	counts map[string]int
}

func newStubReadCallGuard() *stubReadCallGuard {
	return &stubReadCallGuard{counts: make(map[string]int)}
}

func (g *stubReadCallGuard) ObserveRead(path string, startLine, endLine int) int {
	key := fmt.Sprintf("%s|%d-%d", path, startLine, endLine)
	g.counts[key]++
	return g.counts[key]
}

// newTestEnvWithGuard returns a standard test env carrying the given
// identical-call guard.
func newTestEnvWithGuard(t *testing.T, workspaceRoot string, guard ReadCallGuard) ToolEnv {
	t.Helper()
	env := newTestEnv(t, workspaceRoot)
	env.ReadCallGuard = guard
	env.OutputWriter = io.Discard
	return env
}

// writeNumberedFile creates a file with n lines "line N\n" and returns its path.
func writeNumberedFile(t *testing.T, dir, name string, n int) string {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
	return path
}

// ---------------------------------------------------------------------------
// Unknown-argument rejection
// ---------------------------------------------------------------------------

func TestReadFileHandler_UnknownArgumentRejected(t *testing.T) {
	t.Parallel()
	h := &readFileHandler{}

	err := h.Validate(map[string]any{"path": "x.txt", "start": 1})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown argument")
	require.Contains(t, err.Error(), "start")
	// The message must name the valid arguments so the model can correct.
	require.Contains(t, err.Error(), "'path'")
	require.Contains(t, err.Error(), "'view_range'")
	// ... and the accepted aliases.
	require.Contains(t, err.Error(), "start_line")
	require.Contains(t, err.Error(), "offset")
}

func TestReadFileHandler_UnknownArgumentRejectedOnExecute(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)
	path := writeNumberedFile(t, dir, "f.txt", 5)

	// Execute must reject too — the live dispatch path (seed registry) does
	// not call Validate, so the guard cannot live only there.
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": path, "bogus": true})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "unknown argument")
	require.Contains(t, res.Output, "view_range")
}

func TestReadFileHandler_KnownAliasesNotRejected(t *testing.T) {
	t.Parallel()
	h := &readFileHandler{}
	for _, args := range []map[string]any{
		{"path": "x.txt", "view_range": []any{1, 2}},
		{"path": "x.txt", "file_path": "y.txt"},
		{"path": "x.txt", "start_line": 1, "end_line": 2},
		{"path": "x.txt", "line_start": 1, "line_end": 2},
		{"path": "x.txt", "offset": 1, "limit": 2},
	} {
		require.NoError(t, h.Validate(args), "args %v should validate", args)
	}
}

func TestReadFileHandler_CaseInsensitiveAliasesAccepted(t *testing.T) {
	t.Parallel()
	h := &readFileHandler{}
	require.NoError(t, h.Validate(map[string]any{"path": "x.txt", "Start_Line": 1, "END_LINE": 2}))
}

// ---------------------------------------------------------------------------
// Alias mapping equivalence
// ---------------------------------------------------------------------------

func TestReadFileHandler_AliasesReturnSameLinesAsViewRange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)
	path := writeNumberedFile(t, dir, "f.txt", 20)

	canonical, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":       path,
		"view_range": []any{float64(5), float64(8)},
	})
	require.NoError(t, err)
	require.Contains(t, canonical.Output, "line 5")
	require.Contains(t, canonical.Output, "line 8")
	require.NotContains(t, canonical.Output, "line 9")

	cases := map[string]map[string]any{
		"start_end":  {"path": path, "start_line": 5, "end_line": 8},
		"line_start": {"path": path, "line_start": 5, "line_end": 8},
		"offset_lim": {"path": path, "offset": 5, "limit": 4}, // offset 1-based, limit is a count
	}
	for name, args := range cases {
		run := func(t *testing.T) {
			res, err := h.Execute(ctx, newTestEnv(t, dir), args)
			require.NoError(t, err)
			require.Equal(t, canonical.Output, res.Output, "%s should match view_range result", name)
		}
		t.Run(name, run)
	}
}

func TestReadFileHandler_ExplicitViewRangeWinsOverAliases(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)
	path := writeNumberedFile(t, dir, "f.txt", 20)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":       path,
		"view_range": []any{float64(1), float64(2)},
		"start_line": 10,
		"end_line":   12,
	})
	require.NoError(t, err)
	require.Contains(t, res.Output, "line 1")
	require.Contains(t, res.Output, "line 2")
	require.NotContains(t, res.Output, "line 10")
}

func TestReadFileHandler_OffsetLimitInterpretedAsStartAndCount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)
	path := writeNumberedFile(t, dir, "f.txt", 30)

	// offset=20, limit=5 → lines 20-24 (1-based start, count of 5).
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path": path, "offset": 20, "limit": 5,
	})
	require.NoError(t, err)
	require.Contains(t, res.Output, "Lines 20-24")
	require.Contains(t, res.Output, "line 20")
	require.Contains(t, res.Output, "line 24")
	require.NotContains(t, res.Output, "line 25")
}

func TestReadFileHandler_FilePathAliasForPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)
	path := writeNumberedFile(t, dir, "f.txt", 5)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"file_path": path})
	require.NoError(t, err)
	require.Contains(t, res.Output, "line 1")
}

func TestReadFileHandler_StartLineOnlyReadsToEnd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)
	path := writeNumberedFile(t, dir, "f.txt", 10)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": path, "start_line": 8})
	require.NoError(t, err)
	require.Contains(t, res.Output, "line 8")
	require.Contains(t, res.Output, "line 10")
	require.NotContains(t, res.Output, "line 7")
}

func TestReadFileHandler_EndLineOnlyReadsFromStart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)
	path := writeNumberedFile(t, dir, "f.txt", 10)

	// A lone end alias is still a bounded read (lines 1-3), not the whole file.
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": path, "end_line": 3})
	require.NoError(t, err)
	require.Contains(t, res.Output, "Lines 1-3")
	require.Contains(t, res.Output, "line 3")
	require.NotContains(t, res.Output, "line 4")
}

// ---------------------------------------------------------------------------
// Repeat guard
// ---------------------------------------------------------------------------

func TestReadFileHandler_RepeatGuardThirdIdenticalCallReturnsNote(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)
	path := writeNumberedFile(t, dir, "f.txt", 50)
	guard := newStubReadCallGuard()

	args := map[string]any{"path": path, "view_range": []any{float64(10), float64(20)}}

	r1, err := h.Execute(ctx, newTestEnvWithGuard(t, dir, guard), args)
	require.NoError(t, err)
	require.Contains(t, r1.Output, "line 10")

	r2, err := h.Execute(ctx, newTestEnvWithGuard(t, dir, guard), args)
	require.NoError(t, err)
	require.Contains(t, r2.Output, "line 10")

	r3, err := h.Execute(ctx, newTestEnvWithGuard(t, dir, guard), args)
	require.NoError(t, err)
	require.Contains(t, r3.Output, "identical to your last read")
	require.Contains(t, r3.Output, "view_range [10, 20]")
	require.NotContains(t, r3.Output, "line 10")
	require.False(t, r3.IsError, "the note is a normal result, not an error")
}

func TestReadFileHandler_RepeatGuardDifferentRangeStillReturnsContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)
	path := writeNumberedFile(t, dir, "f.txt", 50)
	guard := newStubReadCallGuard()

	// Third call of range A returns the note.
	for i := 0; i < 3; i++ {
		_, err := h.Execute(ctx, newTestEnvWithGuard(t, dir, guard), map[string]any{"path": path, "view_range": []any{float64(10), float64(20)}})
		require.NoError(t, err)
	}
	// A different range is a different key — content, not a note.
	res, err := h.Execute(ctx, newTestEnvWithGuard(t, dir, guard), map[string]any{"path": path, "view_range": []any{float64(30), float64(40)}})
	require.NoError(t, err)
	require.Contains(t, res.Output, "line 30")
	require.NotContains(t, res.Output, "identical to your last read")
}

func TestReadFileHandler_RepeatGuardDifferentPathIsIndependent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)
	p1 := writeNumberedFile(t, dir, "a.txt", 10)
	p2 := writeNumberedFile(t, dir, "b.txt", 10)
	guard := newStubReadCallGuard()

	for i := 0; i < 3; i++ {
		_, err := h.Execute(ctx, newTestEnvWithGuard(t, dir, guard), map[string]any{"path": p1})
		require.NoError(t, err)
	}
	res, err := h.Execute(ctx, newTestEnvWithGuard(t, dir, guard), map[string]any{"path": p2})
	require.NoError(t, err)
	require.Contains(t, res.Output, "line 1")
	require.NotContains(t, res.Output, "identical to your last read")
}

func TestReadFileHandler_RepeatGuardNoteForFullFileRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)
	path := writeNumberedFile(t, dir, "f.txt", 5)
	guard := newStubReadCallGuard()

	for i := 0; i < 3; i++ {
		res, err := h.Execute(ctx, newTestEnvWithGuard(t, dir, guard), map[string]any{"path": path})
		require.NoError(t, err)
		if i < 2 {
			require.Contains(t, res.Output, "line 1")
		} else {
			require.Contains(t, res.Output, "identical to your last read")
			require.Contains(t, res.Output, "view_range")
		}
	}
}

func TestReadFileHandler_NoGuardEveryCallReturnsContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)
	path := writeNumberedFile(t, dir, "f.txt", 5)

	for i := 0; i < 5; i++ {
		res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": path})
		require.NoError(t, err)
		require.Contains(t, res.Output, "line 1", "call %d should return content", i+1)
	}
}

// ---------------------------------------------------------------------------
// Truncation notice
// ---------------------------------------------------------------------------

func TestReadFile_HeadTailNoticeStatesTotalLinesAndRange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// ~2000 lines at ~150 bytes each is well over the 32KB default cap but
	// under the 2MB line-range cap.
	path := filepath.Join(dir, "big.txt")
	var b strings.Builder
	for i := 1; i <= 2000; i++ {
		fmt.Fprintf(&b, "Line %d: %s\n", i, strings.Repeat("x", 130))
	}
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))

	ctx := filesystem.WithWorkspaceRoot(context.Background(), dir)
	result, err := ReadFile(ctx, path)
	require.NoError(t, err)
	require.Contains(t, result, "[WARN]")
	// The file has 2000 newline-terminated lines; reader line indexing
	// (strings.Split) yields 2001 elements.
	require.Contains(t, result, "total 2001 lines")
	require.Regexp(t, `view_range=\[\d+, \d+\]`, result)
	require.Contains(t, result, "for lines")
}

func TestReadFile_HeadTailNoticeSuggestedRangeCoversExactOmittedMiddle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// > 32KB default cap but < 2MB line-range cap, so the head+tail path runs
	// and the subsequent ranged read reads the whole file.
	path := filepath.Join(dir, "big.txt")
	var b strings.Builder
	for i := 1; i <= 2000; i++ {
		fmt.Fprintf(&b, "Line %d: %s\n", i, strings.Repeat("x", 130))
	}
	content := b.String()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	ctx := filesystem.WithWorkspaceRoot(context.Background(), dir)
	result, err := ReadFileWithRange(ctx, path, 0, 0)
	require.NoError(t, err)
	require.Contains(t, result, "[WARN]")

	// The notice reports the head/tail line counts; derive the expected
	// omitted-middle bounds from them plus the file's true line count L.
	headLines := mustMatchInt(t, result, `first (\d+) lines`)
	tailLines := mustMatchInt(t, result, `last (\d+) lines`)
	L := len(strings.Split(content, "\n"))
	wantFirst := headLines + 1
	wantLast := L - tailLines
	require.Less(t, wantFirst, wantLast, "file must have a non-empty omitted middle")

	m := regexp.MustCompile(`view_range=\[(\d+), (\d+)\]`).FindStringSubmatch(result)
	require.Len(t, m, 3, "notice should suggest a view_range, got: %s", result)
	gotFirst, err := strconv.Atoi(m[1])
	require.NoError(t, err)
	gotLast, err := strconv.Atoi(m[2])
	require.NoError(t, err)

	require.Equal(t, wantFirst, gotFirst, "suggested range must start at the first omitted line")
	require.Equal(t, wantLast, gotLast, "suggested range must end at the last omitted line")

	// The suggested range must actually read the omitted middle: non-empty and
	// its first line carries the expected content.
	ranged, err := ReadFileWithRange(ctx, path, gotFirst, gotLast)
	require.NoError(t, err)
	require.NotEmpty(t, ranged)
	require.Contains(t, ranged, fmt.Sprintf("Lines %d-%d", gotFirst, gotLast))
	require.Contains(t, ranged, fmt.Sprintf("Line %d:", gotFirst))
	require.Contains(t, ranged, fmt.Sprintf("Line %d:", gotLast))
	require.NotContains(t, ranged, fmt.Sprintf("Line %d:", gotLast+1))
	require.NotContains(t, ranged, fmt.Sprintf("Line %d:", gotFirst-1))
}

// mustMatchInt extracts the first capture group of pattern from s as an int.
func mustMatchInt(t *testing.T, s, pattern string) int {
	t.Helper()
	m := regexp.MustCompile(pattern).FindStringSubmatch(s)
	require.Len(t, m, 2, "pattern %q not found in: %s", pattern, s)
	n, err := strconv.Atoi(m[1])
	require.NoError(t, err)
	return n
}

func TestReadFile_LineRangeCapNoticeHonestAboutUnknownTotal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// > 2MB so a line-range read hits the read cap. Each line is ~150 bytes,
	// so ~20k lines ≈ 3MB.
	path := filepath.Join(dir, "huge.txt")
	var b strings.Builder
	for i := 1; i <= 20000; i++ {
		fmt.Fprintf(&b, "Line %d: %s\n", i, strings.Repeat("x", 130))
	}
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))

	ctx := filesystem.WithWorkspaceRoot(context.Background(), dir)
	result, err := ReadFileWithRange(ctx, path, 1000, 1100)
	require.NoError(t, err)
	require.Contains(t, result, "[WARN]")
	require.Contains(t, result, "exceeds the 2MB line-range read cap")
	require.Contains(t, result, "total line count is unavailable")
	require.Contains(t, result, "readable content ends at line")
	require.Regexp(t, `view_range=\[\d+, \d+\]`, result)
}

func TestCountFileLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()

	require.Equal(t, 0, countFileLines(ctx, filepath.Join(dir, "missing.txt")))

	empty := filepath.Join(dir, "empty.txt")
	require.NoError(t, os.WriteFile(empty, nil, 0o644))
	require.Equal(t, 0, countFileLines(ctx, empty))

	// Semantics match strings.Split(content, "\n"): a trailing newline yields
	// an extra (empty) final element.
	withTrailing := filepath.Join(dir, "trailing.txt")
	require.NoError(t, os.WriteFile(withTrailing, []byte("a\nb\nc\n"), 0o644))
	require.Equal(t, 4, countFileLines(ctx, withTrailing))

	noTrailing := filepath.Join(dir, "no_trailing.txt")
	require.NoError(t, os.WriteFile(noTrailing, []byte("a\nb\nc"), 0o644))
	require.Equal(t, 3, countFileLines(ctx, noTrailing))

	// Cross-check against the reader's own indexing for a multi-chunk file
	// (forces several 64KB reads).
	big := filepath.Join(dir, "big.txt")
	var b strings.Builder
	for i := 1; i <= 20000; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	content := b.String()
	require.NoError(t, os.WriteFile(big, []byte(content), 0o644))
	require.Equal(t, len(strings.Split(content, "\n")), countFileLines(ctx, big))
}

func TestReadFileRepeatNoteText(t *testing.T) {
	t.Parallel()
	withRange := readRepeatNote("f.txt", 10, 20, 3)
	require.Contains(t, withRange, "identical to your last read")
	require.Contains(t, withRange, "view_range [10, 20]")
	require.Contains(t, withRange, "lines 10-20")

	noRange := readRepeatNote("f.txt", 0, 0, 4)
	require.Contains(t, noRange, "identical to your last read")
	require.Contains(t, noRange, "view_range")
}
