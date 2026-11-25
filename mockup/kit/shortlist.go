// Shortlisting: the palettes a draw offers the model. A shortlist is
// sampled across colour families — so two ideas do not come out alike by
// palette family — from the base palettes, filtered to those fitting the
// brief's mood words and not avoided, and seeded so tests can reproduce it.
package kit

import (
	"math/rand/v2"
	"sort"
	"strings"
)

// Shortlist samples up to n base palettes across families. Tokens are the
// brief's words; a palette fits when one of its mood words matches a token
// (a shared prefix of at least 4 letters counts). Avoided holds the colour
// families to exclude — unless Named is non-empty, in which case the named
// colours always win and avoidance is ignored. Named lists colour families
// the person named in their words ("blue and orange" → blue, orange). The
// seed makes the sample reproducible.
func Shortlist(tokens, named []string, avoided map[string]bool, seed int64, n int) []Palette {
	if n <= 0 {
		n = 1
	}
	namedSet := map[string]bool{}
	for _, f := range named {
		namedSet[f] = true
	}
	type scored struct {
		p Palette
		s int
	}
	var cand []scored
	for _, p := range BasePalettes() {
		f := p.Family()
		if len(namedSet) > 0 {
			if !namedSet[f] {
				continue
			}
			cand = append(cand, scored{p, 0})
			continue
		} else {
			if avoided[f] {
				continue
			}
			s := 0
			for _, m := range strings.Fields(p.Mood) {
				m = strings.ToLower(m)
				for _, t := range tokens {
					if tokenMatches(t, m) {
						s++
						break
					}
				}
			}
			if s > 0 {
				cand = append(cand, scored{p, s})
			} else {
				cand = append(cand, scored{p, 0})
			}
		}
	}
	if len(cand) == 0 {
		return nil
	}
	// A palette that fits the brief's mood outranks one that does not; the
	// seed breaks ties so the sample is reproducible.
	rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed)*2862933555777941757+3037000500))
	rng.Shuffle(len(cand), func(i, j int) { cand[i], cand[j] = cand[j], cand[i] })
	sort.SliceStable(cand, func(i, j int) bool { return cand[i].s > cand[j].s })
	// One candidate per family first — a mood match outranks one that
	// fits nothing, but every family is tried before any family is
	// picked twice — then fill the rest.
	seen := map[string]bool{}
	var out []Palette
	for _, c := range cand {
		if len(out) >= n {
			return out
		}
		if seen[c.p.Family()] {
			continue
		}
		seen[c.p.Family()] = true
		out = append(out, c.p)
	}
	for _, c := range cand {
		if len(out) >= n {
			break
		}
		if containsPalette(out, c.p.ID) {
			continue
		}
		if countOf(out, c.p.Family()) >= 2 {
			continue
		}
		out = append(out, c.p)
	}
	return out
}

func countOf(ps []Palette, family string) int {
	n := 0
	for _, p := range ps {
		if p.Family() == family {
			n++
		}
	}
	return n
}

func containsPalette(ps []Palette, id string) bool {
	for _, p := range ps {
		if p.ID == id {
			return true
		}
	}
	return false
}

// tokenMatches: exact, or a shared prefix of at least 4 letters, so
// "walks" matches a mood word "walk".
func tokenMatches(token, word string) bool {
	if token == word {
		return true
	}
	if len(token) < 4 || len(word) < 4 {
		return false
	}
	return strings.HasPrefix(token, word) || strings.HasPrefix(word, token)
}
