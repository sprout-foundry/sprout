package agent

import (
	"context"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/langguard"
)

// Reliably detectable prose fixtures, mirrored from pkg/langguard's
// detect fixtures (the trigram detector reports them above its
// reliability bar).
const (
	lgEnglishProse  = "The build succeeded after applying the patch, so the tests can run and the release is ready to ship."
	lgEnglishProse2 = "All the tests passed after the patch was applied, so the release is now ready to be shipped."
	lgSpanishProse  = "El paquete está listo para compilar ahora mismo y las pruebas pasan sin errores."
)

// newLanguageGuardAgent builds an Agent backed by a scripted client with
// the language-guard config knobs set: the configured fallback language
// ("" leaves it unset) and the guard opt-out.
func newLanguageGuardAgent(t *testing.T, configuredLanguage string, guardDisabled bool, responses ...*ScriptedResponse) (*Agent, *ScriptedClient) {
	t.Helper()
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if configuredLanguage != "" || guardDisabled {
		if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.Language = configuredLanguage
			cfg.DisableLanguageGuard = guardDisabled
			return nil
		}); err != nil {
			t.Fatalf("UpdateConfigNoSave: %v", err)
		}
	}
	client := NewScriptedClient(responses...)
	ag, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	t.Cleanup(func() { ag.Shutdown() })
	return ag, client
}

// lastAssistantMessage returns the last assistant message in the agent's
// state.
func lastAssistantMessage(t *testing.T, ag *Agent) api.Message {
	t.Helper()
	msgs := ag.GetMessages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return msgs[i]
		}
	}
	t.Fatal("no assistant message in state")
	return api.Message{}
}

// TestLanguageGuardMismatchRegenerates drives the §152b success path:
// the user's language is inferred from their own (Spanish) message, the
// turn's final answer comes back in English (a mismatch), so the guard
// regenerates once — the corrected Spanish text ends up in state, the
// model was called exactly once for the regeneration, and that call
// carried the explicit language instruction.
func TestLanguageGuardMismatchRegenerates(t *testing.T) {
	ag, client := newLanguageGuardAgent(t, "", false,
		NewStopResponse(lgEnglishProse), // the turn's answer: wrong language
		NewStopResponse(lgSpanishProse), // the regeneration: correct
	)

	result, err := ag.ProcessQuery(lgSpanishProse)
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if result != lgSpanishProse {
		t.Errorf("result = %q, want the regenerated %q", result, lgSpanishProse)
	}
	if last := lastAssistantMessage(t, ag); last.Content != lgSpanishProse {
		t.Errorf("final assistant message = %q, want %q", last.Content, lgSpanishProse)
	}

	reqs := client.GetSentRequests()
	if len(reqs) != 2 {
		t.Fatalf("model calls = %d, want 2 (the turn + one regeneration)", len(reqs))
	}
	lastReq := reqs[len(reqs)-1]
	if len(lastReq) != 3 {
		t.Fatalf("regeneration request has %d messages, want 3 (user message, mismatched reply, instruction)", len(lastReq))
	}
	if lastReq[1].Content != lgEnglishProse {
		t.Errorf("regeneration request does not carry the mismatched reply (got %q)", lastReq[1].Content)
	}
	if !strings.Contains(strings.ToLower(lastReq[2].Content), "spanish") {
		t.Errorf("regeneration instruction %q does not name the user's language", lastReq[2].Content)
	}
}

// TestLanguageGuardMismatchStillFailingShowsNotice drives the §152b
// second-mismatch path: the regeneration also comes back in the wrong
// language, so the user sees the templated notice in their (configured,
// Spanish) language and the original is retained for "view original".
func TestLanguageGuardMismatchStillFailingShowsNotice(t *testing.T) {
	ag, client := newLanguageGuardAgent(t, "es", false,
		NewStopResponse(lgEnglishProse),  // the turn's answer: English
		NewStopResponse(lgEnglishProse2), // the regeneration: still English
	)

	result, err := ag.ProcessQuery("Hola")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	wantNotice := LanguageMismatchNotice(langguard.Language{Code: "es", Name: "Spanish"})
	if result != wantNotice {
		t.Errorf("result = %q, want the notice %q", result, wantNotice)
	}

	last := lastAssistantMessage(t, ag)
	if last.Content != wantNotice {
		t.Errorf("final assistant message = %q, want the notice %q", last.Content, wantNotice)
	}
	if original := last.Meta[langGuardOriginalMetaKey]; original != lgEnglishProse {
		t.Errorf("view-original payload = %q, want the mismatched original %q", original, lgEnglishProse)
	}
	if len(client.GetSentRequests()) != 2 {
		t.Errorf("model calls = %d, want 2 (the turn + one regeneration)", len(client.GetSentRequests()))
	}
}

// TestLanguageGuardPassNoRegeneration drives the no-mismatch path: the
// guard runs, the answer is in the user's language, and nothing is
// changed — in particular no extra model call is made.
func TestLanguageGuardPassNoRegeneration(t *testing.T) {
	ag, client := newLanguageGuardAgent(t, "es", false,
		NewStopResponse(lgSpanishProse),
	)

	result, err := ag.ProcessQuery("Hola")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if result != lgSpanishProse {
		t.Errorf("result = %q, want the unguarded %q", result, lgSpanishProse)
	}
	if last := lastAssistantMessage(t, ag); last.Content != lgSpanishProse {
		t.Errorf("final assistant message = %q, want %q", last.Content, lgSpanishProse)
	}
	if calls := len(client.GetSentRequests()); calls != 1 {
		t.Errorf("model calls = %d, want 1 (the turn only, no regeneration)", calls)
	}
}

// TestLanguageGuardDisabledLeavesMessageUnchanged pins the opt-out: with
// the guard disabled a wrong-language answer is not touched and no
// regeneration call is made.
func TestLanguageGuardDisabledLeavesMessageUnchanged(t *testing.T) {
	ag, client := newLanguageGuardAgent(t, "es", true,
		NewStopResponse(lgEnglishProse),
	)

	result, err := ag.ProcessQuery("Hola")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if result != lgEnglishProse {
		t.Errorf("result = %q, want the untouched %q", result, lgEnglishProse)
	}
	if last := lastAssistantMessage(t, ag); last.Content != lgEnglishProse {
		t.Errorf("final assistant message = %q, want the untouched %q", last.Content, lgEnglishProse)
	}
	if calls := len(client.GetSentRequests()); calls != 1 {
		t.Errorf("model calls = %d, want 1 (the guard is off, no regeneration)", calls)
	}
}

// TestLanguageGuardUndeterminedUserLanguageUnchanged pins the
// undetermined path: no configured language and a message too short to
// infer one, so the guard never guesses and the answer is untouched.
func TestLanguageGuardUndeterminedUserLanguageUnchanged(t *testing.T) {
	ag, client := newLanguageGuardAgent(t, "", false,
		NewStopResponse(lgEnglishProse),
	)

	result, err := ag.ProcessQuery("hi")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if result != lgEnglishProse {
		t.Errorf("result = %q, want the untouched %q", result, lgEnglishProse)
	}
	if last := lastAssistantMessage(t, ag); last.Content != lgEnglishProse {
		t.Errorf("final assistant message = %q, want the untouched %q", last.Content, lgEnglishProse)
	}
	if calls := len(client.GetSentRequests()); calls != 1 {
		t.Errorf("model calls = %d, want 1 (the guard never judges an undetermined user)", calls)
	}
}

// TestLanguageGuardSkipsSubagents pins that subagents (whose output goes
// to the orchestrator, not the end user) never trigger the user-facing
// notice flow: the guard is skipped and no regeneration call is made.
func TestLanguageGuardSkipsSubagents(t *testing.T) {
	ag, client := newLanguageGuardAgent(t, "es", false,
		NewStopResponse(lgEnglishProse),
		NewStopResponse(lgSpanishProse),
	)
	ag.subagentDepth = 1

	result, err := ag.ProcessQuery("Hola")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if result != lgEnglishProse {
		t.Errorf("result = %q, want the untouched %q (subagents are exempt)", result, lgEnglishProse)
	}
	if last := lastAssistantMessage(t, ag); last.Content != lgEnglishProse {
		t.Errorf("final assistant message = %q, want the untouched %q", last.Content, lgEnglishProse)
	}
	if calls := len(client.GetSentRequests()); calls != 1 {
		t.Errorf("model calls = %d, want 1 (subagents never regenerate)", calls)
	}
}

// TestApplyLanguageGuardNilConfig pins the guard's behavior on an agent
// without a config manager: the guard resolves to its default (enabled,
// §152f), the user's language is inferred from their message, and the
// failed regeneration (no client) shows the notice — without a panic.
func TestApplyLanguageGuardNilConfig(t *testing.T) {
	ag := NewTestAgent()
	ag.SetMessages([]api.Message{
		{Role: "user", Content: lgSpanishProse},
		{Role: "assistant", Content: lgEnglishProse},
	})
	qc := &queryRunContext{runCtx: context.Background(), processedQuery: lgSpanishProse}

	out := ag.applyLanguageGuard(qc, lgEnglishProse)
	want := LanguageMismatchNotice(langguard.Language{Code: "es", Name: "Spanish"})
	if out != want {
		t.Errorf("applyLanguageGuard = %q, want the notice %q", out, want)
	}
	if last := lastAssistantMessage(t, ag); last.Content != want {
		t.Errorf("final assistant message = %q, want the notice %q", last.Content, want)
	}
}

// TestLanguageGuardMismatchRecordedPerModel pins the §152e metric (item
// 152.9): the guard records a check for every judged final message and a
// mismatch when one is detected, both keyed by the turn's model ID. A
// mismatch turn records one check and one mismatch; a pass turn records one
// check and no mismatch.
func TestLanguageGuardMismatchRecordedPerModel(t *testing.T) {
	// Mismatch turn: user writes in Spanish, the answer comes back in
	// English; the guard regenerates into Spanish. A fresh recorder is
	// installed so the assertions are isolated from other tests.
	mismatchMetrics := NewLanguageGuardMetrics()
	cleanupA := SetGlobalLanguageGuardMetricsForTest(mismatchMetrics)
	agA, _ := newLanguageGuardAgent(t, "", false,
		NewStopResponse(lgEnglishProse), // the turn's answer: wrong language
		NewStopResponse(lgSpanishProse), // the regeneration: correct
	)
	modelA := metricKeyFor(agA)
	if _, err := agA.ProcessQuery(lgSpanishProse); err != nil {
		t.Fatalf("ProcessQuery (mismatch turn): %v", err)
	}
	cleanupA()

	mism := modelStat(mismatchMetrics, modelA)
	if mism.Checks != 1 {
		t.Errorf("mismatch turn: model %q Checks = %d, want 1", modelA, mism.Checks)
	}
	if mism.Mismatches != 1 {
		t.Errorf("mismatch turn: model %q Mismatches = %d, want 1", modelA, mism.Mismatches)
	}

	// Pass turn: the answer is already in the user's (Spanish) language, so
	// the guard judges it but records no mismatch.
	passMetrics := NewLanguageGuardMetrics()
	cleanupB := SetGlobalLanguageGuardMetricsForTest(passMetrics)
	agB, _ := newLanguageGuardAgent(t, "es", false,
		NewStopResponse(lgSpanishProse),
	)
	modelB := metricKeyFor(agB)
	if _, err := agB.ProcessQuery("Hola"); err != nil {
		t.Fatalf("ProcessQuery (pass turn): %v", err)
	}
	cleanupB()

	pass := modelStat(passMetrics, modelB)
	if pass.Checks != 1 {
		t.Errorf("pass turn: model %q Checks = %d, want 1", modelB, pass.Checks)
	}
	if pass.Mismatches != 0 {
		t.Errorf("pass turn: model %q Mismatches = %d, want 0", modelB, pass.Mismatches)
	}
}

// metricKeyFor maps an agent's model ID to the key the recorder buckets it
// under (an empty model ID is recorded as "unknown").
func metricKeyFor(ag *Agent) string {
	id := ag.GetModel()
	if id == "" {
		return "unknown"
	}
	return id
}

// modelStat returns the stat for a model ID from a recorder's snapshot
// (a zero stat when the model has no recorded checks).
func modelStat(m *LanguageGuardMetrics, modelID string) LanguageGuardModelStat {
	for _, s := range m.Snapshot() {
		if s.ModelID == modelID {
			return s
		}
	}
	return LanguageGuardModelStat{ModelID: modelID}
}
