//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// setCostStoreForTest replaces the global cost store with a test instance
// and pins it so that GetCostStore() returns our instance.
// Must be called before NewReactWebServer() to prevent the real store from loading.
func setCostStoreForTest(t *testing.T, cs *CostStore) {
	t.Helper()
	// Save original state for cleanup
	origStore := costStore
	// Set the global directly so GetCostStore() returns it
	costStore = cs
	// Reset the Once by creating a new one (cannot copy sync.Once).
	costStoreOnce = sync.Once{}
	costStoreOnce.Do(func() {}) // consume the Once
	// Restore on cleanup
	t.Cleanup(func() {
		costStore = origStore
		costStoreOnce = sync.Once{}
	})
}

// makeTestCostStore creates a CostStore with a temp file and given records.
func makeTestCostStore(t *testing.T, records []CostRecord) *CostStore {
	t.Helper()
	cs := &CostStore{
		filePath:    t.TempDir() + "/costs.json",
		lastPersist: time.Now(),
		records:     records,
	}
	return cs
}

func TestHandleCostsSummary(t *testing.T) {
	// Set up isolated cost store before creating server
	cs := makeTestCostStore(t, []CostRecord{
		{Timestamp: time.Now(), Provider: "openai", Model: "gpt-4", Cost: 0.05},
		{Timestamp: time.Now(), Provider: "anthropic", Model: "claude", Cost: 0.10},
	})
	setCostStoreForTest(t, cs)
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("non-GET returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/costs/summary", nil)
		rec := httptest.NewRecorder()

		server.handleCostsSummary(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected status 405, got %d", rec.Code)
		}
	})

	t.Run("GET returns summary", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/costs/summary", nil)
		rec := httptest.NewRecorder()

		server.handleCostsSummary(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var summary CostSummary
		if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		if !floatEq(summary.TotalCost, 0.15, 0.0001) {
			t.Errorf("totalCost = %f, want 0.15", summary.TotalCost)
		}
		if len(summary.ByProvider) == 0 {
			t.Error("expected non-empty by_provider")
		}
		if len(summary.ByModel) == 0 {
			t.Error("expected non-empty by_model")
		}
	})
}

func TestHandleCostsHistory(t *testing.T) {
	now := time.Now()
	// Set up isolated cost store before creating server
	cs := makeTestCostStore(t, []CostRecord{
		{Timestamp: now, Provider: "openai", Model: "gpt-4", Cost: 0.05},
		{Timestamp: now.AddDate(0, 0, -1), Provider: "openai", Model: "gpt-4", Cost: 0.03},
		{Timestamp: now.AddDate(0, 0, -31), Provider: "openai", Model: "gpt-4", Cost: 0.99},
	})
	setCostStoreForTest(t, cs)
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("non-GET returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/costs/history", nil)
		rec := httptest.NewRecorder()

		server.handleCostsHistory(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected status 405, got %d", rec.Code)
		}
	})

	t.Run("GET with days param", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/costs/history?days=1", nil)
		rec := httptest.NewRecorder()

		server.handleCostsHistory(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		if days, ok := resp["days"].(float64); !ok || days != 1 {
			t.Errorf("expected days=1, got %v", resp["days"])
		}
	})

	t.Run("GET default 30 days", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/costs/history", nil)
		rec := httptest.NewRecorder()

		server.handleCostsHistory(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		if days, ok := resp["days"].(float64); !ok || days != 30 {
			t.Errorf("expected days=30, got %v", resp["days"])
		}

		dailyCosts, ok := resp["daily_costs"].([]interface{})
		if !ok {
			t.Fatal("expected daily_costs to be an array")
		}
		// We have records for today and yesterday only (the -31 day record is outside range)
		if len(dailyCosts) != 2 {
			t.Errorf("expected 2 daily entries, got %d", len(dailyCosts))
		}
	})
}

func TestHandleCostsDetail(t *testing.T) {
	now := time.Now()
	// Set up isolated cost store before creating server
	cs := makeTestCostStore(t, []CostRecord{
		{Timestamp: now, Provider: "openai", Model: "gpt-4", Cost: 0.05},
		{Timestamp: now.AddDate(0, 0, -1), Provider: "anthropic", Model: "claude", Cost: 0.10},
	})
	setCostStoreForTest(t, cs)
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("non-GET returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/costs/detail", nil)
		rec := httptest.NewRecorder()

		server.handleCostsDetail(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected status 405, got %d", rec.Code)
		}
	})

	t.Run("GET with start_date and end_date", func(t *testing.T) {
		yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
		today := now.Format("2006-01-02")
		req := httptest.NewRequest(http.MethodGet, "/api/costs/detail?start_date="+yesterday+"&end_date="+today, nil)
		rec := httptest.NewRecorder()

		server.handleCostsDetail(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		if startDate, ok := resp["start_date"].(string); !ok || startDate != yesterday {
			t.Errorf("expected start_date=%s, got %v", yesterday, resp["start_date"])
		}
		if endDate, ok := resp["end_date"].(string); !ok || endDate != today {
			t.Errorf("expected end_date=%s, got %v", today, resp["end_date"])
		}
	})

	t.Run("GET default 30 days", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/costs/detail", nil)
		rec := httptest.NewRecorder()

		server.handleCostsDetail(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		total, ok := resp["total_cost"].(float64)
		if !ok {
			t.Fatalf("expected total_cost to be a number, got %v", resp["total_cost"])
		}
		if !floatEq(total, 0.15, 0.0001) {
			t.Errorf("expected total_cost=0.15, got %v", total)
		}

		if _, ok := resp["by_provider"].(map[string]interface{}); !ok {
			t.Error("expected by_provider to be an object")
		}
		if _, ok := resp["by_model"].(map[string]interface{}); !ok {
			t.Error("expected by_model to be an object")
		}
	})
}
