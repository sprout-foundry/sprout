// repetition_metrics_test.go — unit tests for the per-(provider, model)
// repetition-loop metric.
package agent

import "testing"

// TestRepetitionMetricsRecordsPerProviderModel pins that loops are counted per
// (provider, model) pair, with an empty provider/model bucketed under
// "unknown" and a stable, sorted snapshot.
func TestRepetitionMetricsRecordsPerProviderModel(t *testing.T) {
	m := NewRepetitionMetrics()
	m.Record("deepseek", "v4-flash")
	m.Record("deepseek", "v4-flash")
	m.Record("openai", "gpt-5")
	m.Record("", "")

	snapshot := m.Snapshot()
	want := []RepetitionLoopStat{
		{Provider: "deepseek", Model: "v4-flash", Loops: 2},
		{Provider: "openai", Model: "gpt-5", Loops: 1},
		{Provider: "unknown", Model: "unknown", Loops: 1},
	}
	if len(snapshot) != len(want) {
		t.Fatalf("snapshot has %d cells, want %d: %+v", len(snapshot), len(want), snapshot)
	}
	for i := range want {
		if snapshot[i] != want[i] {
			t.Errorf("cell %d = %+v, want %+v", i, snapshot[i], want[i])
		}
	}
	if got := m.Total(); got != 4 {
		t.Errorf("Total() = %d, want 4", got)
	}
}

// TestRepetitionMetricsNilSafe pins that a nil recorder is a no-op (Record,
// Snapshot, Total) so a caller never panics without a recorder.
func TestRepetitionMetricsNilSafe(t *testing.T) {
	var m *RepetitionMetrics
	m.Record("p", "mo") // must not panic
	if got := m.Snapshot(); got != nil {
		t.Errorf("nil Snapshot() = %+v, want nil", got)
	}
	if got := m.Total(); got != 0 {
		t.Errorf("nil Total() = %d, want 0", got)
	}
}

// TestAgentRepetitionLoopStatsExposesGlobal pins the agent accessor: it reads
// the process-wide recorder, so a recorded loop is visible through the agent.
func TestAgentRepetitionLoopStatsExposesGlobal(t *testing.T) {
	metrics := NewRepetitionMetrics()
	cleanup := SetGlobalRepetitionMetricsForTest(metrics)
	defer cleanup()

	GlobalRepetitionMetrics().Record("test", "test-model")

	ag := &Agent{}
	stats := ag.RepetitionLoopStats()
	found := false
	for _, s := range stats {
		if s.Provider == "test" && s.Model == "test-model" && s.Loops == 1 {
			found = true
		}
	}
	if !found {
		t.Errorf("RepetitionLoopStats() = %+v, want the recorded (test, test-model) loop", stats)
	}
}
