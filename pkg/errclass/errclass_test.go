package errclass

import (
	"strings"
	"testing"
)

// TestClassifyFixtures maps representative raw outputs (one or more per
// category) to the expected category and asserts every classification
// carries a non-empty explanation and preserves the raw output verbatim.
func TestClassifyFixtures(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want Category
	}{
		{
			name: "go missing module",
			raw: "# github.com/acme/app\n" +
				"main.go:5:2: no required module provides package github.com/acme/missing; to add it:\n" +
				"\tgo get github.com/acme/missing\n",
			want: CategoryMissingDependency,
		},
		{
			name: "go cannot find package",
			raw:  "main.go:3:8: cannot find package \"example.com/dep\" in any of:\n\t/usr/local/go/src/example.com/dep (from $GOROOT)\n",
			want: CategoryMissingDependency,
		},
		{
			name: "python module not found",
			raw:  "Traceback (most recent call last):\n  File \"app.py\", line 1, in <module>\n    import requests\nModuleNotFoundError: No module named 'requests'\n",
			want: CategoryMissingDependency,
		},
		{
			name: "node module not found",
			raw:  "Error: Cannot find module 'left-pad'\nRequire stack:\n- /app/index.js\n",
			want: CategoryMissingDependency,
		},
		{
			name: "npm 404",
			raw:  "npm ERR! code E404\nnpm ERR! 404 Not Found - GET https://registry.npmjs.org/nope - Not found\n",
			want: CategoryMissingDependency,
		},
		{
			name: "go syntax error",
			raw:  "# github.com/acme/app\n./main.go:7:1: syntax error: unexpected newline in composite literal\n",
			want: CategorySyntaxError,
		},
		{
			name: "go expected operand",
			raw:  "./main.go:12:1: expected operand, found '}'\n",
			want: CategorySyntaxError,
		},
		{
			name: "python syntax error",
			raw:  "  File \"app.py\", line 4\n    def f(\n          ^\nSyntaxError: invalid syntax\n",
			want: CategorySyntaxError,
		},
		{
			name: "js unexpected token",
			raw:  "SyntaxError: Unexpected token '}'\n    at parse (node:internal/modules/cjs/loader:1:1)\n",
			want: CategorySyntaxError,
		},
		{
			name: "go type error cannot use",
			raw:  "./main.go:9:2: cannot use n (variable of type int) as string value in argument to greet\n",
			want: CategoryTypeError,
		},
		{
			name: "go undefined identifier",
			raw:  "./main.go:15:20: undefined: missingHelper\n",
			want: CategoryTypeError,
		},
		{
			name: "typescript type error",
			raw:  "src/index.ts(3,5): error TS2322: Type 'number' is not assignable to type 'string'.\n",
			want: CategoryTypeError,
		},
		{
			name: "typescript property type error alongside expected keyword",
			raw:  "src/index.ts(9,14): error TS2551: Property 'titel' does not exist on type 'Post'. Did you mean 'title'? (expected 'title')\n",
			want: CategoryTypeError,
		},
		{
			name: "go missing identifier alongside unexpected token message",
			raw:  "./main.go:4:2: undefined: helper (expected declaration)\n",
			want: CategoryTypeError,
		}, {
			name: "python type error",
			raw:  "Traceback (most recent call last):\n  File \"app.py\", line 2, in <module>\n    x = 1 + \"a\"\nTypeError: unsupported operand type(s) for +: 'int' and 'str'\n",
			want: CategoryTypeError,
		},
		{
			name: "go failing test",
			raw:  "--- FAIL: TestAdd (0.00s)\n    math_test.go:12: Add(1, 2) = 4, want 3\nFAIL\nFAIL\tgithub.com/acme/app\t0.003s\nFAIL\n",
			want: CategoryFailingTest,
		},
		{
			name: "go failing package fail line",
			raw:  "ok  \tgithub.com/acme/app/util\t0.002s\nFAIL\tgithub.com/acme/app\t0.004s\n",
			want: CategoryFailingTest,
		},
		{
			name: "pytest failures",
			raw:  "============================= test session starts ==============================\ncollected 3 items\n\ntest_app.py F..                                                          [100%]\n\n=================================== FAILURES ===================================\n_________________________________ test_one _________________________________\n\ndef test_one():\n>       assert 1 == 2\nE       assert 1 == 2\n\ntest_app.py:4: AssertionError\n=========================== short test summary info ============================\nFAILED test_app.py::test_one - assert 1 == 2\n",
			want: CategoryFailingTest,
		},
		{
			name: "python value error traceback is not an app crash",
			raw:  "Traceback (most recent call last):\n  File \"build.py\", line 12, in <module>\n    config = json.load(path)\nValueError: not a valid config path\n",
			want: CategoryUnknown,
		}, {
			name: "jest failing suite",
			raw:  "FAIL src/app.test.js\n  ● adds numbers\n\nTests: 1 failed, 2 passed, 3 total\n",
			want: CategoryFailingTest,
		},
		{
			name: "go panic on start",
			raw:  "panic: runtime error: invalid memory address or nil pointer dereference\n[signal SIGSEGV: segmentation violation code=0x1]\n\ngoroutine 1 [running]:\nmain.main()\n\t/app/main.go:10 +0x1a\nexit status 2\n",
			want: CategoryAppCrash,
		},
		{
			name: "port already in use",
			raw:  "Error: listen tcp :8080: bind: address already in use\n",
			want: CategoryAppCrash,
		},
		{
			name: "node port in use",
			raw:  "Error: listen EADDRINUSE: address already in use :::3000\n    at Server.setupListenHandle [as _listen2] (net.js:1:1)\n",
			want: CategoryAppCrash,
		},
		{
			name: "child exited with status",
			raw:  "starting server...\nError: process exited with status 1 before becoming ready\n",
			want: CategoryAppCrash,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.raw)
			if got.Category != tc.want {
				t.Fatalf("Classify(%q).Category = %q, want %q", tc.name, got.Category, tc.want)
			}
			if strings.TrimSpace(got.Explanation) == "" {
				t.Fatalf("Classify(%q) returned an empty explanation", tc.name)
			}
			if got.Raw != tc.raw {
				t.Fatalf("Classify(%q) did not preserve the raw output verbatim", tc.name)
			}
		})
	}
}

// TestClassifyUnknownIsConservative asserts that output matching no known
// failure shape is reported as unknown rather than guessed at.
func TestClassifyUnknownIsConservative(t *testing.T) {
	unmatched := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"whitespace", "   \n\t\n"},
		{"generic message", "operation not permitted\n"},
		{"unrelated log", "2026-01-01T00:00:00Z INFO starting\n2026-01-01T00:00:00Z INFO done\n"},
	}
	for _, tc := range unmatched {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.raw)
			if got.Category != CategoryUnknown {
				t.Fatalf("Classify(%q).Category = %q, want %q", tc.name, got.Category, CategoryUnknown)
			}
			if got.Category.Known() {
				t.Fatalf("CategoryUnknown.Known() = true, want false")
			}
			if strings.TrimSpace(got.Explanation) == "" {
				t.Fatalf("unknown category must still carry an explanation")
			}
			if got.Signal != "" {
				t.Fatalf("unknown classification must carry no signal, got %q", got.Signal)
			}
			if got.Raw != tc.raw {
				t.Fatalf("unknown classification did not preserve the raw output")
			}
		})
	}
}

// TestExplanationAccompaniesRawOutput pins the display contract: a
// classification exposes both the raw output and the explanation, and the
// explanation is a fixed template that does not embed the raw text.
func TestExplanationAccompaniesRawOutput(t *testing.T) {
	raw := "panic: boom\n"
	got := Classify(raw)
	if got.Raw != raw {
		t.Fatalf("Raw = %q, want input preserved verbatim", got.Raw)
	}
	if !strings.Contains(got.Explanation, "crash") {
		t.Fatalf("explanation for a panic should describe a crash, got %q", got.Explanation)
	}
	if strings.Contains(got.Explanation, "boom") {
		t.Fatalf("explanation must not embed the raw output, got %q", got.Explanation)
	}
}

// TestCategoryExplanationIsStable asserts every closed-set category has a
// distinct, non-empty explanation and that an unrecognized value falls
// back to the unknown explanation rather than an empty string.
func TestCategoryExplanationIsStable(t *testing.T) {
	seen := map[string]Category{}
	for _, c := range Categories() {
		exp := c.Explanation()
		if strings.TrimSpace(exp) == "" {
			t.Fatalf("category %q has an empty explanation", c)
		}
		if other, ok := seen[exp]; ok {
			t.Fatalf("categories %q and %q share an explanation", c, other)
		}
		seen[exp] = c
	}
	if got := Category("bogus").Explanation(); got != explainUnknown {
		t.Fatalf("unrecognized category explanation = %q, want the unknown template", got)
	}
}

// TestCategoriesClosedSet pins the taxonomy: exactly the five classified
// failures plus unknown, in a stable order.
func TestCategoriesClosedSet(t *testing.T) {
	want := []Category{
		CategoryMissingDependency,
		CategorySyntaxError,
		CategoryTypeError,
		CategoryFailingTest,
		CategoryAppCrash,
		CategoryUnknown,
	}
	got := Categories()
	if len(got) != len(want) {
		t.Fatalf("Categories() has %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Categories()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestKnownCategories covers the Known predicate across the closed set.
func TestKnownCategories(t *testing.T) {
	for _, c := range Categories() {
		wantKnown := c != CategoryUnknown
		if c.Known() != wantKnown {
			t.Fatalf("%q.Known() = %v, want %v", c, c.Known(), wantKnown)
		}
	}
	if Category("").Known() {
		t.Fatalf("empty category reported as known")
	}
}

// TestResultCarriesSignalForKnownCategories asserts a classified result
// names the deciding pattern, which lets a caller log why a category was
// chosen without re-running the match.
func TestResultCarriesSignalForKnownCategories(t *testing.T) {
	got := Classify("./main.go:3:8: cannot find package \"x\"")
	if got.Category != CategoryMissingDependency {
		t.Fatalf("category = %q, want missing dependency", got.Category)
	}
	if got.Signal == "" {
		t.Fatalf("known classification should name its signal")
	}
}
