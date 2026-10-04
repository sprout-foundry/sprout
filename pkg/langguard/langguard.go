// Package langguard is the detection foundation of the outbound language
// guard (SP-152, roadmap/SP-152-language-guard.md): it extracts prose from
// model messages, identifies the dominant writing script of that prose,
// and decides whether a message is long enough to judge at all.
//
// The guard's purpose (§152a) is to keep model-authored text from being
// displayed when it is not written in the user's language. This package
// provides the deterministic, in-process half of that check (§152d): no
// external service and no model call, pure Go with the standard library
// only (no cgo), so the same code runs in the CLI process, in the sprout
// process of a hosted workspace, and compiled into the WASM browser
// build — and can be called by other Go programs embedding sprout.
//
// The intended pipeline, which later SP-152 items build on this package:
//
//	prose := langguard.ExtractProse(modelReply)
//	if langguard.Judgable(prose) {
//	    if v := langguard.CheckScript(prose, userScript); v == langguard.VerdictMismatch {
//	        // same-script detector (152.2) or regeneration (152.5)
//	    }
//	}
//
// The same-script trigram detector (152.2, language.go) and user-language
// resolution (152.3, resolve.go) build on this package's prose and script
// primitives. Deliberately not included here: reply-path wiring and
// streaming hold-back (152.5–152.7), and mismatch metrics (152.9). They
// layer on top of this package's API; the API is shaped for them (Check
// and CheckLanguage take the user's script and language as parameters,
// ResolveUserLanguage returns the resolved user language, String methods
// exist for logging, and the thresholds are tunable constants).
package langguard

import (
	"unicode/utf8"
)

// MinJudgedProseRunes is the minimum length, in runes, of the extracted
// prose below which a message is not judged at all (§152a: "Messages
// below a length threshold are not judged (detection is unreliable on a
// few words)"). The threshold applies to the extracted prose — code, URLs
// and paths removed — not to the raw message, so a reply that is long
// only because it contains code is still not judged.
//
// 32 runes is roughly five to seven words of Latin prose or one full
// sentence of CJK. It will be tuned from the detector tests of SP-152
// 152.2; change it here.
const MinJudgedProseRunes = 32

// Judgable reports whether the extracted prose of a message is long
// enough for language detection to be reliable.
func Judgable(prose string) bool {
	return utf8.RuneCountInString(prose) >= MinJudgedProseRunes
}

// Verdict is the outcome of the script pass over one model message.
type Verdict int

// The script-pass verdicts.
const (
	// VerdictPass: the reply's dominant script is compatible with the
	// user's script (equal scripts, or the two Japanese kana scripts).
	VerdictPass Verdict = iota
	// VerdictMismatch: the reply is reliably written in a different
	// script than the user's — the "wrong script entirely" failure mode
	// the script pass exists to catch (§152d).
	VerdictMismatch
	// VerdictUndetermined: the message is too short to judge, has no
	// reliable dominant script, or the user's script is unknown. The
	// script pass neither passes nor fails it; a same-script detector
	// (SP-152 152.2) may still judge it.
	VerdictUndetermined
)

// String renders the verdict for diagnostics and logging (SP-152 152.9).
func (v Verdict) String() string {
	switch v {
	case VerdictPass:
		return "pass"
	case VerdictMismatch:
		return "mismatch"
	default:
		return "undetermined"
	}
}

// CheckScript runs the script pass (§152d) on the extracted prose of a
// model reply. userScript is the script the user writes in; SP-152 152.3
// (user-language resolution) will derive it — it can be built directly on
// this package's DominantScript. Pass ScriptUnknown when it could not be
// determined: the pass then reports VerdictUndetermined instead of
// guessing.
//
// The check only judges messages whose extracted prose is Judgable and
// whose dominant script is reliable; everything else is
// VerdictUndetermined.
func CheckScript(replyProse string, userScript Script) Verdict {
	if !Judgable(replyProse) {
		return VerdictUndetermined
	}
	reply, _, reliable := DominantScript(replyProse)
	if !reliable {
		return VerdictUndetermined
	}
	if userScript == ScriptUnknown {
		return VerdictUndetermined
	}
	if scriptsCompatible(userScript, reply) {
		return VerdictPass
	}
	return VerdictMismatch
}

// Check is the intended single entry point for the reply paths (SP-152
// 152.5–152.7): it extracts the prose from a raw model reply and runs the
// script pass against the user's script.
func Check(text string, userScript Script) Verdict {
	return CheckScript(ExtractProse(text), userScript)
}

// scriptsCompatible reports whether two scripts can carry the same
// language: equal scripts, or the two Japanese kana scripts, which share
// a language. Without this, a Japanese reply whose dominant script
// happens to be katakana would be a mismatch for a user who wrote in
// hiragana (or vice versa).
func scriptsCompatible(a, b Script) bool {
	if a == b {
		return true
	}
	return a.IsJapanese() && b.IsJapanese()
}
