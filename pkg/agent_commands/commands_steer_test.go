package commands

import (
	"strings"
	"testing"
)

func TestSteerCompletionCandidates(t *testing.T) {
	r := NewCommandRegistry()

	candidates := r.SteerCompletionCandidates()

	// Convert to a map for easy lookup.
	candMap := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		candMap[c] = true
	}

	// "info" is steer-safe (SafeDuringSteer() == true).
	if !candMap["info"] {
		t.Error("SteerCompletionCandidates should include \"info\" (steer-safe)")
	}

	// "commit" is NOT steer-safe (SafeDuringSteer() == false).
	if candMap["commit"] {
		t.Error("SteerCompletionCandidates should NOT include \"commit\" (unsafe during steer)")
	}

	// "search" is steer-safe, so its alias "s" should appear.
	if !candMap["s"] {
		t.Error("SteerCompletionCandidates should include alias \"s\" (alias for steer-safe \"search\")")
	}

	// "commit" alias "c" should NOT appear (canonical "commit" is unsafe).
	if candMap["c"] {
		t.Error("SteerCompletionCandidates should NOT include alias \"c\" (alias for unsafe \"commit\")")
	}
}

func TestSteerCompletionCandidates_SortedAlphabetically(t *testing.T) {
	r := NewCommandRegistry()
	candidates := r.SteerCompletionCandidates()

	// Verify the result is sorted.
	for i := 1; i < len(candidates); i++ {
		if candidates[i] < candidates[i-1] {
			t.Errorf("candidates not sorted: %q (index %d) < %q (index %d)",
				candidates[i], i, candidates[i-1], i-1)
		}
	}
}

func TestSteerCompletionCandidates_SubsetOfAllCandidates(t *testing.T) {
	r := NewCommandRegistry()

	all := r.CompletionCandidates()
	steer := r.SteerCompletionCandidates()

	allSet := make(map[string]bool, len(all))
	for _, c := range all {
		allSet[c] = true
	}

	// Every steer candidate must be in the full set.
	for _, s := range steer {
		if !allSet[s] {
			t.Errorf("steer candidate %q not found in full CompletionCandidates list", s)
		}
	}

	// Steer must be a strict subset (at least one command is excluded).
	if len(steer) >= len(all) {
		t.Errorf("steer candidates (%d) should be fewer than all candidates (%d)", len(steer), len(all))
	}
}

func TestSteerCompletionCandidates_Caching(t *testing.T) {
	r := NewCommandRegistry()

	first := r.SteerCompletionCandidates()
	second := r.SteerCompletionCandidates()

	// Same cached result — content should be identical.
	if len(first) != len(second) {
		t.Fatal("steer candidate counts differ between calls")
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("steer candidate mismatch at index %d: %q vs %q", i, first[i], second[i])
		}
	}

	if len(first) == 0 {
		t.Fatal("should have at least some steer-safe candidates")
	}
}

func TestSteerCompletionCandidates_NoDuplicates(t *testing.T) {
	r := NewCommandRegistry()
	candidates := r.SteerCompletionCandidates()

	seen := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		if seen[c] {
			t.Errorf("duplicate steer candidate: %q", c)
		}
		seen[c] = true
	}
}

func TestSteerCompletionCandidates_NoSlashPrefix(t *testing.T) {
	r := NewCommandRegistry()
	candidates := r.SteerCompletionCandidates()

	for _, c := range candidates {
		if strings.HasPrefix(c, "/") {
			t.Errorf("steer candidate %q should not have / prefix (names only, like CompletionCandidates)", c)
		}
	}
}
