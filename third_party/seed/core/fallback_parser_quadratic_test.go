package core

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// These tests guard against the quadratic worst case in the fallback parser's
// brace matching. The original implementation called matchBrace from every
// opening-brace candidate, making extraction O(N^2) on content with many
// unmatched openers and effectively hanging the conversation goroutine.
// See BUG-FALLBACK-PARSER-QUADRATIC-HANG.md.

// quadraticScaleFactor is the maximum acceptable runtime ratio between an input
// of size N and 2N. Linear work is ~2.0; quadratic is ~4.0 per doubling. A
// threshold of 3.5 comfortably rejects true quadratic scaling (~4.0) while
// leaving headroom for GC pauses and scheduler noise on loaded CI runners,
// where a single STW pause can otherwise push an otherwise-linear ratio past a
// tighter bound and flake the suite. This threshold only needs to reject the
// quadratic signature documented in BUG-FALLBACK-PARSER-QUADRATIC-HANG.md.
const quadraticScaleFactor = 3.5

// TestParse_BareJSON_QuadraticNoHang feeds tens of thousands of unmatched
// opening braces (preceded by a strong fallback marker so the pattern gate
// passes) through Parse end-to-end and asserts:
//   - it returns no tool calls (the content is malformed), and
//   - it completes within a generous bound that the old O(N^2) code would blow.
func TestParse_BareJSON_QuadraticNoHang(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	const openers = 40000
	content := `"arguments" ` + strings.Repeat("{", openers)

	start := time.Now()
	result := fp.Parse(content)
	elapsed := time.Since(start)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls from malformed content, got %d", len(result.ToolCalls))
	}

	// The old O(N^2) implementation took >5s for this input; the linear one is
	// a few milliseconds. A 2s bound is conservative yet catches the regression.
	if elapsed > 2*time.Second {
		t.Fatalf("Parse took %s for %d unmatched openers; expected sub-second (linear)", elapsed, openers)
	}
	t.Logf("%d unmatched openers parsed in %s", openers, elapsed)
}

// TestParse_BareJSON_NotQuadratic verifies that doubling the input does not
// produce ~4x runtime (the quadratic signature from the bug report).
func TestParse_BareJSON_NotQuadratic(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	const n = 40000

	measure := func(openers int) time.Duration {
		content := `"arguments" ` + strings.Repeat("{", openers)
		start := time.Now()
		_ = fp.Parse(content)
		return time.Since(start)
	}

	// Warm up both sizes once before measuring. This primes the allocator and
	// grows the parser's internal structures (and the Go runtime) to their
	// steady-state, reducing cold-start / first-allocation noise.
	_ = measure(n)
	_ = measure(2 * n)

	// Noise (GC pauses, a descheduled CI runner) only ever adds time, so the
	// fastest of several interleaved samples is the stable estimate of each
	// size's real cost. A single sample let one stall push a linear ratio
	// past the threshold.
	const samples = 7
	tN, t2N := time.Duration(1<<62), time.Duration(1<<62)
	for i := 0; i < samples; i++ {
		tN = min(tN, measure(n))
		t2N = min(t2N, measure(2*n))
	}
	t.Logf("N=%d: %s, 2N=%d: %s (ratio %.2f)", n, tN, 2*n, t2N, float64(t2N)/float64(tN))

	if tN > 0 {
		if ratio := float64(t2N) / float64(tN); ratio >= quadraticScaleFactor {
			t.Fatalf("quadratic scaling detected: 2N/N ratio %.2f >= %.1f (linear ~2.0, quadratic ~4.0)",
				ratio, quadraticScaleFactor)
		}
	}
}

// TestExtractBareJSON_QuadraticNoHang exercises the extraction function
// directly (independent of the pattern gate) to lock in linear behavior at the
// exact site of the original bug.
func TestExtractBareJSON_QuadraticNoHang(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	const openers = 50000
	content := strings.Repeat("{", openers)

	start := time.Now()
	blocks := fp.extractBareJSON(content)
	elapsed := time.Since(start)

	if len(blocks) != 0 {
		t.Fatalf("expected no blocks from unmatched openers, got %d", len(blocks))
	}
	if elapsed > 2*time.Second {
		t.Fatalf("extractBareJSON took %s for %d unmatched openers; expected sub-second", elapsed, openers)
	}
}

// TestExtractNamedToolBlocks_QuadraticNoHang guards the second quadratic site:
// extractNamedToolBlocks also called matchBrace per '{' candidate. Many
// identifier-open-brace pairs with unmatched braces must parse in linear time.
func TestExtractNamedToolBlocks_QuadraticNoHang(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	const reps = 50000
	content := strings.Repeat("tool {", reps)

	start := time.Now()
	blocks := fp.extractNamedToolBlocks(content)
	elapsed := time.Since(start)

	// Each "tool {" has an unmatched '{', so no valid JSON blocks are produced.
	if len(blocks) != 0 {
		t.Fatalf("expected no blocks from unmatched named openers, got %d", len(blocks))
	}
	if elapsed > 2*time.Second {
		t.Fatalf("extractNamedToolBlocks took %s for %d reps; expected sub-second", elapsed, reps)
	}
}

// TestComputeBraceMatches_CorrectMatches confirms the O(N) precompute returns
// the correct open->close pairs: brackets inside JSON string literals are
// non-structural and must NOT be matched, escapes are honored, and unmatched
// openers are absent from the map. These inputs are checked against explicit
// expected positions.
func TestComputeBraceMatches_CorrectMatches(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	tests := []struct {
		name string
		s    string
		want map[int]int // openPos -> closePos; omitted openers are unmatched
	}{
		{"empty_object", `{}`, map[int]int{0: 1}},
		{"spaced_object", `{ "x": 1 }`, map[int]int{0: 9}},
		{"nested_arrays", `[[1, 2], [3, 4]]`, map[int]int{0: 15, 1: 6, 9: 14}},
		{"nested_object", `{"a": {"b": 1}}`, map[int]int{0: 14, 6: 13}},
		{"escaped_quote", `{"quote": "she said \"hi\""}`, map[int]int{0: 27}},
		{"brace_in_string", `{"key": "has { brace"}`, map[int]int{0: 21}},
		{"closebrace_in_string", `{"key": "has } brace"}`, map[int]int{0: 21}},
		{"brackets_in_strings", `{"arr": "[1,2,3]", "obj": "{}"}`, map[int]int{0: 30}},
		{"escaped_backslash", `{"key": "\\"}`, map[int]int{0: 12}},
		{"mixed_nested", `[{"a":1},{"b":[2,3]}]`, map[int]int{0: 20, 1: 7, 9: 19, 14: 18}},
		{"deep_mixed", `{"a":[1,2,3],"b":{"c":{}}}`, map[int]int{0: 25, 5: 11, 17: 24, 22: 23}},
		// unmatched openers -> empty map
		{"unmatched_curly", `{`, map[int]int{}},
		{"unmatched_array", `[[[`, map[int]int{}},
		{"unmatched_nested", `{"a": {`, map[int]int{}},
		// per-type stacks: '}' closes '{', ']' closes '[' independently.
		{"mismatched_pairs", `{ [ } ]`, map[int]int{0: 4, 2: 6}},
		{"with_prose", `prefix {"k": "v"} suffix [1,2] tail`,
			map[int]int{7: 16, 25: 29}},
	}
	for _, tc := range tests {
		got := fp.computeBraceMatches(tc.s)
		for openPos, wantClose := range tc.want {
			if gotClose, ok := got[openPos]; !ok {
				t.Errorf("%s: expected pos %d -> %d, got unmatched", tc.name, openPos, wantClose)
			} else if gotClose != wantClose {
				t.Errorf("%s: pos %d = %d, want %d", tc.name, openPos, gotClose, wantClose)
			}
		}
		// No opener should be recorded beyond the expected set.
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %d matches, want %d (got=%v, want=%v)",
				tc.name, len(got), len(tc.want), got, tc.want)
		}
	}
}

// TestComputeBraceMatches_UnterminatedStringBeforeOpener pins the conscious
// behavioral divergence from the old matchBrace when an opening bracket
// immediately follows an unterminated " (an odd number of quotes).
//
// computeBraceMatches carries the "in-string" state forward across the whole
// string, so a '{' or '[' following an unterminated quote is treated as
// inside-string and is NOT recorded as a structural opener. The old matchBrace
// resets inString=false at its call site, so it would have scanned forward and
// returned a (bogus) match.
//
// This is a one-directional reduction: extraction recovers FEWER candidates in
// this case, never more, and the result is lexically more correct. We accept
// it and pin it here so any future change is intentional.
func TestComputeBraceMatches_UnterminatedStringBeforeOpener(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})

	// Each input begins with an unterminated " followed by an opener at a known
	// position, then trailing content that matchBrace would (wrongly) match.
	tests := []struct {
		name      string
		s         string
		openerPos int // index of the opener following the unterminated quote
	}{
		{
			name:      "unterminated_quote_then_curly",
			s:         `"unterminated {"tool_calls": [{"function":{"name":"x","arguments":"{}"}}] }`,
			openerPos: 14, // '{' right after the unterminated quote
		},
		{
			name:      "unterminated_quote_then_array",
			s:         `"unterminated ["tool_calls": [{"function":{"name":"x","arguments":"{}"}}] }`,
			openerPos: 14, // '[' right after the unterminated quote
		},
	}
	for _, tc := range tests {
		got := fp.computeBraceMatches(tc.s)

		// The opener following the unterminated quote must NOT be matched.
		if _, ok := got[tc.openerPos]; ok {
			t.Errorf("%s: expected opener at pos %d to be treated as inside-string (no match), got match to %d",
				tc.name, tc.openerPos, got[tc.openerPos])
		}

		// Document the divergence from the old path. matchBrace resets its
		// string state to false at the call site, so it does NOT see the
		// unterminated quote and may scan forward to a (bogus) match — which
		// for the curly opener here is position 74. For the array opener the
		// trailing structure happens to leave no usable closer, so matchBrace
		// also returns unmatched; either way computeBraceMatches is the more
		// correct result, and we log the matchBrace outcome for the record.
		if _, err := fp.matchBrace(tc.s, tc.openerPos); err != nil {
			t.Logf("%s: matchBrace returns unmatched for pos %d (no divergence this case)", tc.name, tc.openerPos)
		} else {
			t.Logf("%s: matchBrace returns a match for pos %d — the divergence this test pins", tc.name, tc.openerPos)
		}
	}
}

// TestComputeBraceMatches_AgreesWithMatchBraceForCodeOpeners cross-checks the
// precompute against the reference matchBrace for openers that are NOT inside a
// string literal. (matchBrace resets string state at its call site, so it has a
// latent quirk for in-string openers that computeBraceMatches intentionally
// avoids; the extraction callers only ever act on real matches, and the full
// suite confirms no behavioral change.)
func TestComputeBraceMatches_AgreesWithMatchBraceForCodeOpeners(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	// These inputs have no openers inside string literals, so the two
	// implementations must agree everywhere.
	inputs := []string{
		`{}`,
		`{ "x": 1 }`,
		`[[1, 2], [3, 4]]`,
		`{"a": {"b": 1}}`,
		`[{"a":1},{"b":[2,3]}]`,
		`{"a":[1,2,3],"b":{"c":{}}}`,
		`{ [ } ]`,
		`prefix {"k": "v"} suffix [1,2] tail`,
	}
	for _, s := range inputs {
		matches := fp.computeBraceMatches(s)
		for pos := 0; pos < len(s); pos++ {
			c := s[pos]
			if c != '{' && c != '[' {
				continue
			}
			ref, err := fp.matchBrace(s, pos)
			got, ok := matches[pos]
			if err != nil {
				if ok {
					t.Errorf("input %q pos %d: compute=%d but matchBrace=unmatched", s, pos, got)
				}
				continue
			}
			if !ok {
				t.Errorf("input %q pos %d: compute=unmatched but matchBrace=%d", s, pos, ref)
			} else if got != ref {
				t.Errorf("input %q pos %d: compute=%d, matchBrace=%d", s, pos, got, ref)
			}
		}
	}
}

// TestParse_BareJSON_ValidStillExtracts ensures the linear rewrite still
// extracts real tool calls from valid nested JSON with escaped quotes.
func TestParse_BareJSON_ValidStillExtracts(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	content := `{"tool_calls": [{"id": "1", "type": "function", "function": {"name": "search", "arguments": "{\"q\": \"a { b } c\"}"}}]}`
	result := fp.Parse(content)
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Function.Name != "search" {
		t.Errorf("expected name 'search', got %q", result.ToolCalls[0].Function.Name)
	}
}

// BenchmarkParse_UnmatchedBraces documents the per-size cost; N vs 2N should
// scale ~linearly. Not run by `go test -short`; invoke with -bench.
func BenchmarkParse_UnmatchedBraces(b *testing.B) {
	fp := NewFallbackParser(FallbackParserOptions{})
	for _, openers := range []int{10000, 20000, 40000, 80000} {
		content := `"arguments" ` + strings.Repeat("{", openers)
		b.Run(fmt.Sprintf("%dk", openers/1000), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = fp.Parse(content)
			}
		})
	}
}
