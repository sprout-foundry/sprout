//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
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

	summary := cs.GetCostSummary(time.Time{}, time.Time{})

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

	summary := cs.GetCostSummary(time.Time{}, time.Time{})

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
	if summary.FirstActivity != nil {
		t.Errorf("FirstActivity should be nil for empty store, got %v", summary.FirstActivity)
	}
	if summary.LastActivity != nil {
		t.Errorf("LastActivity should be nil for empty store, got %v", summary.LastActivity)
	}
}

// --- SP-080: All-time activity bounds for stale-data banner ---

func TestCostSummary_ActivityBounds_Populated(t *testing.T) {
	cs := makeCostStore(t)
	now := time.Now()

	// Records in non-sorted order so the min/max has to scan, not just
	// grab the first/last slice element.
	cs.records = []CostRecord{
		{Timestamp: now.Add(-10 * 24 * time.Hour), Provider: "openai", Model: "gpt-4", Cost: 0.05},
		{Timestamp: now.Add(-90 * 24 * time.Hour), Provider: "anthropic", Model: "claude", Cost: 0.10},
		{Timestamp: now.Add(-45 * 24 * time.Hour), Provider: "openai", Model: "gpt-3.5", Cost: 0.03},
		{Timestamp: now.Add(-1 * time.Hour), Provider: "openai", Model: "gpt-4", Cost: 0.02},
	}

	summary := cs.GetCostSummary(time.Time{}, time.Time{})

	if summary.FirstActivity == nil {
		t.Fatal("FirstActivity should not be nil for populated store")
	}
	if summary.LastActivity == nil {
		t.Fatal("LastActivity should not be nil for populated store")
	}

	wantFirst := now.Add(-90 * 24 * time.Hour)
	wantLast := now.Add(-1 * time.Hour)

	// Allow 1s slack because the recorded timestamps are derived from
	// `now` at test start; comparing with millisecond precision would
	// be flaky on slower machines.
	if diff := summary.FirstActivity.Sub(wantFirst); diff > time.Second || diff < -time.Second {
		t.Errorf("FirstActivity = %v, want ~%v (diff %v)", summary.FirstActivity, wantFirst, diff)
	}
	if diff := summary.LastActivity.Sub(wantLast); diff > time.Second || diff < -time.Second {
		t.Errorf("LastActivity = %v, want ~%v (diff %v)", summary.LastActivity, wantLast, diff)
	}

	// Bounds are emitted in UTC.
	if summary.FirstActivity.Location() != time.UTC {
		t.Errorf("FirstActivity location = %v, want UTC", summary.FirstActivity.Location())
	}
	if summary.LastActivity.Location() != time.UTC {
		t.Errorf("LastActivity location = %v, want UTC", summary.LastActivity.Location())
	}
}

func TestCostSummary_ActivityBounds_IndependentOfRangeFilter(t *testing.T) {
	cs := makeCostStore(t)
	now := time.Now()

	// All records are older than the 7-day range we'll filter by, but
	// the all-time bounds must still reflect min/max across ALL records.
	old := now.AddDate(0, 0, -60)
	older := now.AddDate(0, 0, -90)
	oldest := now.AddDate(0, 0, -120)

	cs.records = []CostRecord{
		{Timestamp: old, Provider: "openai", Model: "gpt-4", Cost: 0.05},
		{Timestamp: older, Provider: "openai", Model: "gpt-4", Cost: 0.05},
		{Timestamp: oldest, Provider: "openai", Model: "gpt-4", Cost: 0.05},
	}

	startDate := now.AddDate(0, 0, -7)
	endDate := now
	summary := cs.GetCostSummary(startDate, endDate)

	if summary.FirstActivity == nil || summary.LastActivity == nil {
		t.Fatal("activity bounds should be populated regardless of range filter")
	}
	if diff := summary.FirstActivity.Sub(oldest); diff > time.Second || diff < -time.Second {
		t.Errorf("FirstActivity = %v, want ~%v", summary.FirstActivity, oldest)
	}
	if diff := summary.LastActivity.Sub(old); diff > time.Second || diff < -time.Second {
		t.Errorf("LastActivity = %v, want ~%v", summary.LastActivity, old)
	}
}

func TestCostSummary_ActivityBounds_SingleRecord(t *testing.T) {
	cs := makeCostStore(t)
	now := time.Now()

	cs.records = []CostRecord{
		{Timestamp: now.Add(-2 * time.Hour), Provider: "openai", Model: "gpt-4", Cost: 0.05},
	}

	summary := cs.GetCostSummary(time.Time{}, time.Time{})

	if summary.FirstActivity == nil || summary.LastActivity == nil {
		t.Fatal("single record should produce non-nil bounds")
	}
	// For one record, first and last should be the same instant.
	if !summary.FirstActivity.Equal(*summary.LastActivity) {
		t.Errorf("single-record bounds should be equal: first=%v last=%v",
			summary.FirstActivity, summary.LastActivity)
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

func TestCostSummary_TopSessions_Populated(t *testing.T) {
	cs := makeCostStore(t)
	now := time.Now()

	// Create 12 sessions with descending costs
	for i := 0; i < 12; i++ {
		cost := float64(12-i) * 0.01
		cs.records = append(cs.records, CostRecord{
			Timestamp:  now.Add(-time.Duration(i) * time.Hour),
			Provider:   "openai",
			Model:      "gpt-4",
			Cost:       cost,
			SessionID:  fmt.Sprintf("sess-%d", i),
			Title:      fmt.Sprintf("Session %d", i),
			WorkingDir: fmt.Sprintf("/workspace/%d", i),
			LastUpdated: now.Format(time.RFC3339),
		})
	}

	summary := cs.GetCostSummary(time.Time{}, time.Time{})

	// Should have exactly 10 rows (top 10 of 12)
	if len(summary.TopSessions) != 10 {
		t.Fatalf("expected 10 top sessions, got %d", len(summary.TopSessions))
	}

	// Should be sorted by cost descending
	for i := 0; i < len(summary.TopSessions)-1; i++ {
		if summary.TopSessions[i].TotalCost < summary.TopSessions[i+1].TotalCost {
			t.Errorf("TopSessions not sorted desc: row %d cost %f < row %d cost %f",
				i, summary.TopSessions[i].TotalCost, i+1, summary.TopSessions[i+1].TotalCost)
		}
	}

	// Most expensive should be sess-0 (cost=0.12)
	if summary.TopSessions[0].SessionID != "sess-0" {
		t.Errorf("top session = %q, want %q", summary.TopSessions[0].SessionID, "sess-0")
	}
	if !floatEq(summary.TopSessions[0].TotalCost, 0.12, 0.0001) {
		t.Errorf("top session cost = %f, want 0.12", summary.TopSessions[0].TotalCost)
	}
}

func TestCostSummary_TopSessions_FilteredByTimeRange(t *testing.T) {
	cs := makeCostStore(t)
	now := time.Now()

	// Old session (outside range)
	cs.records = append(cs.records, CostRecord{
		Timestamp:  now.AddDate(0, 0, -60),
		Provider:   "openai",
		Model:      "gpt-4",
		Cost:       1.00,
		SessionID:  "old-sess",
		Title:      "Old Session",
		WorkingDir: "/old",
		LastUpdated: now.AddDate(0, 0, -60).Format(time.RFC3339),
	})

	// Recent session (inside range)
	cs.records = append(cs.records, CostRecord{
		Timestamp:  now.Add(-1 * time.Hour),
		Provider:   "openai",
		Model:      "gpt-4",
		Cost:       0.05,
		SessionID:  "new-sess",
		Title:      "New Session",
		WorkingDir: "/new",
		LastUpdated: now.Format(time.RFC3339),
	})

	startDate := now.AddDate(0, 0, -30)
	endDate := now

	summary := cs.GetCostSummary(startDate, endDate)

	// Should only include the recent session
	if len(summary.TopSessions) != 1 {
		t.Fatalf("expected 1 top session, got %d", len(summary.TopSessions))
	}
	if summary.TopSessions[0].SessionID != "new-sess" {
		t.Errorf("top session = %q, want %q", summary.TopSessions[0].SessionID, "new-sess")
	}
}

func TestCostSummary_TopSessions_Empty(t *testing.T) {
	cs := makeCostStore(t)

	summary := cs.GetCostSummary(time.Time{}, time.Time{})

	if summary.TopSessions == nil {
		t.Error("TopSessions should be empty slice, not nil")
	}
	if len(summary.TopSessions) != 0 {
		t.Errorf("expected 0 top sessions, got %d", len(summary.TopSessions))
	}
}

func TestCostSummary_TopSessions_MultipleRecordsPerSession(t *testing.T) {
	cs := makeCostStore(t)
	now := time.Now()

	// Two records for the same session
	cs.records = append(cs.records,
		CostRecord{
			Timestamp:  now.Add(-2 * time.Hour),
			Provider:   "openai",
			Model:      "gpt-4",
			Cost:       0.05,
			SessionID:  "sess-multi",
			Title:      "Multi-record Session",
			WorkingDir: "/workspace",
			LastUpdated: now.Add(-2 * time.Hour).Format(time.RFC3339),
		},
		CostRecord{
			Timestamp:  now.Add(-1 * time.Hour),
			Provider:   "anthropic",
			Model:      "claude",
			Cost:       0.10,
			SessionID:  "sess-multi",
			Title:      "Multi-record Session",
			WorkingDir: "/workspace",
			LastUpdated: now.Format(time.RFC3339),
		},
	)

	summary := cs.GetCostSummary(time.Time{}, time.Time{})

	if len(summary.TopSessions) != 1 {
		t.Fatalf("expected 1 top session, got %d", len(summary.TopSessions))
	}
	row := summary.TopSessions[0]
	if row.SessionID != "sess-multi" {
		t.Errorf("session = %q, want %q", row.SessionID, "sess-multi")
	}
	if !floatEq(row.TotalCost, 0.15, 0.0001) {
		t.Errorf("cost = %f, want 0.15", row.TotalCost)
	}
	if row.Title != "Multi-record Session" {
		t.Errorf("title = %q, want %q", row.Title, "Multi-record Session")
	}
	if row.WorkingDir != "/workspace" {
		t.Errorf("working_dir = %q, want %q", row.WorkingDir, "/workspace")
	}
}

func TestCostSummary_TopSessions_NoSessionID(t *testing.T) {
	cs := makeCostStore(t)
	now := time.Now()

	// Records without session IDs should not appear in TopSessions
	cs.records = append(cs.records,
		CostRecord{
			Timestamp: now.Add(-1 * time.Hour),
			Provider:  "openai",
			Model:     "gpt-4",
			Cost:      0.05,
			SessionID: "", // no session
		},
	)

	summary := cs.GetCostSummary(time.Time{}, time.Time{})

	if len(summary.TopSessions) != 0 {
		t.Errorf("expected 0 top sessions (no session IDs), got %d", len(summary.TopSessions))
	}
}

// --- SP-080: Billing-type-aware cost tracking tests ---

func TestCostSummaryBillingType_MixedBillingTypes(t *testing.T) {
	cs := makeCostStore(t)
	now := time.Now()

	cs.records = []CostRecord{
		{
			Timestamp:    now,
			Provider:     "openai",
			Model:        "gpt-4",
			PromptTokens: 1000,
			OutputTokens: 500,
			Cost:         0.10,
			BillingType:  "pay_per_token",
			ChargedCost:  0.10,
			TokenCost:    0.10,
		},
		{
			Timestamp:    now,
			Provider:     "zai-coding",
			Model:        "glm-4",
			PromptTokens: 2000,
			OutputTokens: 1000,
			Cost:         0,
			BillingType:  "subscription",
			ChargedCost:  0,
			TokenCost:    0.05,
		},
		{
			Timestamp:    now,
			Provider:     "local",
			Model:        "llama-3",
			PromptTokens: 500,
			OutputTokens: 250,
			Cost:         0,
			BillingType:  "free",
			ChargedCost:  0,
			TokenCost:    0.02,
		},
	}

	summary := cs.GetCostSummary(time.Time{}, time.Time{})

	// ByBillingType should have 3 entries
	if len(summary.ByBillingType) != 3 {
		t.Fatalf("ByBillingType has %d entries, want 3", len(summary.ByBillingType))
	}

	// pay_per_token bucket
	ppt, ok := summary.ByBillingType["pay_per_token"]
	if !ok {
		t.Fatal("missing pay_per_token in ByBillingType")
	}
	if !floatEq(ppt.Cost, 0.10, 0.0001) {
		t.Errorf("pay_per_token cost = %f, want 0.10", ppt.Cost)
	}
	if ppt.Tokens != 1500 {
		t.Errorf("pay_per_token tokens = %d, want 1500", ppt.Tokens)
	}

	// subscription bucket
	sub, ok := summary.ByBillingType["subscription"]
	if !ok {
		t.Fatal("missing subscription in ByBillingType")
	}
	if !floatEq(sub.Cost, 0.0, 0.0001) {
		t.Errorf("subscription cost = %f, want 0.0", sub.Cost)
	}
	if sub.Tokens != 3000 {
		t.Errorf("subscription tokens = %d, want 3000", sub.Tokens)
	}

	// free bucket
	free, ok := summary.ByBillingType["free"]
	if !ok {
		t.Fatal("missing free in ByBillingType")
	}
	if !floatEq(free.Cost, 0.0, 0.0001) {
		t.Errorf("free cost = %f, want 0.0", free.Cost)
	}
	if free.Tokens != 750 {
		t.Errorf("free tokens = %d, want 750", free.Tokens)
	}
}

func TestCostSummaryBillingType_ChargedCostAndTokenValue(t *testing.T) {
	cs := makeCostStore(t)
	now := time.Now()

	cs.records = []CostRecord{
		{
			Timestamp:    now,
			Provider:     "openai",
			Model:        "gpt-4",
			PromptTokens: 1000,
			OutputTokens: 500,
			Cost:         0.10,
			BillingType:  "pay_per_token",
			ChargedCost:  0.10,
			TokenCost:    0.12,
		},
		{
			Timestamp:    now,
			Provider:     "zai-coding",
			Model:        "glm-4",
			PromptTokens: 2000,
			OutputTokens: 1000,
			Cost:         0,
			BillingType:  "subscription",
			ChargedCost:  0,
			TokenCost:    0.05,
		},
		{
			Timestamp:    now,
			Provider:     "local",
			Model:        "llama-3",
			PromptTokens: 500,
			OutputTokens: 250,
			Cost:         0,
			BillingType:  "free",
			ChargedCost:  0,
			TokenCost:    0.02,
		},
	}

	summary := cs.GetCostSummary(time.Time{}, time.Time{})

	// ChargedCost should sum all ChargedCost fields (with fallback to Cost)
	if !floatEq(summary.ChargedCost, 0.10, 0.0001) {
		t.Errorf("ChargedCost = %f, want 0.10", summary.ChargedCost)
	}

	// TokenValue should sum all TokenCost fields
	if !floatEq(summary.TokenValue, 0.19, 0.0001) {
		t.Errorf("TokenValue = %f, want 0.19", summary.TokenValue)
	}
}

func TestCostSummaryBillingType_OldRecordsDefaultToPayPerToken(t *testing.T) {
	cs := makeCostStore(t)
	now := time.Now()

	// Old record with no BillingType and no ChargedCost — should default to pay_per_token
	// and fall back to Cost field for charged cost.
	cs.records = []CostRecord{
		{
			Timestamp:    now,
			Provider:     "openai",
			Model:        "gpt-4",
			PromptTokens: 1000,
			OutputTokens: 500,
			Cost:         0.10,
			BillingType:  "", // missing — should default to pay_per_token
			ChargedCost:  0,  // missing — should fall back to Cost
			TokenCost:    0,
		},
	}

	summary := cs.GetCostSummary(time.Time{}, time.Time{})

	// Should be categorized as pay_per_token
	ppt, ok := summary.ByBillingType["pay_per_token"]
	if !ok {
		t.Fatal("missing pay_per_token in ByBillingType")
	}
	// ChargedCost falls back to Cost (0.10)
	if !floatEq(ppt.Cost, 0.10, 0.0001) {
		t.Errorf("pay_per_token cost = %f, want 0.10 (fallback to Cost field)", ppt.Cost)
	}
	if ppt.Tokens != 1500 {
		t.Errorf("pay_per_token tokens = %d, want 1500", ppt.Tokens)
	}

	// ChargedCost total should use the fallback
	if !floatEq(summary.ChargedCost, 0.10, 0.0001) {
		t.Errorf("ChargedCost = %f, want 0.10", summary.ChargedCost)
	}
}

func TestCostSummaryBillingType_MultipleRecordsPerType(t *testing.T) {
	cs := makeCostStore(t)
	now := time.Now()

	cs.records = []CostRecord{
		{
			Timestamp:    now,
			Provider:     "openai",
			Model:        "gpt-4",
			PromptTokens: 1000,
			OutputTokens: 500,
			Cost:         0.10,
			BillingType:  "pay_per_token",
			ChargedCost:  0.10,
			TokenCost:    0.10,
		},
		{
			Timestamp:    now,
			Provider:     "anthropic",
			Model:        "claude-3",
			PromptTokens: 2000,
			OutputTokens: 1000,
			Cost:         0.20,
			BillingType:  "pay_per_token",
			ChargedCost:  0.20,
			TokenCost:    0.20,
		},
		{
			Timestamp:    now,
			Provider:     "zai-coding",
			Model:        "glm-4",
			PromptTokens: 3000,
			OutputTokens: 1500,
			Cost:         0,
			BillingType:  "subscription",
			ChargedCost:  0,
			TokenCost:    0.08,
		},
	}

	summary := cs.GetCostSummary(time.Time{}, time.Time{})

	// pay_per_token should aggregate both records
	ppt, ok := summary.ByBillingType["pay_per_token"]
	if !ok {
		t.Fatal("missing pay_per_token in ByBillingType")
	}
	if !floatEq(ppt.Cost, 0.30, 0.0001) {
		t.Errorf("pay_per_token cost = %f, want 0.30", ppt.Cost)
	}
	if ppt.Tokens != 4500 {
		t.Errorf("pay_per_token tokens = %d, want 4500", ppt.Tokens)
	}

	// subscription should have its own totals
	sub, ok := summary.ByBillingType["subscription"]
	if !ok {
		t.Fatal("missing subscription in ByBillingType")
	}
	if !floatEq(sub.Cost, 0.0, 0.0001) {
		t.Errorf("subscription cost = %f, want 0.0", sub.Cost)
	}
	if sub.Tokens != 4500 {
		t.Errorf("subscription tokens = %d, want 4500", sub.Tokens)
	}

	// Totals
	if !floatEq(summary.ChargedCost, 0.30, 0.0001) {
		t.Errorf("ChargedCost = %f, want 0.30", summary.ChargedCost)
	}
	if !floatEq(summary.TokenValue, 0.38, 0.0001) {
		t.Errorf("TokenValue = %f, want 0.38", summary.TokenValue)
	}
}

func TestCostSummaryBillingType_EmptyStore(t *testing.T) {
	cs := makeCostStore(t)

	summary := cs.GetCostSummary(time.Time{}, time.Time{})

	if summary.ChargedCost != 0 {
		t.Errorf("ChargedCost = %f, want 0", summary.ChargedCost)
	}
	if summary.TokenValue != 0 {
		t.Errorf("TokenValue = %f, want 0", summary.TokenValue)
	}
	if len(summary.ByBillingType) != 0 {
		t.Errorf("ByBillingType should be empty, got %v", summary.ByBillingType)
	}
}
