package langguard

import (
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
)

// letterCount counts the Unicode letters in s. For pure-script fixtures
// this is an independent cross-check on the script tables: every letter
// of such a fixture belongs to exactly the fixture's script, so the
// expected dominant count equals the fixture's letter count.
func letterCount(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n
}

// allScripts is every defined script, in canonical order.
var allScripts = []Script{
	ScriptUnknown, ScriptLatin, ScriptCyrillic, ScriptGreek, ScriptArabic,
	ScriptHebrew, ScriptDevanagari, ScriptThai, ScriptHangul,
	ScriptHiragana, ScriptKatakana, ScriptHan,
}

// TestDominantScript is the table test over the Unicode script pass
// (SP-152 §152d): the correct script for prose across a broad sample of
// languages and scripts, dominant-script reporting for mixed text, and
// reliability ("not enough data") for short or split text.
func TestDominantScript(t *testing.T) {
	cases := []struct {
		name      string
		prose     string
		want      Script
		wantCount int
		wantOK    bool
		// crossCheck pins wantCount to the fixture's Unicode letter
		// count; only valid for pure-script prose.
		crossCheck bool
	}{
		{
			name: "Spanish is Latin", prose: "El paquete está listo para compilar",
			want: ScriptLatin, wantCount: 30, wantOK: true, crossCheck: true,
		},
		{
			name: "English is Latin", prose: "The package is ready to compile now",
			want: ScriptLatin, wantCount: 29, wantOK: true, crossCheck: true,
		},
		{
			name: "French with accents is Latin", prose: "Le paquet est prêt à compiler maintenant",
			want: ScriptLatin, wantCount: 34, wantOK: true, crossCheck: true,
		},
		{
			name: "Russian is Cyrillic", prose: "Пакет готов к компиляции сейчас",
			want: ScriptCyrillic, wantCount: 27, wantOK: true, crossCheck: true,
		},
		{
			name: "Greek is Greek", prose: "Το πακέτο είναι έτοιμο για σύνδεση τώρα",
			want: ScriptGreek, wantCount: 33, wantOK: true, crossCheck: true,
		},
		{
			name: "Chinese is Han", prose: "这个包已经准备好可以编译了",
			want: ScriptHan, wantCount: 13, wantOK: true, crossCheck: true,
		},
		{
			name: "Arabic is Arabic", prose: "الحزمة جاهزة للترجمة الآن",
			want: ScriptArabic, wantCount: 22, wantOK: true, crossCheck: true,
		},
		{
			name: "Hebrew is Hebrew", prose: "החבילה מוכנה לתרגום עכשיו",
			want: ScriptHebrew, wantCount: 22, wantOK: true, crossCheck: true,
		},
		{
			// No crossCheck: Devanagari vowel signs (matras) are Unicode
			// marks, not letters, so the letter count undercounts the
			// script count (18 letters vs 27 script runes).
			name: "Hindi is Devanagari", prose: "पैकज कम्पाइल करने के लिए तैयार है",
			want: ScriptDevanagari, wantCount: 27, wantOK: true,
		},
		{
			// No crossCheck: Thai vowel and tone marks are Unicode marks,
			// not letters (27 letters vs 32 script runes).
			name: "Thai is Thai", prose: "แพ็กเกจพร้อมสำหรับการคอมไพล์แล้ว",
			want: ScriptThai, wantCount: 32, wantOK: true,
		},
		{
			name: "Korean is Hangul", prose: "패키지는 이제 컴파일을 준비했습니다",
			want: ScriptHangul, wantCount: 16, wantOK: true, crossCheck: true,
		},
		{
			name: "Japanese hiragana is Hiragana", prose: "こんにちは げんきですか",
			want: ScriptHiragana, wantCount: 11, wantOK: true, crossCheck: true,
		},
		{
			name: "Japanese katakana is Katakana", prose: "カタカナオンリノテキスト",
			want: ScriptKatakana, wantCount: 12, wantOK: true, crossCheck: true,
		},
		{
			name:  "mixed Latin and CJK reports the dominant script",
			prose: "The build failed: 构建失败了，请检查日志。",
			want:  ScriptLatin, wantCount: 14, wantOK: true,
		},
		{
			// 11 Latin and 11 Han letters: the tie is not a majority, so
			// the result is unreliable; the tie itself resolves to the
			// earlier script (deterministic order).
			name:  "an even Latin/Han split is not reliable",
			prose: "Failed build 中文回复应该出现在这里",
			want:  ScriptLatin, wantCount: 11, wantOK: false,
		},
		{
			// 9 katakana, 9 hiragana and 2 Han: no reliable dominant
			// script, so a real Japanese sentence is not judged by this
			// pass (the same-script detector of 152.2 is what would see
			// it as Japanese).
			name:  "mixed kana and kanji is not reliable",
			prose: "パッケージはコンパイルの準備ができています",
			want:  ScriptHiragana, wantCount: 9, wantOK: false,
		},
		{
			name: "very short prose is not reliable", prose: "Hola",
			want: ScriptLatin, wantCount: 4, wantOK: false,
		},
		{
			// Bengali is not a recognized script: its letters count as
			// untracked, so no script is assigned at all.
			name: "an untracked script is not assigned", prose: "এটি একটি বাংলা বাক্য",
			want: ScriptUnknown, wantCount: 0, wantOK: false,
		},
		{
			name: "digits carry no script", prose: "12345 67890",
			want: ScriptUnknown, wantCount: 0, wantOK: false,
		},
		{
			name: "empty prose", prose: "",
			want: ScriptUnknown, wantCount: 0, wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.crossCheck {
				// Independent pin: the expected count equals the number
				// of Unicode letters in the fixture.
				assert.Equal(t, letterCount(tc.prose), tc.wantCount,
					"fixture letter count drifted; update wantCount")
			}
			gotScript, gotCount, gotOK := DominantScript(tc.prose)
			assert.Equal(t, tc.want, gotScript, "dominant script")
			assert.Equal(t, tc.wantCount, gotCount, "dominant rune count")
			assert.Equal(t, tc.wantOK, gotOK, "reliability")
		})
	}
}

// TestScriptString pins the diagnostic names (SP-152 152.9 logs them).
func TestScriptString(t *testing.T) {
	seen := map[string]Script{}
	for _, s := range allScripts {
		name := s.String()
		assert.NotEmpty(t, name, "script %d has no name", int(s))
		if other, dup := seen[name]; dup {
			t.Errorf("script names are not unique: %q used by %d and %d", name, other, s)
		}
		seen[name] = s
	}
	assert.Equal(t, "unknown", ScriptUnknown.String())
	assert.Equal(t, "unknown", Script(999).String(), "out-of-range scripts fall back to unknown")
}

// TestIsJapanese pins the kana compatibility helper.
func TestIsJapanese(t *testing.T) {
	assert.True(t, ScriptHiragana.IsJapanese())
	assert.True(t, ScriptKatakana.IsJapanese())
	assert.False(t, ScriptHan.IsJapanese())
	assert.False(t, ScriptLatin.IsJapanese())
	assert.False(t, ScriptUnknown.IsJapanese())
}
