//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// handleCostsSummary returns cost summary data
func (ws *ReactWebServer) handleCostsSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	costStore := GetCostStore()

	// Parse optional date range (matching /api/costs/detail pattern)
	var startDate, endDate time.Time
	if start := r.URL.Query().Get("start_date"); start != "" {
		if t, err := time.Parse("2006-01-02", start); err == nil {
			startDate = t
		}
	}
	if end := r.URL.Query().Get("end_date"); end != "" {
		if t, err := time.Parse("2006-01-02", end); err == nil {
			endDate = t
		}
	}

	summary := costStore.GetCostSummary(startDate, endDate)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// handleCostsHistory returns historical cost data
func (ws *ReactWebServer) handleCostsHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	costStore := GetCostStore()

	// Parse query params
	days := 30 // default to last 30 days
	if d := r.URL.Query().Get("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			days = parsed
		}
	}

	dailyCosts := costStore.GetDailyCosts(days)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"daily_costs": dailyCosts,
		"days":        days,
	})
}

// handleCostsDetail returns detailed cost records
func (ws *ReactWebServer) handleCostsDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	costStore := GetCostStore()

	// Parse date range
	now := time.Now()
	startDate := now.AddDate(0, 0, -30) // default 30 days

	if start := r.URL.Query().Get("start_date"); start != "" {
		if t, err := time.Parse("2006-01-02", start); err == nil {
			startDate = t
		}
	}
	if end := r.URL.Query().Get("end_date"); end != "" {
		if t, err := time.Parse("2006-01-02", end); err == nil {
			now = t
		}
	}

	totalCost, byProvider, byModel := costStore.GetSummary(startDate, now)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_cost":  totalCost,
		"by_provider": byProvider,
		"by_model":    byModel,
		"start_date":  startDate.Format("2006-01-02"),
		"end_date":    now.Format("2006-01-02"),
	})
}
