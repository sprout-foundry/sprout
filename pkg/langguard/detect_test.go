package langguard

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Trigram-pass fixtures. Every prose fixture is above
// MinJudgedProseRunes so DetectLanguage actually judges it, and long
// enough that the detector's confidence clears MinDetectConfidence.
var (
	englishProse = "The build succeeded after applying the patch, so the tests can run and the release is ready to ship."
	spanishProse = "El paquete está listo para compilar ahora mismo y las pruebas pasan sin errores."
	frenchProse  = "Le paquet est prêt à compiler maintenant et les tests se déroulent sans erreur."
	germanProse  = "Das Paket ist bereit für die Kompilierung und die Tests laufen ohne Fehler durch."
	russianProse = "Пакет готов к компиляции, и теперь можно запускать тесты без ошибок."
	// Chinese (Han script): a single Han-dominated sentence.
	chineseProse = "这个包已经准备好可以编译了，现在可以运行测试程序来验证结果是否正确。"
	// Japanese: mixed kana and kanji, as real Japanese prose is.
	japaneseProse = "パッケージはすでにコンパイルの準備ができています。テストを実行して結果を確認してください。"
	// Japanese katakana-only, so the script pass also judges it
	// reliably (used by the cross-pass agreement table).
	japaneseKanaProse = "コンピュウタノプログラムハカンジョウニオワッタゴトニシマシタデスネ"
	arabicProse       = "الحزمة جاهزة للترجمة الآن ويمكن تشغيل الاختبارات دون أي أخطاء."
	italianProse      = "Il pacchetto è pronto per la compilazione e i test vengono eseguiti senza errori."
	portugueseProse   = "O pacote está pronto para compilação e os testes são executados sem erros."
	greekProse        = "Το πακέτο είναι έτοιμο για μεταγραφή και οι δοκιμές εκτελούνται χωρίς σφάλματα."
	hindiProse        = "पैकेज कम्पाइल करने के लिए तैयार है और परीक्षण बिना किसी त्रुटि के चलेंगे।"
	turkishProse      = "Paket derleme için hazır ve testler hatasız çalışıyor, artık paylaşılabilir."
	koreanProse       = "패키지는 이제 컴파일을 위한 준비가 완료되었고 테스트는 오류 없이 실행됩니다."
	thaiProse         = "แพ็กเกจพร้อมสำหรับการคอมไพล์แล้ว และจะรันการทดสอบโดยไม่ต้องมีข้อผิดพลาด"
	polishProse       = "Pakiet jest gotowy do kompilacji, a testy uruchamiają się bez błędów."
)

// TestDetectLanguage is the table test over the trigram pass:
// the correct language for prose across a broad sample of
// languages and scripts, dominant-language reporting for mixed text,
// and "not reliable" for short, code-only or undetectable input.
func TestDetectLanguage(t *testing.T) {
	cases := []struct {
		name    string
		prose   string
		want    Language
		wantRel bool
	}{
		{"English (Latin)", englishProse, Language{Code: "en", Name: "English"}, true},
		{"Spanish (Latin)", spanishProse, Language{Code: "es", Name: "Spanish"}, true},
		{"French (Latin)", frenchProse, Language{Code: "fr", Name: "French"}, true},
		{"German (Latin)", germanProse, Language{Code: "de", Name: "German"}, true},
		{"Russian (Cyrillic)", russianProse, Language{Code: "ru", Name: "Russian"}, true},
		{"Chinese (Han)", chineseProse, Language{Code: "zh", Name: "Mandarin"}, true},
		{"Japanese (mixed kana and kanji)", japaneseProse, Language{Code: "ja", Name: "Japanese"}, true},
		{"Japanese (katakana only)", japaneseKanaProse, Language{Code: "ja", Name: "Japanese"}, true},
		{"Arabic", arabicProse, Language{Code: "ar", Name: "Arabic"}, true},
		{"Italian (Latin)", italianProse, Language{Code: "it", Name: "Italian"}, true},
		{"Portuguese (Latin)", portugueseProse, Language{Code: "pt", Name: "Portuguese"}, true},
		{"Greek", greekProse, Language{Code: "el", Name: "Greek"}, true},
		{"Hindi (Devanagari)", hindiProse, Language{Code: "hi", Name: "Hindi"}, true},
		{"Turkish (Latin)", turkishProse, Language{Code: "tr", Name: "Turkish"}, true},
		{"Korean (Hangul)", koreanProse, Language{Code: "ko", Name: "Korean"}, true},
		{"Thai", thaiProse, Language{Code: "th", Name: "Thai"}, true},
		{"Polish (Latin)", polishProse, Language{Code: "pl", Name: "Polish"}, true},
		// A mixed-language message reports its dominant language: the
		// English part is five times the Spanish part.
		{
			name:  "mixed message reports the dominant language",
			prose: "El paquete está listo. " + englishProse,
			want:  Language{Code: "en", Name: "English"},
			// The English trigram signature dominates, so the detector
			// is confident even with the Spanish fragment present.
			wantRel: true,
		},
		// Reliability: below the length threshold the message is not
		// judged at all, whatever the detector would say.
		{"short prose is not judged", "Hola", Language{}, false},
		{
			name: "code-heavy message has too little prose",
			// 50 lines of Go plus "ok": the extracted prose is just "ok".
			prose: ExtractProse("```go\n" + strings.Repeat("x := 1;\n", 50) + "```\nok"),
			want:  Language{},
		},
		{"empty prose", "", Language{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, conf, reliable := DetectLanguage(tc.prose)
			assert.Equal(t, tc.want, got, "confidence: %v", conf)
			assert.Equal(t, tc.wantRel, reliable, "confidence: %v", conf)
			if reliable {
				assert.Greater(t, conf, MinDetectConfidence,
					"a reliable result must clear the confidence bar")
				assert.LessOrEqual(t, conf, 1.0)
			}
		})
	}
}

// TestDetectLanguageConfidence pins that the confidence is the
// detector's own value, in [0, 1], and that a reliable detection
// actually clears the bar.
func TestDetectLanguageConfidence(t *testing.T) {
	for _, prose := range []string{englishProse, russianProse, chineseProse} {
		_, conf, reliable := DetectLanguage(prose)
		assert.GreaterOrEqual(t, conf, 0.0, "prose: %q", prose)
		assert.LessOrEqual(t, conf, 1.0, "prose: %q", prose)
		if reliable {
			assert.Greater(t, conf, MinDetectConfidence)
		}
	}
}

// TestDetectLanguageAgreesWithScriptPass checks that the two passes
// agree where they overlap: for the same prose, the script pass
// reports the script of the detected language, so a mismatch verdict
// from either pass is a real mismatch.
func TestDetectLanguageAgreesWithScriptPass(t *testing.T) {
	cases := []struct {
		name       string
		prose      string
		wantScript Script
		wantLang   string
	}{
		{"English", englishProse, ScriptLatin, "en"},
		{"Spanish", spanishProse, ScriptLatin, "es"},
		{"French", frenchProse, ScriptLatin, "fr"},
		{"German", germanProse, ScriptLatin, "de"},
		{"Russian", russianProse, ScriptCyrillic, "ru"},
		{"Chinese", chineseProse, ScriptHan, "zh"},
		{"Japanese (katakana)", japaneseKanaProse, ScriptKatakana, "ja"},
		{"Arabic", arabicProse, ScriptArabic, "ar"},
		{"Hindi", hindiProse, ScriptDevanagari, "hi"},
		{"Greek", greekProse, ScriptGreek, "el"},
		{"Korean", koreanProse, ScriptHangul, "ko"},
		{"Thai", thaiProse, ScriptThai, "th"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			script, _, scriptOK := DominantScript(tc.prose)
			lang, _, langOK := DetectLanguage(tc.prose)
			assert.True(t, scriptOK, "script pass must judge: %q", tc.prose)
			assert.True(t, langOK, "trigram pass must judge: %q", tc.prose)
			assert.Equal(t, tc.wantScript, script)
			assert.Equal(t, tc.wantLang, lang.Code)
		})
	}
}

// TestCheckLanguage covers the trigram pass's raw-message entry point:
// code, URLs and quoted spans are stripped first, the verdict
// follows reliability, and codes compare case-insensitively.
func TestCheckLanguage(t *testing.T) {
	esUser := Language{Code: "es", Name: "Spanish"}
	enUser := Language{Code: "en", Name: "English"}

	rawSpanish := "Hecho. " + spanishProse + "\n\n```go\nfmt.Println(1)\n```\n\nSee https://example.com."
	rawEnglish := "Done. " + englishProse + "\n\n```go\nfmt.Println(1)\n```"

	cases := []struct {
		name string
		text string
		user Language
		want Verdict
	}{
		{"Spanish reply, Spanish user", rawSpanish, esUser, VerdictPass},
		{"English reply, Spanish user", rawEnglish, esUser, VerdictMismatch},
		{"Spanish reply, English user", rawSpanish, enUser, VerdictMismatch},
		{"English reply, English user", rawEnglish, enUser, VerdictPass},
		// "OK done" is below the length threshold: not judged.
		{"short reply is undetermined", "OK done", esUser, VerdictUndetermined},
		// Code plus a few words: the extracted prose is not judgable.
		{
			name: "code-heavy reply is undetermined",
			text: "```go\n" + strings.Repeat("x := 1;\n", 50) + "```\ndone",
			user: esUser,
			want: VerdictUndetermined,
		},
		{"no user language is undetermined", rawSpanish, Language{}, VerdictUndetermined},
		// A configured language setting may use upper case codes.
		{
			name: "codes compare case-insensitively",
			text: rawSpanish,
			user: Language{Code: "ES"},
			want: VerdictPass,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, CheckLanguage(tc.text, tc.user), "text: %q", tc.text)
		})
	}
}

// TestLanguageString pins the diagnostic rendering, which logs
// mismatches with the language involved.
func TestLanguageString(t *testing.T) {
	assert.Equal(t, "es (Spanish)", Language{Code: "es", Name: "Spanish"}.String())
	assert.Equal(t, "en", Language{Code: "en"}.String(), "nameless codes render bare")
	assert.Equal(t, "", Language{}.String(), "the zero language renders empty")
}

// TestDetectLanguageIsTotal checks the detector on inputs that must
// not panic: a judgable run of digits (no script letters), a lone
// character, and a long input.
func TestDetectLanguageIsTotal(t *testing.T) {
	// Digits carry no script, so a judgable run of them must stay
	// unknown: no language can be reported reliably there.
	digits := strings.Repeat("12345 ", 10)
	lang, _, reliable := DetectLanguage(digits)
	assert.False(t, reliable, "input: %q", digits)
	assert.Equal(t, Language{}, lang)

	// The remaining inputs only pin non-panic behavior (a long
	// repetition can yield a confident, if odd, detection).
	inputs := []string{"…", "«", strings.Repeat("word ", 1000)}
	for i, in := range inputs {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			_, _, _ = DetectLanguage(in)
		})
	}
}
