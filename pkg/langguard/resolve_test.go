package langguard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A reliably-detectable Persian sentence. Persian has no ISO 639-1 code, so
// the trigram pass reports its 639-3 code ("pes") — the fixture for the
// "no 639-1 code round-trips" case (SP-152 152.3).
const persianProse = "هر شخص حق آزادی و حقوق برابر دارد و کشور باید از آنها محافظت کند."

// reliableSpanish and friends are the package-level fixtures from
// detect_test.go (all detected reliably by DetectLanguage). They are used
// here as "recent user messages".

// TestResolveUserLanguageMajority covers the core resolution (§152a): a
// clear majority over the reliable per-message detections resolves to that
// language, and a clear majority beats a configured fallback.
func TestResolveUserLanguageMajority(t *testing.T) {
	cases := []struct {
		name           string
		messages       []string
		configured     Language
		want           Language
		wantDetermined bool
	}{
		{
			name:           "majority Spanish beats English minority",
			messages:       []string{spanishProse, spanishProse, englishProse},
			want:           Language{Code: "es", Name: "Spanish"},
			wantDetermined: true,
		},
		{
			name:           "single reliable message is its own majority",
			messages:       []string{russianProse},
			want:           Language{Code: "ru", Name: "Russian"},
			wantDetermined: true,
		},
		{
			name: "unique minority mode wins over a tie-free field",
			// Russian is the unique most-frequent (2 votes); the other
			// three languages tie at 1 each. The winner is unique, so it
			// resolves — it need not be an absolute majority of the
			// recent messages.
			messages:       []string{russianProse, russianProse, englishProse, spanishProse, chineseProse},
			want:           Language{Code: "ru", Name: "Russian"},
			wantDetermined: true,
		},
		{
			name:           "majority wins even when a configured value is set",
			messages:       []string{spanishProse, spanishProse, englishProse},
			configured:     Language{Code: "fr", Name: "French"},
			want:           Language{Code: "es", Name: "Spanish"},
			wantDetermined: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, determined := ResolveUserLanguage(tc.messages, tc.configured)
			assert.Equal(t, tc.wantDetermined, determined)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestResolveUserLanguageFallback covers the short / mixed / code-only input
// falling back to the configured language (§152a).
func TestResolveUserLanguageFallback(t *testing.T) {
	configured := Language{Code: "es", Name: "Spanish"}

	codeOnly := "```go\n" + `x := 1;` + "\n```\ndone"

	cases := []struct {
		name     string
		messages []string
	}{
		{"single very short message", []string{"Hola"}},
		{"all messages too short", []string{"OK", "si", "yes"}},
		{"code-only messages are not prose", []string{codeOnly, codeOnly}},
		{"genuinely mixed: tied top", []string{spanishProse, russianProse}},
		{"genuinely mixed: three-way tie", []string{spanishProse, russianProse, englishProse}},
		{"empty message slice", nil},
		{"empty strings", []string{"", ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, determined := ResolveUserLanguage(tc.messages, configured)
			assert.True(t, determined, "should fall back to the configured language")
			assert.Equal(t, configured, got)
		})
	}
}

// TestResolveUserLanguageUndetermined covers the no-configured-value case:
// with no reliable detection and nothing configured, the resolver returns
// the zero Language (undetermined) and must not guess (§152a).
func TestResolveUserLanguageUndetermined(t *testing.T) {
	cases := map[string][]string{
		"single very short message":     {"Hola"},
		"code-only message":             {"```go\nx := 1;\n```"},
		"mixed with nothing configured": {spanishProse, russianProse},
		"empty":                         {},
	}
	for name, msgs := range cases {
		t.Run(name, func(t *testing.T) {
			got, determined := ResolveUserLanguage(msgs, Language{})
			assert.False(t, determined, "must be undetermined")
			assert.Equal(t, Language{}, got, "zero language means undetermined")
		})
	}
}

// TestResolveUserLanguageNo6391RoundTrip covers the "no fixed language list"
// property for a language without an ISO 639-1 code: the 639-3 code the
// detector reports must survive resolution and parse back.
func TestResolveUserLanguageNo6391RoundTrip(t *testing.T) {
	// A reliable Persian message: the detector reports its 639-3 code.
	detected, _, reliable := DetectLanguage(persianProse)
	require.True(t, reliable, "the Persian fixture must be reliably detected")
	assert.Equal(t, "pes", detected.Code, "Persian has no 639-1 code, so it reports 639-3")

	// Majority of Persian messages resolves to the same 639-3 code.
	messages := []string{persianProse, persianProse, spanishProse}
	got, determined := ResolveUserLanguage(messages, Language{})
	require.True(t, determined)
	assert.Equal(t, "pes", got.Code, "the 639-3 code must round-trip through the resolver")

	// ParseLanguage accepts the 639-3 code and round-trips it to the same
	// identity the resolver produced.
	parsed := ParseLanguage("pes")
	assert.Equal(t, got.Code, parsed.Code)
	assert.Equal(t, "Persian", parsed.Name)
}

// TestParseLanguage covers the read side of the configured language setting:
// 639-1 and 639-3 codes, case-insensitivity, canonicalization to the code
// form the detector reports, and empty / unrecognized codes collapsing to
// the zero Language.
func TestParseLanguage(t *testing.T) {
	cases := []struct {
		name string
		code string
		want Language
	}{
		{"639-1 English", "en", Language{Code: "en", Name: "English"}},
		{"639-1 Spanish", "es", Language{Code: "es", Name: "Spanish"}},
		{"639-3 Persian (no 639-1)", "pes", Language{Code: "pes", Name: "Persian"}},
		{"639-3 Cebuano (no 639-1)", "ceb", Language{Code: "ceb", Name: "Cebuano"}},
		// A 639-3 code for a language that also has a 639-1 code
		// canonicalizes to the 639-1 form the detector reports, so it
		// stays comparable to DetectLanguage output by code.
		{"639-3 input canonicalizes to 639-1", "spa", Language{Code: "es", Name: "Spanish"}},
		{"639-3 input canonicalizes to 639-1 (English)", "eng", Language{Code: "en", Name: "English"}},
		{"upper-case 639-1", "ES", Language{Code: "es", Name: "Spanish"}},
		{"mixed case 639-3", "PES", Language{Code: "pes", Name: "Persian"}},
		{"surrounding whitespace", "  fr ", Language{Code: "fr", Name: "French"}},
		{"empty", "", Language{}},
		{"unrecognized code", "xx", Language{}},
		{"non-ISO string", "English", Language{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ParseLanguage(tc.code))
		})
	}
}

// TestParseLanguageMatchesDetector pins the MUST_FIX from review: the Code
// ParseLanguage returns must be the same code form DetectLanguage reports
// for the same language, so a configured value is comparable to a detection
// result by code equality (152.5's CheckLanguage depends on this).
func TestParseLanguageMatchesDetector(t *testing.T) {
	// Spanish has both a 639-1 code ("es") and a 639-3 code ("spa"); the
	// detector reports the 639-1 form.
	detected, _, reliable := DetectLanguage(spanishProse)
	require.True(t, reliable)
	assert.Equal(t, "es", detected.Code)
	assert.Equal(t, detected.Code, ParseLanguage("es").Code, "639-1 input")
	assert.Equal(t, detected.Code, ParseLanguage("spa").Code, "639-3 input canonicalizes to 639-1")

	// Persian has no 639-1 code; the detector reports the 639-3 form.
	assert.Equal(t, "pes", ParseLanguage("pes").Code, "639-3-only language")
}

// TestParseLanguageFeedsResolver proves the read-side contract end to end: a
// config string read through ParseLanguage works as the configured fallback
// for the resolver.
func TestParseLanguageFeedsResolver(t *testing.T) {
	configured := ParseLanguage("ru")
	require.Equal(t, "ru", configured.Code)

	// Short input with a configured Russian fallback resolves to Russian.
	got, determined := ResolveUserLanguage([]string{"OK"}, configured)
	require.True(t, determined)
	assert.Equal(t, "ru", got.Code)

	// The same configured value also lets a reliable detection agree.
	en := ParseLanguage("en")
	mixed := []string{englishProse, englishProse, spanishProse}
	got, determined = ResolveUserLanguage(mixed, en)
	require.True(t, determined)
	assert.Equal(t, "en", got.Code, "a clear majority wins over the configured value")
}
