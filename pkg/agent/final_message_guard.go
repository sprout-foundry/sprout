// final_message_guard.go — the final-message language guard (SP-152
// §152b). The final (completed) assistant message of a turn is judged
// against the user's language (152.3's user-language resolution plus the
// configured language fallback); on a mismatch the message is not
// displayed: it is regenerated ONCE with an explicit instruction to
// answer in the user's language, and if the regeneration still mismatches
// (or fails) a short templated notice in the user's language is shown
// with the option to view the original. Images, screenshots and rendered
// previews pass through unchanged — the judgment is over extracted prose
// only (ExtractProse), so non-prose content is never affected.
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/langguard"
)

// GuardOutcome is the result of running the final assistant message
// through the final-message guard (§152b).
type GuardOutcome struct {
	// Display is what the user sees: the final text (no mismatch, or a
	// successful regeneration) or the localized templated notice (a
	// mismatch the single regeneration could not repair).
	Display string
	// Original is the mismatched message — the "view original" payload.
	// "" when there was no mismatch.
	Original string
	// Regenerated reports the regeneration's role in the outcome: true on
	// the display path (the displayed text came from the regeneration
	// call); on the notice path, true only when the regeneration produced
	// text that still failed the check, false when it errored or
	// produced nothing.
	Regenerated bool
	// Mismatched reports whether a language mismatch was detected in the
	// final message.
	Mismatched bool
}

// Regenerator performs the single regeneration model call (§152b:
// "regenerate once"). Implementations issue exactly one model call — a
// focused prompt, not a re-run of the tool loop. The guard calls it at
// most once, never in a retry loop.
type Regenerator func(ctx context.Context) (string, error)

// FinalMessageGuard checks finalText against the user's language and
// repairs a mismatch exactly once (§152b). Behavior matrix:
//
//   - user language undetermined (user.Code == ""): Display is finalText
//     unchanged (byte-identical passthrough), no mismatch, regenerate is
//     not called.
//   - CheckLanguage(finalText, user) is pass or undetermined (too short,
//     code-heavy, detector unsure): Display is finalText unchanged, no
//     mismatch, regenerate is not called.
//   - mismatch: regenerate is called EXACTLY ONCE and its result is
//     re-checked:
//   - regenerated text that is not a mismatch is displayed
//     (Regenerated=true, Original=finalText);
//   - a regenerated text that is still a mismatch, or a regeneration
//     error / empty result, shows the templated notice in the user's
//     language (Mismatched=true, Original=finalText, Regenerated only
//     when the regeneration produced text).
//
// An empty regeneration result counts as a failed regeneration — the
// notice is shown, never a blank display.
func FinalMessageGuard(ctx context.Context, finalText string, user langguard.Language, regenerate Regenerator) GuardOutcome {
	if user.Code == "" || langguard.CheckLanguage(finalText, user) != langguard.VerdictMismatch {
		// Nothing to judge, or the message passes / is undetermined:
		// byte-identical passthrough, no regeneration call.
		return GuardOutcome{Display: finalText}
	}
	if regenerate == nil {
		return GuardOutcome{
			Display:    LanguageMismatchNotice(user),
			Original:   finalText,
			Mismatched: true,
		}
	}
	regenerated, err := regenerate(ctx)
	produced := err == nil && strings.TrimSpace(regenerated) != ""
	if produced && langguard.CheckLanguage(regenerated, user) != langguard.VerdictMismatch {
		return GuardOutcome{
			Display:     regenerated,
			Original:    finalText,
			Regenerated: true,
			Mismatched:  true,
		}
	}
	return GuardOutcome{
		Display:     LanguageMismatchNotice(user),
		Original:    finalText,
		Regenerated: produced,
		Mismatched:  true,
	}
}

// languageNotices are the §152b templated notices, keyed by the resolved
// user language's ISO code: a short sentence saying the reply came back
// in the wrong language, plus the "view original" affordance, in the
// user's own language. Each template carries the user language's own
// self-name ("es" -> "español"): naming the language the reply actually
// came back in would need a translation of every detector language into
// every supported user language, which is not maintainable — the notice
// names the expected language instead. The English entry is the fallback
// for any code without a dedicated entry.
//
// This is a UI-string localization table only. The detector's "no fixed
// language list" constraint (§152a) governs language detection, not
// these strings: any language the detector recognizes remains a valid
// target, and a code without a template simply falls back to English.
var languageNotices = map[string]string{
	"en": "The reply came back in a different language instead of English. You can view the original, or ask me to try again.",
	"es": "La respuesta llegó en un idioma diferente en lugar de en español. Puedes ver el texto original o pedirme que lo intente de nuevo.",
	"fr": "La réponse est revenue dans une langue différente au lieu du français. Vous pouvez consulter le texte original ou demander une nouvelle tentative.",
	"de": "Die Antwort kam in einer anderen Sprache statt auf Deutsch zurück. Sie können den Originaltext ansehen oder mich bitten, es erneut zu versuchen.",
	"pt": "A resposta voltou em um idioma diferente em vez de português. Você pode ver o texto original ou pedir para tentar novamente.",
	"it": "La risposta è arrivata in una lingua diversa dall'italiano. Puoi visualizzare il testo originale o chiedere di riprovare.",
	"ru": "Ответ пришёл на другом языке, а не на русском. Вы можете просмотреть оригинальный текст или попросить меня повторить.",
	"zh": "回复不是用中文写成的，而是用了另一种语言。您可以查看原始文本，或要求我重新回答。",
	"ja": "返信は別の言語で返され、日本語ではなかった。元のテキストを表示するか、もう一度やり直していただくこともできます。",
	"ar": "وصل الرد بلغة مختلفة بدلاً من العربية. يمكنك عرض النص الأصلي أو طلب الإجابة مرة أخرى.",
	"hi": "जवाब हिंदी के बजाय किसी अन्य भाषा में आया है। आप मूल पाठ देख सकते हैं या फिर से कोशिश करने का अनुरोध कर सकते हैं।",
}

// LanguageMismatchNotice returns the templated §152b notice in the user's
// language (the table's English entry for any code without a dedicated
// entry, including the zero language).
func LanguageMismatchNotice(user langguard.Language) string {
	if s, ok := languageNotices[strings.ToLower(user.Code)]; ok {
		return s
	}
	return languageNotices["en"]
}

// ---------------------------------------------------------------------------
// Agent wiring
// ---------------------------------------------------------------------------

// languageGuardRecentMessages is how many of the user's recent messages
// the guard feeds to ResolveUserLanguage (§152a's "majority over recent
// turns").
const languageGuardRecentMessages = 10

// langGuardOriginalMetaKey is where the mismatched original (the "view
// original" payload, §152b) is kept on the replaced assistant message.
const langGuardOriginalMetaKey = "language_guard_original"

// langGuardRepairedMetaKey marks an assistant message the streaming guard
// already repaired during the stream (a held reply or a mid-stream switch):
// its content is the delivered replacement, and its mismatch is already
// recorded. The final-message guard skips such messages — re-running on them
// would regenerate a second time for the same reply (or re-publish a
// duplicate replacement event).
const langGuardRepairedMetaKey = "language_guard_repaired"

// LastLanguageGuardOriginal returns the most recent "view original" payload
// the language guard stored (langGuardOriginalMetaKey on an assistant
// message), scanning the conversation from the end. It returns "" when no
// message carries one. Exported so the CLI's /original command reads the
// same key the guard writes — the string literal lives here and in the
// write sites above, nowhere else.
func LastLanguageGuardOriginal(messages []api.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "assistant" {
			continue
		}
		if original := messages[i].Meta[langGuardOriginalMetaKey]; original != "" {
			return original
		}
	}
	return ""
}

// applyLanguageGuard runs the turn's final assistant message through the
// final-message guard (§152b) and, on a mismatch outcome, replaces the
// last assistant message in state with the display text. It returns the
// display text as the turn's result: equal to result whenever the guard
// does not fire (subagent, guard disabled, undetermined user language,
// or no mismatch), so those paths are byte-for-byte unchanged.
func (a *Agent) applyLanguageGuard(qc *queryRunContext, result string) string {
	if a.IsSubagent() {
		// Subagent output goes to the orchestrator, not the end user; the
		// user-facing notice flow never applies to it.
		return result
	}

	cfg := a.GetConfig()
	// LanguageGuardEnabled is nil-safe: a nil config resolves to the
	// default (enabled, §152f).
	if !cfg.LanguageGuardEnabled() {
		return result
	}

	configured := langguard.Language{}
	if cfg != nil {
		configured = langguard.ParseLanguage(cfg.Language)
	}

	messages := a.state.GetMessages()
	replaceIndex := -1
	finalText := ""
	var finalMessage api.Message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			if messages[i].Content != "" {
				finalText = messages[i].Content
				finalMessage = messages[i]
				replaceIndex = i
			}
			break
		}
	}
	if finalText == "" {
		// No assistant message to judge (or an empty one): nothing to
		// guard and nothing in state to replace.
		return result
	}
	// The streaming guard already repaired this reply during the stream (a
	// held reply or a mid-stream switch): its content IS the repair (the
	// regenerated text or the notice) the client received, and its mismatch
	// is already recorded. Re-judging it would regenerate a second time for
	// the same reply — or re-publish a duplicate replacement event. What the
	// user sees and what state carries already agree.
	if messageRepairedByLanguageGuard(finalMessage) {
		return result
	}

	user, determined := langguard.ResolveUserLanguage(
		a.recentUserMessages(messages),
		configured,
	)
	if !determined {
		return result
	}

	outcome := FinalMessageGuard(qc.runCtx, finalText, user,
		func(ctx context.Context) (string, error) {
			return a.regenerateInUserLanguage(ctx, qc, finalText, user)
		})

	// SP-152 §152e (item 152.9): record the judgment in the per-model
	// language-guard metric — a check always, a mismatch when one was
	// detected — and log the mismatch with the model ID. This is the
	// canonical recording point: applyLanguageGuard runs on every
	// successful turn (buffered, held-stream and streamed-rechecked alike),
	// so each mismatch is counted exactly once here. The role dimension
	// (SP-150 §150c, item 150.5) buckets the check under the agent's role
	// (the guard only runs on the primary agent, so this is the coder role
	// in practice) so the metric keys by (model, role).
	modelID := a.GetModel()
	GlobalLanguageGuardMetrics().Record(modelID, a.GetRole(), outcome.Mismatched)
	if outcome.Mismatched {
		a.Logger().Info("[langguard] final-message mismatch: model=%s user=%s regenerated=%v",
			modelID, user.Code, outcome.Regenerated)
	}

	if !outcome.Mismatched {
		return result
	}

	updated := make([]api.Message, len(messages))
	copy(updated, messages)
	updated[replaceIndex].Content = outcome.Display
	updated[replaceIndex].SetMeta(langGuardOriginalMetaKey, outcome.Original)
	a.state.SetMessages(updated)

	if a.debug {
		a.Logger().Debug("[langguard] final message mismatch (%s): notice=%v, regenerated=%v\n",
			user, outcome.Display == LanguageMismatchNotice(user), outcome.Regenerated)
	}
	return outcome.Display
}

// messageRepairedByLanguageGuard reports whether msg carries the streaming
// guard's repaired marker (langGuardRepairedMetaKey).
func messageRepairedByLanguageGuard(msg api.Message) bool {
	return msg.Meta != nil && msg.Meta[langGuardRepairedMetaKey] != ""
}

// recentUserMessages collects the user's recent messages (capped at
// languageGuardRecentMessages, newest first) in chronological order for
// ResolveUserLanguage's majority vote. Timestamp envelopes are stripped:
// the envelope is a machine stamp, not user writing, and its ASCII would
// dilute the language signal of short messages. Verification repair
// reports are skipped the same way: they are machine-injected user-role
// messages the turn-end hook feeds back to the model, not the user's own
// writing, so they never count in the user-language vote — left in, two
// repair rounds could tie or outvote a non-English user's messages and
// the vote would no longer reflect the user's language.
func (a *Agent) recentUserMessages(messages []api.Message) []string {
	var recent []string
	for i := len(messages) - 1; i >= 0 && len(recent) < languageGuardRecentMessages; i-- {
		if messages[i].Role != "user" {
			continue
		}
		text := StripUserMessageTimestamp(messages[i].Content)
		if strings.TrimSpace(text) == "" {
			continue
		}
		// A machine-injected verification report: the tag prefix is the
		// precise marker — a user quoting a report would not start their
		// message with it.
		if strings.HasPrefix(text, verificationReportOpenTag) {
			continue
		}
		recent = append(recent, text)
	}
	// ResolveUserLanguage's vote is order-independent; return
	// chronological so the list reads like the conversation.
	for i, j := 0, len(recent)-1; i < j; i, j = i+1, j-1 {
		recent[i], recent[j] = recent[j], recent[i]
	}
	return recent
}

// regenerateInUserLanguage issues the §152b regeneration: a single model
// call carrying the user's last message, the mismatched reply, and an
// explicit instruction to answer in the user's language (code blocks,
// URLs, file paths and quoted text stay as-is). It is a focused prompt —
// one model call, no tool loop. The run's query (qc.processedQuery) is the
// user's last message for the turn.
func (a *Agent) regenerateInUserLanguage(ctx context.Context, qc *queryRunContext, mismatched string, user langguard.Language) (string, error) {
	return a.regenerateInUserLanguageCore(ctx, qc.processedQuery, mismatched, user)
}

// regenerateInUserLanguageCore is the shared §152b regeneration primitive: a
// single model call carrying the user's last message, the mismatched reply,
// and an explicit instruction to answer in the user's language (code blocks,
// URLs, file paths and quoted text stay as-is). It is a focused prompt — one
// model call, no tool loop. Both the final-message guard (via
// regenerateInUserLanguage, using the run's query) and the streaming path (via
// the turn's stored user message) call it, so the regeneration prompt is
// byte-identical across the two guard layers.
func (a *Agent) regenerateInUserLanguageCore(ctx context.Context, lastUserMessage string, mismatched string, user langguard.Language) (string, error) {
	client := a.getClient()
	if client == nil {
		return "", errors.New("language guard: no client available for regeneration")
	}
	name := user.Name
	if name == "" {
		name = user.Code
	}
	instruction := fmt.Sprintf(
		"Your previous reply was not in %s. Answer again in %s. "+
			"Keep code blocks, URLs, file paths and quoted text exactly as they are; "+
			"write only the surrounding prose in %s.",
		name, name, name,
	)
	req := []api.Message{
		{Role: "user", Content: StripUserMessageTimestamp(lastUserMessage)},
		{Role: "assistant", Content: mismatched},
		{Role: "user", Content: instruction},
	}
	if req[0].Content == "" {
		// A wakeup-only turn has no user message of its own; the
		// mismatched reply plus the instruction is enough to regenerate.
		req = req[1:]
	}
	resp, err := client.SendChatRequest(ctx, req, nil, "", false)
	if err != nil {
		return "", err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return "", errors.New("language guard: regeneration returned no response")
	}
	return resp.Choices[0].Message.Content, nil
}
