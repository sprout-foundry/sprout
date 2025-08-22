package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLooksLikeDocOnly_PositiveAndNegative(t *testing.T) {
	if looksLikeDocOnly("comment out this code") {
		t.Fatalf("'comment out' should be negative")
	}
	if looksLikeDocOnly("change a to b") {
		t.Fatalf("replacement phrasing should be negative")
	}
	if !looksLikeDocOnly("add header summary at top of file pkg/agent/agent.go") {
		t.Fatalf("expected positive doc-only detection")
	}
}

func TestDetectCommentStyle_LineVsBlock(t *testing.T) {
	if st := detectCommentStyle("file.go"); st.kind != "line" || st.linePrefix == "" {
		t.Fatalf("go style line expected")
	}
	if st := detectCommentStyle("file.py"); st.kind != "line" || st.linePrefix != "# " {
		t.Fatalf("py style line expected")
	}
	if st := detectCommentStyle("file.css"); st.kind != "block" || st.blockStart != "/*" {
		t.Fatalf("css block expected")
	}
	if st := detectCommentStyle("file.html"); st.kind != "block" || st.blockStart != "<!--" {
		t.Fatalf("html block expected")
	}
}

func TestWrapText(t *testing.T) {
	lines := wrapText("a b c d e f g", 3)
	if len(lines) == 0 {
		t.Fatalf("expected some wrapping")
	}
}

func TestInsertTopComment_PreservesEOL(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.go")
	content := "package x\r\n\r\nfunc A(){}\r\n"
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := insertTopComment(p, "hello world header"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	// Should contain CRLF preserved after insertion
	if idx := indexOf(s, "\r\npackage x"); idx == -1 {
		t.Fatalf("expected CRLF preserved, got: %q", s)
	}
}
