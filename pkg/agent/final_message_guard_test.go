package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/langguard"
)

// Reliably detectable prose fixtures, mirrored from pkg/langguard's
// detect fixtures (the trigram detector reports them above its
// reliability bar, so CheckLanguage yields a decisive verdict).
const (
	fmEnglishProse = "The build succeeded after applying the patch, so the tests can run and the release is ready to ship."
	fmSpanishProse = "El paquete está listo para compilar ahora mismo y las pruebas pasan sin errores."
)

var fmSpanish = langguard.Language{Code: "es", Name: "Spanish"}

// TestFinalMessageGuardBehaviorMatrix is the §152b acceptance contract:
// the full pass / undetermined / mismatch matrix with the regenerate call
// count, and the Display / Original / Regenerated / Mismatched fields
// for every branch.
func TestFinalMessageGuardBehaviorMatrix(t *testing.T) {
	spanishNotice := LanguageMismatchNotice(fmSpanish)

	cases := []struct {
		name         string
		user         langguard.Language
		finalText    string
		regenText    string
		regenErr     error
		regenNil     bool
		wantDisplay  string
		wantOriginal string
		wantRegen    bool
		wantMismatch bool
		wantCalls    int
	}{
		{
			name:        "undetermined user language: passthrough, no regeneration",
			user:        langguard.Language{},
			finalText:   fmEnglishProse,
			wantDisplay: fmEnglishProse,
			wantCalls:   0,
		},
		{
			name:        "pass: reply in the user's language",
			user:        fmSpanish,
			finalText:   fmSpanishProse,
			wantDisplay: fmSpanishProse,
			wantCalls:   0,
		},
		{
			name:        "undetermined verdict: too short to judge",
			user:        fmSpanish,
			finalText:   "Hola",
			wantDisplay: "Hola",
			wantCalls:   0,
		},
		{
			name:         "mismatch: regeneration repairs it",
			user:         fmSpanish,
			finalText:    fmEnglishProse,
			regenText:    fmSpanishProse,
			wantDisplay:  fmSpanishProse,
			wantOriginal: fmEnglishProse,
			wantRegen:    true,
			wantMismatch: true,
			wantCalls:    1,
		},
		{
			name:         "mismatch: regeneration still mismatches",
			user:         fmSpanish,
			finalText:    fmEnglishProse,
			regenText:    fmEnglishProse,
			wantDisplay:  spanishNotice,
			wantOriginal: fmEnglishProse,
			wantRegen:    true,
			wantMismatch: true,
			wantCalls:    1,
		},
		{
			name:         "mismatch: regeneration errors",
			user:         fmSpanish,
			finalText:    fmEnglishProse,
			regenErr:     errors.New("model call failed"),
			wantDisplay:  spanishNotice,
			wantOriginal: fmEnglishProse,
			wantMismatch: true,
			wantCalls:    1,
		},
		{
			name:         "mismatch: regeneration produced no text",
			user:         fmSpanish,
			finalText:    fmEnglishProse,
			wantDisplay:  spanishNotice,
			wantOriginal: fmEnglishProse,
			wantMismatch: true,
			wantCalls:    1,
		},
		{
			name:         "mismatch without a regenerator shows the notice",
			user:         fmSpanish,
			finalText:    fmEnglishProse,
			regenNil:     true,
			wantDisplay:  spanishNotice,
			wantOriginal: fmEnglishProse,
			wantMismatch: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var regenerate Regenerator
			if !tc.regenNil {
				regenerate = func(ctx context.Context) (string, error) {
					calls++
					return tc.regenText, tc.regenErr
				}
			}
			outcome := FinalMessageGuard(context.Background(), tc.finalText, tc.user, regenerate)
			if outcome.Display != tc.wantDisplay {
				t.Errorf("Display = %q, want %q", outcome.Display, tc.wantDisplay)
			}
			if outcome.Original != tc.wantOriginal {
				t.Errorf("Original = %q, want %q", outcome.Original, tc.wantOriginal)
			}
			if outcome.Regenerated != tc.wantRegen {
				t.Errorf("Regenerated = %v, want %v", outcome.Regenerated, tc.wantRegen)
			}
			if outcome.Mismatched != tc.wantMismatch {
				t.Errorf("Mismatched = %v, want %v", outcome.Mismatched, tc.wantMismatch)
			}
			if calls != tc.wantCalls {
				t.Errorf("regenerate called %d times, want exactly %d", calls, tc.wantCalls)
			}
		})
	}
}

// TestFinalMessageGuardPassThroughIsByteIdentical pins the contract that
// a pass / undetermined result is the input itself, no re-wrapping.
func TestFinalMessageGuardPassThroughIsByteIdentical(t *testing.T) {
	finalText := "  " + fmSpanishProse + "\n\n"
	outcome := FinalMessageGuard(context.Background(), finalText, fmSpanish, func(context.Context) (string, error) {
		t.Error("regenerate must not be called on a pass")
		return "", nil
	})
	if outcome.Display != finalText {
		t.Errorf("Display = %q, want byte-identical input %q", outcome.Display, finalText)
	}
}

// TestFinalMessageGuardPropagatesContext pins that the guard hands the
// caller's context to the regeneration call.
func TestFinalMessageGuardPropagatesContext(t *testing.T) {
	type ctxKey struct{}
	want := context.WithValue(context.Background(), ctxKey{}, "marker")
	FinalMessageGuard(want, fmEnglishProse, fmSpanish, func(got context.Context) (string, error) {
		if got.Value(ctxKey{}) != "marker" {
			t.Error("the regenerator did not receive the guard's context")
		}
		return fmSpanishProse, nil
	})
}

// TestLanguageMismatchNoticeInUserLanguage checks the notice is localized
// for a couple of user languages (it names the expected language in the
// user's own tongue).
func TestLanguageMismatchNoticeInUserLanguage(t *testing.T) {
	cases := []struct {
		code     string
		contains string
	}{
		{"en", "English"},
		{"es", "español"},
	}
	for _, tc := range cases {
		notice := LanguageMismatchNotice(langguard.Language{Code: tc.code})
		if !strings.Contains(notice, tc.contains) {
			t.Errorf("%s notice %q does not name %q", tc.code, notice, tc.contains)
		}
	}
}

// TestLanguageMismatchNoticeUnknownCodeFallsBackToEnglish pins the
// English fallback for codes without a dedicated template (and for the
// zero language).
func TestLanguageMismatchNoticeUnknownCodeFallsBackToEnglish(t *testing.T) {
	english := languageNotices["en"]
	for _, code := range []string{"", "ko", "nl", "sv"} {
		if got := LanguageMismatchNotice(langguard.Language{Code: code}); got != english {
			t.Errorf("code %q: got %q, want the English fallback", code, got)
		}
	}
}

// TestLanguageMismatchNoticeCoversCoreLanguages pins that the core set
// (en, es, fr, de, pt, it, ru, zh, ja, ar, hi) each has a dedicated
// template rather than falling back to English.
func TestLanguageMismatchNoticeCoversCoreLanguages(t *testing.T) {
	english := languageNotices["en"]
	for _, code := range []string{"en", "es", "fr", "de", "pt", "it", "ru", "zh", "ja", "ar", "hi"} {
		got := LanguageMismatchNotice(langguard.Language{Code: code})
		if code != "en" && got == english {
			t.Errorf("code %q has no dedicated notice template (fell back to English)", code)
		}
	}
}

// TestLanguageMismatchNoticeKeysAreCaseInsensitive pins that the notice
// lookup tolerates the mixed-case codes the configured-language parser
// may carry.
func TestLanguageMismatchNoticeKeysAreCaseInsensitive(t *testing.T) {
	lower := LanguageMismatchNotice(langguard.Language{Code: "es"})
	upper := LanguageMismatchNotice(langguard.Language{Code: "ES"})
	if lower != upper {
		t.Errorf("ES notice %q differs from es notice %q", upper, lower)
	}
}
