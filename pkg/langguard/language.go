// The same-script trigram pass (SP-152 §152d): identify the natural
// language of extracted prose with a small, deterministic trigram
// detector. The script pass (script.go) catches the "wrong script
// entirely" failure mode, but it cannot tell Spanish from English —
// two languages that share the Latin script. This pass discriminates
// within a script.
//
// The detector is github.com/abadojack/whatlanggo (v1.0.1): a pure-Go,
// stdlib-only trigram-frequency library (no cgo, no network, no model
// download), so it runs in the CLI process, in the sprout process of a
// hosted workspace, and compiled into the WASM browser build — the same
// three places §152d requires, and it stays callable from other Go
// programs embedding sprout.
//
// The intended pipeline (layered items build on this file):
//
//	prose := langguard.ExtractProse(modelReply)
//	lang, conf, reliable := langguard.DetectLanguage(prose)
//	if reliable && lang.Code != userLang.Code {
//	    // mismatch: regenerate once (152.5) or fall back to the
//	    // script pass; log with model ID and role (152.9).
//	}
//
// 152.3 (user-language resolution) derives the user's own language with
// the same DetectLanguage call over their recent messages; 152.5 (the
// final-message guard) uses CheckLanguage to compare the reply against
// it.
package langguard

import (
	"strings"

	whatlang "github.com/abadojack/whatlanggo"
)

// Language identifies the natural language the trigram pass reports.
// The zero value (empty Code) means "no known language".
type Language struct {
	// Code is the language's ISO 639-1 code ("en", "es", "ru", ...), or
	// — for the few languages without a 639-1 code — the ISO 639-3
	// code ("cmn", "pes", ...). It is the stable identity used to
	// compare two detection results (152.3's majority vote over the
	// user's recent messages).
	Code string
	// Name is the detector's human-readable language name ("English",
	// "Spanish", ...). It feeds diagnostics (152.9) and the explicit
	// regeneration instruction (152.5).
	Name string
}

// String renders the language for diagnostics: "es (Spanish)". A zero
// Language renders as "".
func (l Language) String() string {
	if l.Code == "" {
		return ""
	}
	if l.Name == "" {
		return l.Code
	}
	return l.Code + " (" + l.Name + ")"
}

// MinDetectConfidence is the minimum detector confidence for a
// DetectLanguage result to count as reliable. It matches whatlanggo's
// own reliable-confidence threshold (0.8) and its strict-greater
// semantics (its IsReliable: the rating "has to be succeeded"): at or
// below it, the trigram distance between the two closest candidate
// languages is too small to call the winner reliable.
const MinDetectConfidence = 0.8

// DetectLanguage identifies the natural language of already-extracted
// prose (§152d: for same-script languages, "a small trigram detector
// with no network or model download"). It wraps whatlanggo's trigram
// detector and returns:
//
//   - the detected language (ISO code + human name);
//   - the detector's confidence in [0, 1];
//   - whether the result is reliable enough to judge the reply: the
//     prose must be Judgable (too-short and code-only prose are never
//     judged, §152a) AND the confidence must reach MinDetectConfidence.
//
// Prose that is not judgable, carries no recognized script, or yields an
// unreliable confidence returns the zero Language and reliable=false —
// the caller treats the reply as undetermined (as the script pass does)
// instead of guessing.
func DetectLanguage(prose string) (Language, float64, bool) {
	if !Judgable(prose) {
		return Language{}, 0, false
	}
	info := whatlang.Detect(prose)
	if info.Lang < 0 {
		// No recognized script, or no candidate language: nothing to
		// report. The confidence is still surfaced (0 in practice).
		return Language{}, info.Confidence, false
	}
	code := info.Lang.Iso6391()
	if code == "" {
		// A few languages (Cebuano, Ilocano, Maithili, Persian, ...)
		// have no ISO 639-1 code; fall back to the 639-3 code so Code
		// stays a usable identity.
		code = info.Lang.Iso6393()
	}
	lang := Language{Code: code, Name: info.Lang.String()}
	return lang, info.Confidence, info.Confidence > MinDetectConfidence
}

// CheckLanguage is the trigram pass's raw-message entry point for the
// reply paths (152.5): it extracts the prose from a raw model reply and
// reports the combined verdict of the reply's language against the
// user's language (from 152.3's user-language resolution, or the
// configured language setting). It parallels the script pass's Check:
//
//	Check:   text + user's script  -> Verdict   (wrong script)
//	CheckLanguage: text + user's language -> Verdict (wrong language,
//		same script)
//
// A mismatch can only be asserted from a reliable detection: when the
// reply is too short, code-heavy, or the detector is unsure, the
// verdict is VerdictUndetermined and the script pass remains the
// fallback judgment. Language codes are compared case-insensitively,
// so a configured "ES" matches the detector's "es".
func CheckLanguage(text string, user Language) Verdict {
	lang, _, reliable := DetectLanguage(ExtractProse(text))
	if !reliable || user.Code == "" {
		return VerdictUndetermined
	}
	if strings.EqualFold(lang.Code, user.Code) {
		return VerdictPass
	}
	return VerdictMismatch
}
