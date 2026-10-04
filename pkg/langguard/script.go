// The Unicode script pass (SP-152 §152d): identify the dominant writing
// script of a prose string using the standard library's unicode range
// tables. This is the cheap, exact first half of the guard: it catches
// the common failure mode — a reply that is entirely in the wrong script
// (e.g. Chinese in a Spanish conversation). Languages that share a
// script (Spanish vs English) are not distinguishable here by design;
// that is the same-script detector's job (SP-152 152.2).
package langguard

import (
	"unicode"
)

// Script is a writing script in the Unicode sense. The script pass
// compares scripts, not languages.
type Script int

// The scripts the guard recognizes: the writing scripts of the major
// world languages. Text written in any other script (Bengali, Tamil,
// Georgian, …) is counted as an untracked letter by DominantScript and
// therefore suppresses reliability instead of being misassigned.
const (
	// ScriptUnknown means "no script" or "no reliable script".
	ScriptUnknown Script = iota
	// ScriptLatin covers the Latin script, including the Latin-1 and
	// extended blocks (accented letters and ligatures) — the script of
	// English, Spanish, French, Portuguese, German, Polish, and so on.
	ScriptLatin
	// ScriptCyrillic — Russian, Ukrainian, Bulgarian, and so on.
	ScriptCyrillic
	// ScriptGreek.
	ScriptGreek
	// ScriptArabic.
	ScriptArabic
	// ScriptHebrew.
	ScriptHebrew
	// ScriptDevanagari — Hindi, Marathi, and so on.
	ScriptDevanagari
	// ScriptThai.
	ScriptThai
	// ScriptHangul — Korean.
	ScriptHangul
	// ScriptHiragana and ScriptKatakana are the two Japanese kana
	// scripts; together they cover the kana of Japanese text (Japanese
	// also uses Han, see ScriptHan).
	ScriptHiragana
	ScriptKatakana
	// ScriptHan — CJK Unified Ideographs (Chinese characters). Japanese
	// and Korean text contains Han as well, but a text dominated by Han
	// alone is overwhelmingly Chinese.
	ScriptHan
)

// MinScriptRunes is the minimum number of runes of the dominant script
// for DominantScript to call its result reliable. It is tuned together
// with the detector tests of SP-152 152.2.
const MinScriptRunes = 8

// scriptDef pairs a script with its Unicode range table.
type scriptDef struct {
	script Script
	table  *unicode.RangeTable
}

// scriptDefs lists the recognized scripts in a fixed, deterministic
// order. The tables are the per-script unicode.RangeTable values of the
// standard library's unicode.Scripts map — the Unicode script partition,
// so every code point belongs to at most one of them.
var scriptDefs = []scriptDef{
	{ScriptLatin, unicode.Scripts["Latin"]},
	{ScriptCyrillic, unicode.Scripts["Cyrillic"]},
	{ScriptGreek, unicode.Scripts["Greek"]},
	{ScriptArabic, unicode.Scripts["Arabic"]},
	{ScriptHebrew, unicode.Scripts["Hebrew"]},
	{ScriptDevanagari, unicode.Scripts["Devanagari"]},
	{ScriptThai, unicode.Scripts["Thai"]},
	{ScriptHangul, unicode.Scripts["Hangul"]},
	{ScriptHiragana, unicode.Scripts["Hiragana"]},
	{ScriptKatakana, unicode.Scripts["Katakana"]},
	{ScriptHan, unicode.Scripts["Han"]},
}

// scriptNames maps each script to its diagnostic name; SP-152 152.9 logs
// mismatches with a model ID and role, and this is the readable part.
var scriptNames = map[Script]string{
	ScriptUnknown:    "unknown",
	ScriptLatin:      "latin",
	ScriptCyrillic:   "cyrillic",
	ScriptGreek:      "greek",
	ScriptArabic:     "arabic",
	ScriptHebrew:     "hebrew",
	ScriptDevanagari: "devanagari",
	ScriptThai:       "thai",
	ScriptHangul:     "hangul",
	ScriptHiragana:   "hiragana",
	ScriptKatakana:   "katakana",
	ScriptHan:        "han",
}

// String renders the script name for diagnostics.
func (s Script) String() string {
	if name, ok := scriptNames[s]; ok {
		return name
	}
	return "unknown"
}

// IsJapanese reports whether s is one of the Japanese kana scripts
// (hiragana or katakana).
func (s Script) IsJapanese() bool {
	return s == ScriptHiragana || s == ScriptKatakana
}

// DominantScript identifies the dominant writing script of prose (§152d).
// It returns the dominant script, the number of runes of that script,
// and whether the result is reliable.
//
// A result is reliable only when the dominant script accounts for at
// least MinScriptRunes runes AND a strict majority of all classified
// letters — letters of the recognized scripts plus letters of untracked
// ones. A text split between two scripts (or mostly an untracked script)
// is therefore not reliable, so the caller should treat the message as
// undetermined rather than guess.
func DominantScript(prose string) (Script, int, bool) {
	counts := make(map[Script]int, len(scriptDefs))
	total := 0
	for _, r := range prose {
		if s, ok := scriptOf(r); ok {
			counts[s]++
			total++
			continue
		}
		if unicode.IsLetter(r) {
			// A letter of an untracked script: it counts against
			// dominance but is not assigned to any recognized script.
			total++
		}
	}

	var dominant Script
	best := 0
	for _, d := range scriptDefs {
		if n := counts[d.script]; n > best {
			best, dominant = n, d.script
		}
	}
	if best == 0 {
		return ScriptUnknown, 0, false
	}
	reliable := best >= MinScriptRunes && best*2 > total
	return dominant, best, reliable
}

// scriptOf reports which recognized script a rune belongs to.
func scriptOf(r rune) (Script, bool) {
	for _, d := range scriptDefs {
		if unicode.Is(d.table, r) {
			return d.script, true
		}
	}
	return ScriptUnknown, false
}
