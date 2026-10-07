package langguard

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// spanishReply is a reliably Latin reply: 45 Latin letters, well above
// both the Judgable and the script-reliability thresholds.
const spanishReply = "El paquete está listo para compilar ahora mismo."

// TestJudgable covers the minimum-length threshold: the
// threshold applies to the extracted prose, not the raw message.
func TestJudgable(t *testing.T) {
	codeOnly := "```go\n" + strings.Repeat("x := 1;\n", 50) + "```"

	cases := []struct {
		name  string
		prose string
		want  bool
	}{
		{"one rune below the threshold", strings.Repeat("a", MinJudgedProseRunes-1), false},
		{"at the threshold", strings.Repeat("a", MinJudgedProseRunes), true},
		{"long prose", "El paquete está listo para compilar ahora mismo.", true},
		{"short prose", "Hola", false},
		{"code-heavy message has too little prose", ExtractProse(codeOnly + " ok"), false},
		{"empty prose", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Judgable(tc.prose), "prose: %q", tc.prose)
		})
	}
}

// TestCheckScript covers the script pass: the "wrong
// script entirely" case, and every situation in which the pass must stay
// quiet (too short, no reliable dominant script, unknown user script).
func TestCheckScript(t *testing.T) {
	cases := []struct {
		name       string
		replyProse string
		userScript Script
		want       Verdict
	}{
		{"Latin reply, Latin user", spanishReply, ScriptLatin, VerdictPass},
		{"Latin reply, Cyrillic user", spanishReply, ScriptCyrillic, VerdictMismatch},
		{"Latin reply, Han user", spanishReply, ScriptHan, VerdictMismatch},
		{"Han reply, Latin user", "这个包已经准备好可以编译了，请重新构建一下项目，然后运行测试程序验证结果", ScriptLatin, VerdictMismatch},
		{"short reply is not judged", "OK done", ScriptLatin, VerdictUndetermined},
		{"unknown user script is not judged", spanishReply, ScriptUnknown, VerdictUndetermined},
		{"untracked-script reply is not judged", "এটি একটি বাংলা বাক্য যা অনেক দীর্ঘ হতে হবে", ScriptLatin, VerdictUndetermined},
		// CJK fixtures must be at least MinJudgedProseRunes runes long to
		// be judgable: each character already carries a full word of
		// meaning, so the prose needs to be short in Latin terms.
		{"katakana reply, hiragana user (same language)", "コンピュウタヲヒラテテストヲジッコウシマシタ。パッケージハレディ", ScriptHiragana, VerdictPass},
		{"hiragana reply, katakana user (same language)", "これはひらがなのテキストです。ほんとうにわかりませんけど、てすともおこないます", ScriptKatakana, VerdictPass},
		{"mixed kana reply is not judged", "パッケージはコンパイルの準備ができています", ScriptHiragana, VerdictUndetermined},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, CheckScript(tc.replyProse, tc.userScript),
				"reply: %q", tc.replyProse)
		})
	}
}

// TestCheck covers the raw-message entry point: code, URLs and paths are
// stripped before the script pass runs.
func TestCheck(t *testing.T) {
	raw := "Hecho. " + spanishReply + "\n\n```go\nfmt.Println(1)\n```\n\nSee https://example.com."
	assert.Equal(t, VerdictPass, Check(raw, ScriptLatin), "raw: %q", raw)
	assert.Equal(t, VerdictMismatch, Check(raw, ScriptHan), "raw: %q", raw)
	// A reply that is code plus a few words is not judged at all.
	codeHeavy := "```go\n" + strings.Repeat("x := 1;\n", 50) + "```\ndone"
	assert.Equal(t, VerdictUndetermined, Check(codeHeavy, ScriptLatin), "raw: %q", codeHeavy)
}

// TestVerdictString pins the diagnostic rendering.
func TestVerdictString(t *testing.T) {
	assert.Equal(t, "pass", VerdictPass.String())
	assert.Equal(t, "mismatch", VerdictMismatch.String())
	assert.Equal(t, "undetermined", VerdictUndetermined.String())
	assert.Equal(t, "undetermined", Verdict(99).String())
}
