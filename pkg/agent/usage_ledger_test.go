package agent

import "testing"

func TestTakeUnbookedUsage_BooksEachTurnOnce(t *testing.T) {
	a := newTestAgent(t)

	a.state.SetPromptTokens(100)
	a.state.SetCompletionTokens(10)
	a.state.SetChargedCostTotal(0.01)
	first := a.TakeUnbookedUsage()
	if first.PromptTokens != 100 || first.CompletionTokens != 10 || first.ChargedCost != 0.01 {
		t.Fatalf("first booking = %+v", first)
	}

	a.state.SetPromptTokens(250)
	a.state.SetCompletionTokens(30)
	a.state.SetChargedCostTotal(0.03)
	second := a.TakeUnbookedUsage()
	if second.PromptTokens != 150 || second.CompletionTokens != 20 || second.ChargedCost < 0.0199 || second.ChargedCost > 0.0201 {
		t.Fatalf("second booking = %+v, want only the second turn", second)
	}

	if again := a.TakeUnbookedUsage(); again != (Usage{}) {
		t.Fatalf("nothing new, booked %+v", again)
	}
}

func TestTakeUnbookedUsage_RestoredConversationIsNotRebooked(t *testing.T) {
	source := newTestAgent(t)
	source.state.SetPromptTokens(500)
	source.state.SetChargedCostTotal(0.05)
	snapshot, err := source.ExportState()
	if err != nil {
		t.Fatal(err)
	}

	a := newTestAgent(t)
	if err := a.ImportState(snapshot); err != nil {
		t.Fatal(err)
	}
	if got := a.TakeUnbookedUsage(); got != (Usage{}) {
		t.Fatalf("restored history booked again: %+v", got)
	}
	a.state.SetPromptTokens(600)
	a.state.SetChargedCostTotal(0.06)
	if got := a.TakeUnbookedUsage(); got.PromptTokens != 100 {
		t.Fatalf("after restore booked %+v, want the new turn only", got)
	}
}

func TestTakeUnbookedUsage_ResetTotalsStartOver(t *testing.T) {
	a := newTestAgent(t)
	a.state.SetPromptTokens(400)
	a.TakeUnbookedUsage()

	a.state.SetPromptTokens(50)
	if got := a.TakeUnbookedUsage(); got.PromptTokens != 50 {
		t.Fatalf("after a reset booked %+v, want 50 prompt tokens", got)
	}
}
