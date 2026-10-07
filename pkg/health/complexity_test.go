package health

import "testing"

func TestCountLines(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"empty", "", 0},
		{"no newline", "abc", 1},
		{"one newline", "abc\n", 1},
		{"two lines", "abc\ndef", 2},
		{"two lines trailing", "abc\ndef\n", 2},
		{"blank lines count", "a\n\n\nb\n", 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CountLines([]byte(tc.content)); got != tc.want {
				t.Fatalf("CountLines(%q) = %d, want %d", tc.content, got, tc.want)
			}
		})
	}
}

func TestBranchingComplexityGo(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"straight line", "func f() int { return 1 }", 1},
		{"single if", "func f(x int) int { if x > 0 { return 1 }; return 0 }", 2},
		{"if and for", "func f(xs []int) int { n := 0; for _, x := range xs { if x > 0 { n++ } }; return n }", 3},
		{"bool operators", "func f(a, b bool) bool { return a && b || a }", 3},
		{"switch arms", "func f(x int) int { switch x { case 1: return 1; case 2: return 2 }; return 0 }", 3},
		{"comment keyword ignored", "func f() int { // if for while\n return 1 }", 1},
		{"string keyword ignored", "func f() string { return \"if for while\" }", 1},
		{"select is one branch", "func f() { select { case <-a: case <-b: } }", 2},
		{"select arm with inner block", "func f() { select { case <-a: { x := 1; _ = x }; case <-b: } }", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BranchingComplexity(tc.body, "go"); got != tc.want {
				t.Fatalf("BranchingComplexity(%q, go) = %d, want %d", tc.body, got, tc.want)
			}
		})
	}
}

func TestBranchingComplexityPython(t *testing.T) {
	body := "def f(x):\n    if x:\n        return 1\n    for i in range(3):\n        pass\n    # while loop comment\n    return 0\n"
	// if + for = 2 branches, comment ignored → 2 + 1 = 3
	if got := BranchingComplexity(body, "python"); got != 3 {
		t.Fatalf("complexity = %d, want 3", got)
	}
}

func TestBranchingComplexityPythonHashComment(t *testing.T) {
	body := "def f():\n    return 1  # for while if in a comment\n"
	if got := BranchingComplexity(body, "python"); got != 1 {
		t.Fatalf("complexity = %d, want 1 (hash comment must be ignored)", got)
	}
}
