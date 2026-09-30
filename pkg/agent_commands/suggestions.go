package commands

import (
	"sort"
	"strings"
	"unicode"
)

// SuggestCommands returns up to maxSuggestions command names that are
// plausible corrections for a mistyped command. Matches are ranked by
// Levenshtein distance against the lowercased canonical name; commands
// whose lowercased name has the input as a prefix are pulled to the front
// (distance 0). Returns nil for empty input or when no candidate is
// reasonably close.
func (r *CommandRegistry) SuggestCommands(name string, maxSuggestions int) []string {
	if name == "" || maxSuggestions <= 0 {
		return nil
	}
	target := strings.ToLower(strings.TrimSpace(name))

	type scored struct {
		name string
		dist int
	}

	const maxDistance = 3
	best := make(map[string]int, len(r.commands))

	consider := func(candidate, canonical string) {
		lower := strings.ToLower(candidate)
		dist := levenshtein(target, lower)
		// Treat prefix matches as best-case so "cm" → "commit" wins over
		// equally-distant but unrelated names.
		if strings.HasPrefix(lower, target) {
			dist = 0
		}
		// Short names are within edit distance of almost any short typo, so
		// the budget never exceeds the candidate's own length.
		if dist > maxDistance || dist >= len(lower) {
			return
		}
		if prev, ok := best[canonical]; !ok || dist < prev {
			best[canonical] = dist
		}
	}

	for n := range r.commands {
		consider(n, n)
	}
	// Aliases route typos to their command (e.g. /md → /m → /model), but
	// the suggestion names the command, and punctuation aliases like /?
	// never match a word.
	for alias, canonical := range r.aliases {
		if isPunctuationAlias(alias) {
			continue
		}
		consider(alias, canonical)
	}

	candidates := make([]scored, 0, len(best))
	for n, d := range best {
		candidates = append(candidates, scored{name: n, dist: d})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].dist != candidates[j].dist {
			return candidates[i].dist < candidates[j].dist
		}
		return candidates[i].name < candidates[j].name
	})

	if maxSuggestions > len(candidates) {
		maxSuggestions = len(candidates)
	}
	out := make([]string, maxSuggestions)
	for i := 0; i < maxSuggestions; i++ {
		out[i] = candidates[i].name
	}
	return out
}

func isPunctuationAlias(alias string) bool {
	for _, r := range alias {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// levenshtein returns the edit distance between a and b. O(len(a) × len(b))
// in both time and memory; command names are short so the naive
// implementation is fine.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}

	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			deletion := prev[j] + 1
			insertion := curr[j-1] + 1
			substitution := prev[j-1] + cost
			curr[j] = min3(deletion, insertion, substitution)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
