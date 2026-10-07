// User-language resolution: work out the language the user
// is writing in, from their own recent messages, with a configured
// fallback for very short or mixed input. There is no fixed list of
// supported languages — any language the detector recognizes is a valid
// target, so the resolver has no hardcoded language list of its
// own; it simply votes over what the trigram detector reports.
//
// The resolver is deterministic, in-process, and pure Go (no cgo): it runs
// the same DetectLanguage call over each recent message, takes the majority
// language over the reliable per-message detections, and falls back to a
// configured language when the input is too short or too mixed to yield a
// unique winner. It is shaped so the final-message guard can consume
// it directly, and can be called by other Go programs embedding sprout.
package langguard

import (
	"strings"

	whatlang "github.com/abadojack/whatlanggo"
)

// ResolveUserLanguage resolves the user's language from their own
// recent messages (raw text — prose is extracted internally) with a
// configured fallback. It returns the resolved Language and whether it was
// determined (a non-zero language the caller can judge a reply against).
//
// The resolution is a majority vote over the reliable per-message
// detections. Each message is run through DetectLanguage over its extracted
// prose, and only the reliable results (reliable == true) count; they are
// grouped by language Code case-insensitively. The caller decides how many
// "recent" messages to pass (a few turns); the resolver votes over exactly
// the messages it is given.
//
//   - A unique winner — the single most-frequent language, with no tie at
//     the top — wins, whether or not it is an absolute majority of the
//     recent messages. It is the user's most consistent writing language.
//   - If there are no reliable detections at all (every message is too
//     short, code-only, or not judgable), or the messages are genuinely
//     mixed (a tie for the top, i.e. no unique winner), the configured
//     fallback is returned.
//   - If there is no configured fallback either (its Code is empty), the
//     zero Language (Code == "") is returned with determined == false. The
//     caller treats the user's language as undetermined; the guard never
//     guesses.
//
// A clear majority always beats a configured value: the configured language
// is only a fallback for short or mixed input, never a tie-breaker.
func ResolveUserLanguage(messages []string, configured Language) (Language, bool) {
	counts := make(map[string]int)
	firstSeen := make(map[string]Language)
	for _, msg := range messages {
		lang, _, reliable := DetectLanguage(ExtractProse(msg))
		if !reliable || lang.Code == "" {
			continue
		}
		key := strings.ToLower(lang.Code)
		counts[key]++
		if _, ok := firstSeen[key]; !ok {
			firstSeen[key] = lang
		}
	}
	if len(counts) == 0 {
		return fallbackLanguage(configured)
	}

	top := 0
	for _, n := range counts {
		if n > top {
			top = n
		}
	}
	winners := 0
	var winnerKey string
	for code, n := range counts {
		if n == top {
			winners++
			winnerKey = code
		}
	}
	if winners != 1 {
		// A tie at the top: genuinely mixed input, fall back to the
		// configured language.
		return fallbackLanguage(configured)
	}
	return firstSeen[winnerKey], true
}

// fallbackLanguage returns the configured language when one was set, and
// the zero Language (undetermined) when none was.
func fallbackLanguage(configured Language) (Language, bool) {
	if configured.Code != "" {
		return configured, true
	}
	return Language{}, false
}

// langByCode maps a normalized (lower-case) ISO 639-1 or 639-3 code to the
// detector's language, built once over the detector's own catalog. Both
// code families resolve to the same language, so a configured value may
// use either. 639-1 codes are two letters and 639-3 codes three, so the two
// families never collide.
var langByCode = func() map[string]whatlang.Lang {
	m := make(map[string]whatlang.Lang, 128)
	for lang := range whatlang.Langs {
		if c := lang.Iso6391(); c != "" {
			m[strings.ToLower(c)] = lang
		}
		if c := lang.Iso6393(); c != "" {
			m[strings.ToLower(c)] = lang
		}
	}
	return m
}()

// ParseLanguage turns a configured language code into a Language. It accepts
// an ISO 639-1 code ("es", "en", ...) or, for the few languages without a
// 639-1 code, the 639-3 code ("pes", "ceb", ...), case-insensitively. The
// Name is filled from the detector's catalog when the code is recognized,
// so a parsed language renders usefully in diagnostics.
//
// The Code is canonicalized to the same form DetectLanguage reports — the
// 639-1 code when the language has one, otherwise the 639-3 code — so a
// configured value stays comparable to a detection result by code equality.
// A configured 639-3 code for a language that also has a 639-1 code (e.g.
// "spa" for Spanish) canonicalizes to the 639-1 code ("es"), the form the
// trigram pass emits. This matters because the final-message guard's
// CheckLanguage compares the reply's detected code against the user's code.
//
// An empty code, or a code the detector does not recognize, returns the zero
// Language (Code == ""): "no fixed language list" means the detector's own
// catalog is the universe of valid targets, and a code outside it
// is undetermined rather than guessed. This is the read side of the
// configured language setting: read the config field, pass it here, and feed
// the result to ResolveUserLanguage.
func ParseLanguage(code string) Language {
	c := strings.ToLower(strings.TrimSpace(code))
	if c == "" {
		return Language{}
	}
	lang, ok := langByCode[c]
	if !ok {
		return Language{}
	}
	// Canonical code form, mirroring DetectLanguage in language.go: 639-1
	// when the language has one, else 639-3.
	canonical := lang.Iso6391()
	if canonical == "" {
		canonical = lang.Iso6393()
	}
	return Language{Code: strings.ToLower(canonical), Name: lang.String()}
}
