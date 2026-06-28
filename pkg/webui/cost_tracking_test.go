//go:build !js

package webui

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// floatEq checks two floats are within tolerance
func floatEq(got, want float64, tol float64) bool {
	return math.Abs(got-want) < tol
}

func makeCostStore(t *testing.T) *CostStore {
	t.Helper()
	cs := &CostStore{
		filePath:    filepath.Join(t.TempDir(), "costs.json"),
		lastPersist: time.Now(), // prevent async persist goroutine from firing
	}
	return cs
}

func TestRecordCost_AddsRecord(t *testing.T) {
	cs := makeCostStore(t)

	cs.RecordCost("openai", "gpt-4", "sess1", "chat1", 100, 50, 0.05)

	if len(cs.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(cs.records))
	}
	r := cs.records[0]
	if r.Provider != "openai" {
		t.Errorf("provider = %q, want %q", r.Provider, "openai")
	}
	if r.Model != "gpt-4" {
		t.Errorf("model = %q, want %q", r.Model, "gpt-4")
	}
	if r.Cost != 0.05 {
		t.Errorf("cost = %f, want 0.05", r.Cost)
	}
	if r.PromptTokens != 100 {
		t.Errorf("prompt_tokens = %d, want 100", r.PromptTokens)
	}
	if r.OutputTokens != 50 {
		t.Errorf("output_tokens = %d, want 50", r.OutputTokens)
	}
	if r.SessionID != "sess1" {
		t.Errorf("session_id = %q, want %q", r.SessionID, "sess1")
	}
	if r.ChatID != "chat1" {
		t.Errorf("chat_id = %q, want %q", r.ChatID, "chat1")
	}
}

func TestRecordCost_IgnoresZeroCost(t *testing.T) {
	cs := makeCostStore(t)

	cs.RecordCost("openai", "gpt-4", "", "", 100, 50, 0)
	cs.RecordCost("anthropic", "claude", "", "", 100, 50, -0.01)

	if len(cs.records) != 0 {
		t.Fatalf("expected 0 records (cost <= 0), got %d", len(cs.records))
	}
}

func TestRecordCost_MultipleRecords(t *testing.T) {
	cs := makeCostStore(t)

	cs.RecordCost("openai", "gpt-4", "", "", 100, 50, 0.05)
	cs.RecordCost("anthropic", "claude", "", "", 200, 100, 0.10)
	cs.RecordCost("openai", "gpt-3.5", "", "", 50, 25, 0.02)

	if len(cs.records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(cs.records))
	}
}

func TestGetSummary_InRange(t *testing.T) {
	cs := makeCostStore(t)

	now := time.Now()

	// Manually insert records with fixed timestamps
	cs.records = []CostRecord{
		{Timestamp: now.Add(-1 * time.Hour), Provider: "openai", Model: "gpt-4", Cost: 0.05},
		{Timestamp: now.Add(-30 * time.Minute), Provider: "anthropic", Model: "claude", Cost: 0.10},
	}

	startDate := now.Add(-2 * time.Hour)
	endDate := now.Add(1 * time.Hour)

	totalCost, byProvider, byModel := cs.GetSummary(startDate, endDate)

	// Both records should be included
	if !floatEq(totalCost, 0.15, 0.0001) {
		t.Errorf("totalCost = %f, want 0.15", totalCost)
	}
	if !floatEq(byProvider["openai"], 0.05, 0.0001) {
		t.Errorf("byProvider[openai] = %f, want 0.05", byProvider["openai"])
	}
	if !floatEq(byProvider["anthropic"], 0.10, 0.0001) {
		t.Errorf("byProvider[anthropic] = %f, want 0.10", byProvider["anthropic"])
	}
	if !floatEq(byModel["openai:gpt-4"], 0.05, 0.0001) {
		t.Errorf("byModel[openai:gpt-4] = %f, want 0.05", byModel["openai:gpt-4"])
	}
	if !floatEq(byModel["anthropic:claude"], 0.10, 0.0001) {
		t.Errorf("byModel[anthropic:claude] = %f, want 0.10", byModel["anthropic:claude"])
	}
}

func TestGetSummary_ExcludedRange(t *testing.T) {
	cs := makeCostStore(t)

	now := time.Now()

	cs.records = []CostRecord{
		{Timestamp: now.Add(-48 * time.Hour), Provider: "openai", Model: "gpt-4", Cost: 0.05},
		{Timestamp: now.Add(-24 * time.Hour), Provider: "anthropic", Model: "claude", Cost: 0.10},
	}

	// Query a range that doesn't include any records
	startDate := now.Add(-1 * time.Hour)
	endDate := now.Add(1 * time.Hour)

	totalCost, byProvider, byModel := cs.GetSummary(startDate, endDate)

	if totalCost != 0 {
		t.Errorf("totalCost = %f, want 0", totalCost)
	}
	if len(byProvider) != 0 {
		t.Errorf("byProvider should be empty, got %v", byProvider)
	}
	if len(byModel) != 0 {
		t.Errorf("byModel should be empty, got %v", byModel)
	}
}

func TestGetSummary_MultipleSameProvider(t *testing.T) {
	cs := makeCostStore(t)

	now := time.Now()
	cs.records = []CostRecord{
		{Timestamp: now.Add(-1 * time.Hour), Provider: "openai", Model: "gpt-4", Cost: 0.05},
		{Timestamp: now.Add(-30 * time.Minute), Provider: "openai", Model: "gpt-3.5", Cost: 0.02},
	}

	startDate := now.Add(-2 * time.Hour)
	endDate := now.Add(1 * time.Hour)

	totalCost, byProvider, byModel := cs.GetSummary(startDate, endDate)

	if totalCost != 0.07 {
		t.Errorf("totalCost = %f, want 0.07", totalCost)
	}
	if byProvider["openai"] != 0.07 {
		t.Errorf("byProvider[openai] = %f, want 0.07", byProvider["openai"])
	}
	if byModel["openai:gpt-4"] != 0.05 {
		t.Errorf("byModel[openai:gpt-4] = %f, want 0.05", byModel["openai:gpt-4"])
	}
	if byModel["openai:gpt-3.5"] != 0.02 {
		t.Errorf("byModel[openai:gpt-3.5] = %f, want 0.02", byModel["openai:gpt-3.5"])
	}
}

func TestGetDailyCosts(t *testing.T) {
	cs := makeCostStore(t)

	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	twoDaysAgo := now.AddDate(0, 0, -2)

	cs.records = []CostRecord{
		{Timestamp: now, Provider: "openai", Model: "gpt-4", Cost: 0.05},
		{Timestamp: now.Add(-30 * time.Minute), Provider: "openai", Model: "gpt-3.5", Cost: 0.03},
		{Timestamp: yesterday, Provider: "anthropic", Model: "claude", Cost: 0.10},
		{Timestamp: twoDaysAgo, Provider: "openai", Model: "gpt-4", Cost: 0.07},
	}

	dailyCosts := cs.GetDailyCosts(7)

	// Should have 3 days with costs
	if len(dailyCosts) != 3 {
		t.Fatalf("expected 3 daily entries, got %d", len(dailyCosts))
	}

	// Build a map for easier checking
	dailyMap := make(map[string]*DailyCost)
	for i := range dailyCosts {
		dailyMap[dailyCosts[i].Date] = &dailyCosts[i]
	}

	today := now.Format("2006-01-02")
	yesterdayStr := yesterday.Format("2006-01-02")
	twoDaysAgoStr := twoDaysAgo.Format("2006-01-02")

	todayCost, ok := dailyMap[today]
	if !ok {
		t.Fatalf("missing today's daily cost entry")
	}
	if todayCost.TotalCost != 0.08 {
		t.Errorf("today total = %f, want 0.08", todayCost.TotalCost)
	}
	if todayCost.ByProvider["openai"] != 0.08 {
		t.Errorf("today openai = %f, want 0.08", todayCost.ByProvider["openai"])
	}

	yesterdayCost, ok := dailyMap[yesterdayStr]
	if !ok {
		t.Fatalf("missing yesterday's daily cost entry")
	}
	if yesterdayCost.TotalCost != 0.10 {
		t.Errorf("yesterday total = %f, want 0.10", yesterdayCost.TotalCost)
	}

	twoDaysAgoCost, ok := dailyMap[twoDaysAgoStr]
	if !ok {
		t.Fatalf("missing two days ago daily cost entry")
	}
	if twoDaysAgoCost.TotalCost != 0.07 {
		t.Errorf("two days ago total = %f, want 0.07", twoDaysAgoCost.TotalCost)
	}
}

func TestGetDailyCosts_OutOfRange(t *testing.T) {
	cs := makeCostStore(t)

	now := time.Now()
	old := now.AddDate(0, 0, -30)

	cs.records = []CostRecord{
		{Timestamp: old, Provider: "openai", Model: "gpt-4", Cost: 0.05},
	}

	// Query last 7 days; old record is outside range
	dailyCosts := cs.GetDailyCosts(7)

	if len(dailyCosts) != 0 {
		t.Errorf("expected 0 daily entries (old record out of range), got %d", len(dailyCosts))
	}
}

func TestGetCostSummary_AllFields(t *testing.T) {
	cs := makeCostStore(t)

	now := time.Now()
	recent := now.Add(-1 * time.Hour)
	fiveDaysAgo := now.AddDate(0, 0, -5)
	twentyDaysAgo := now.AddDate(0, 0, -20)
	fortyDaysAgo := now.AddDate(0, 0, -40)

	startOfThisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	startOfLastMonth := startOfThisMonth.AddDate(0, -1, 1)

	// Place a record safely in last month
	lastMonthDate := startOfLastMonth.AddDate(0, 0, 15)
	if !lastMonthDate.Before(startOfThisMonth) {
		// If we're early in the month, use an earlier day
		lastMonthDate = startOfLastMonth.AddDate(0, 0, 1)
	}

	cs.records = []CostRecord{
		{Timestamp: recent, Provider: "openai", Model: "gpt-4", Cost: 0.05},
		{Timestamp: fiveDaysAgo, Provider: "openai", Model: "gpt-3.5", Cost: 0.03},
		{Timestamp: twentyDaysAgo, Provider: "anthropic", Model: "claude", Cost: 0.10},
		{Timestamp: fortyDaysAgo, Provider: "openai", Model: "gpt-4", Cost: 0.02},
		{Timestamp: lastMonthDate, Provider: "anthropic", Model: "claude", Cost: 0.08},
	}

	summary := cs.GetCostSummary()

	// Total cost should be sum of all
	if !floatEq(summary.TotalCost, 0.05+0.03+0.10+0.02+0.08, 0.0001) {
		t.Errorf("TotalCost = %f, want %f", summary.TotalCost, 0.05+0.03+0.10+0.02+0.08)
	}

	// ByProvider
	if !floatEq(summary.ByProvider["openai"], 0.10, 0.0001) {
		t.Errorf("ByProvider[openai] = %f, want 0.10", summary.ByProvider["openai"])
	}
	if !floatEq(summary.ByProvider["anthropic"], 0.18, 0.0001) {
		t.Errorf("ByProvider[anthropic] = %f, want 0.18", summary.ByProvider["anthropic"])
	}

	// ByModel (keyed as "provider:model")
	if !floatEq(summary.ByModel["openai:gpt-4"], 0.07, 0.0001) {
		t.Errorf("ByModel[openai:gpt-4] = %f, want 0.07", summary.ByModel["openai:gpt-4"])
	}
	if !floatEq(summary.ByModel["openai:gpt-3.5"], 0.03, 0.0001) {
		t.Errorf("ByModel[openai:gpt-3.5] = %f, want 0.03", summary.ByModel["openai:gpt-3.5"])
	}
	if !floatEq(summary.ByModel["anthropic:claude"], 0.18, 0.0001) {
		t.Errorf("ByModel[anthropic:claude] = %f, want 0.18", summary.ByModel["anthropic:claude"])
	}

	// Compute expected time-window sums dynamically based on month boundaries
	var expectedLast7, expectedLast30, expectedThisMonth, expectedLastMonth float64
	for _, r := range cs.records {
		// Last 7 days
		if r.Timestamp.After(now.AddDate(0, 0, -7)) {
			expectedLast7 += r.Cost
		}
		// Last 30 days
		if r.Timestamp.After(now.AddDate(0, 0, -30)) {
			expectedLast30 += r.Cost
		}
		// This month: after midnight of first day of current month
		if r.Timestamp.After(startOfThisMonth) {
			expectedThisMonth += r.Cost
		}
		// Last month: after start of last month and before start of this month
		if r.Timestamp.After(startOfLastMonth) && r.Timestamp.Before(startOfThisMonth) {
			expectedLastMonth += r.Cost
		}
	}

	if !floatEq(summary.Last7Days, expectedLast7, 0.0001) {
		t.Errorf("Last7Days = %f, want %f", summary.Last7Days, expectedLast7)
	}
	if !floatEq(summary.Last30Days, expectedLast30, 0.0001) {
		t.Errorf("Last30Days = %f, want %f", summary.Last30Days, expectedLast30)
	}
	if !floatEq(summary.ThisMonth, expectedThisMonth, 0.0001) {
		t.Errorf("ThisMonth = %f, want %f", summary.ThisMonth, expectedThisMonth)
	}
	if !floatEq(summary.LastMonth, expectedLastMonth, 0.0001) {
		t.Errorf("LastMonth = %f, want %f", summary.LastMonth, expectedLastMonth)
	}

	// ByProviderThisMonth
	var expectedThisMonthProvider = make(map[string]float64)
	for _, r := range cs.records {
		if r.Timestamp.After(startOfThisMonth) {
			expectedThisMonthProvider[r.Provider] += r.Cost
		}
	}
	for provider, expected := range expectedThisMonthProvider {
		if !floatEq(summary.ByProviderThisMonth[provider], expected, 0.0001) {
			t.Errorf("ByProviderThisMonth[%s] = %f, want %f", provider, summary.ByProviderThisMonth[provider], expected)
		}
	}

	// ByProviderLastMonth
	var expectedLastMonthProvider = make(map[string]float64)
	for _, r := range cs.records {
		if r.Timestamp.After(startOfLastMonth) && r.Timestamp.Before(startOfThisMonth) {
			expectedLastMonthProvider[r.Provider] += r.Cost
		}
	}
	for provider, expected := range expectedLastMonthProvider {
		if !floatEq(summary.ByProviderLastMonth[provider], expected, 0.0001) {
			t.Errorf("ByProviderLastMonth[%s] = %f, want %f", provider, summary.ByProviderLastMonth[provider], expected)
		}
	}
}

func TestGetCostSummary_Empty(t *testing.T) {
	cs := makeCostStore(t)

	summary := cs.GetCostSummary()

	if summary.TotalCost != 0 {
		t.Errorf("TotalCost = %f, want 0", summary.TotalCost)
	}
	if summary.Last30Days != 0 {
		t.Errorf("Last30Days = %f, want 0", summary.Last30Days)
	}
	if summary.Last7Days != 0 {
		t.Errorf("Last7Days = %f, want 0", summary.Last7Days)
	}
	if summary.ThisMonth != 0 {
		t.Errorf("ThisMonth = %f, want 0", summary.ThisMonth)
	}
	if summary.LastMonth != 0 {
		t.Errorf("LastMonth = %f, want 0", summary.LastMonth)
	}
	if len(summary.ByProvider) != 0 {
		t.Errorf("ByProvider should be empty, got %v", summary.ByProvider)
	}
	if len(summary.ByModel) != 0 {
		t.Errorf("ByModel should be empty, got %v", summary.ByModel)
	}
	if len(summary.ByProviderThisMonth) != 0 {
		t.Errorf("ByProviderThisMonth should be empty, got %v", summary.ByProviderThisMonth)
	}
	if len(summary.ByProviderLastMonth) != 0 {
		t.Errorf("ByProviderLastMonth should be empty, got %v", summary.ByProviderLastMonth)
	}
}

func TestForcePersist(t *testing.T) {
	cs := makeCostStore(t)

	// Insert records directly to avoid the async persist goroutine in RecordCost
	cs.records = []CostRecord{
		{Timestamp: time.Now(), Provider: "openai", Model: "gpt-4", Cost: 0.05, PromptTokens: 100, OutputTokens: 50},
		{Timestamp: time.Now(), Provider: "anthropic", Model: "claude", Cost: 0.10, PromptTokens: 200, OutputTokens: 100},
	}

	err := cs.ForcePersist()
	if err != nil {
		t.Fatalf("ForcePersist error: %v", err)
	}

	// Verify file exists
	data, err := os.ReadFile(cs.filePath)
	if err != nil {
		t.Fatalf("failed to read persisted file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("persisted file is empty")
	}

	// Verify file contains the records
	var records []CostRecord
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("failed to unmarshal persisted data: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records in file, got %d", len(records))
	}
	if records[0].Provider != "openai" {
		t.Errorf("record 0 provider = %q, want %q", records[0].Provider, "openai")
	}
	if records[1].Provider != "anthropic" {
		t.Errorf("record 1 provider = %q, want %q", records[1].Provider, "anthropic")
	}
}

func TestForcePersist_EmptyStore(t *testing.T) {
	cs := makeCostStore(t)

	err := cs.ForcePersist()
	if err != nil {
		t.Fatalf("ForcePersist error on empty store: %v", err)
	}

	data, err := os.ReadFile(cs.filePath)
	if err != nil {
		t.Fatalf("failed to read persisted file: %v", err)
	}
	// Empty array should be written
	if string(data) != "[]" {
		t.Errorf("persisted data = %q, want []", string(data))
	}
}
