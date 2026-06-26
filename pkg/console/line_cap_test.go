package console

import (
	"strings"
	"testing"
)

// captureSink returns a sink closure plus a pointer to the captured
// output buffer, for assertion in tests.
func captureSink() (func(string), *strings.Builder) {
	var b strings.Builder
	return func(s string) { b.WriteString(s) }, &b
}

func TestLineCapWriter_PassThroughBelowLimit(t *testing.T) {
	sink, buf := captureSink()
	w := NewLineCapWriter(50, sink)
	w.Write("short line\n")
	if got := buf.String(); got != "short line\n" {
		t.Fatalf("expected verbatim pass-through, got %q", got)
	}
}

func TestLineCapWriter_DisabledWhenLimitNonPositive(t *testing.T) {
	sink, buf := captureSink()
	w := NewLineCapWriter(0, sink)
	long := strings.Repeat("x", 1000)
	w.Write(long + "\n")
	if got := buf.String(); got != long+"\n" {
		t.Fatalf("limit=0 should pass through; got %d chars", len(got))
	}
}

func TestLineCapWriter_TruncatesSingleLongLine(t *testing.T) {
	sink, buf := captureSink()
	w := NewLineCapWriter(10, sink)
	w.Write(strings.Repeat("a", 30) + "\n")
	got := buf.String()
	if !strings.HasPrefix(got, "aaaaaaaaaa") {
		t.Fatalf("expected head of 10 a's, got %q", got)
	}
	if !strings.Contains(got, "… [+20 chars]") {
		t.Fatalf("expected suppressed-count marker for 20 chars, got %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("expected trailing newline, got %q", got)
	}
}

func TestLineCapWriter_PreservesShortLinesAfterLongOne(t *testing.T) {
	sink, buf := captureSink()
	w := NewLineCapWriter(10, sink)
	w.Write(strings.Repeat("x", 20) + "\nshort\nalso ok\n")
	got := buf.String()
	if !strings.Contains(got, "short\n") {
		t.Fatalf("short line after a clipped one should pass through, got %q", got)
	}
	if !strings.Contains(got, "also ok\n") {
		t.Fatalf("second short line should pass through, got %q", got)
	}
	// Should NOT have a marker on the short lines.
	if strings.Count(got, "…") != 1 {
		t.Fatalf("expected exactly one truncation marker, got %q", got)
	}
}

func TestLineCapWriter_SuppressionAcrossChunkBoundary(t *testing.T) {
	// A long line split across multiple Write() calls. The cap should
	// still hold and the marker should report the *total* dropped
	// count across chunks.
	sink, buf := captureSink()
	w := NewLineCapWriter(8, sink)
	w.Write(strings.Repeat("a", 5))         // under cap, all emitted
	w.Write(strings.Repeat("b", 5))         // crosses cap: 3 emitted, 2 dropped
	w.Write(strings.Repeat("c", 10) + "\n") // 10 dropped, then newline
	got := buf.String()
	// First 8 chars emitted (5 a's + 3 b's), then marker, then newline.
	if !strings.HasPrefix(got, "aaaaabbb") {
		t.Fatalf("expected 'aaaaabbb' prefix, got %q", got)
	}
	if !strings.Contains(got, "… [+12 chars]") {
		t.Fatalf("expected marker for 12 dropped chars (2 b's + 10 c's), got %q", got)
	}
}

func TestLineCapWriter_MultipleLinesInOneChunk(t *testing.T) {
	sink, buf := captureSink()
	w := NewLineCapWriter(4, sink)
	w.Write("hi\nyou\nLONGLONGLONG\nfine\n")
	got := buf.String()
	// "hi", "you", "fine" should pass through; "LONGLONGLONG" gets clipped.
	if !strings.Contains(got, "hi\nyou\n") {
		t.Fatalf("short lines should pass through, got %q", got)
	}
	if !strings.HasSuffix(got, "fine\n") {
		t.Fatalf("trailing short line should pass through, got %q", got)
	}
	if !strings.Contains(got, "… [+8 chars]") {
		t.Fatalf("expected marker for 8 dropped chars (LONGLONG truncated past LONG), got %q", got)
	}
}

func TestLineCapWriter_FlushEmitsMarkerOnUnterminatedLine(t *testing.T) {
	// If the stream ends mid-line in suppressed state, Flush() must
	// still emit the marker so the user knows characters were dropped.
	sink, buf := captureSink()
	w := NewLineCapWriter(4, sink)
	w.Write(strings.Repeat("z", 20)) // no trailing newline
	w.Flush()
	got := buf.String()
	if !strings.HasPrefix(got, "zzzz") {
		t.Fatalf("expected head, got %q", got)
	}
	if !strings.Contains(got, "… [+16 chars]") {
		t.Fatalf("Flush should emit marker, got %q", got)
	}
}

func TestLineCapWriter_NilSinkSafe(t *testing.T) {
	w := NewLineCapWriter(10, nil)
	// Should not panic.
	w.Write("anything")
}

func TestLineCapWriter_EmptyChunkNoop(t *testing.T) {
	sink, buf := captureSink()
	w := NewLineCapWriter(10, sink)
	w.Write("")
	if buf.Len() != 0 {
		t.Fatalf("empty write should not emit anything, got %q", buf.String())
	}
}
