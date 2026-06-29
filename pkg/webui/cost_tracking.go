//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// CostRecord represents a single cost entry for an API request
type CostRecord struct {
	Timestamp    time.Time `json:"timestamp"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	PromptTokens int       `json:"prompt_tokens"`
	OutputTokens int       `json:"output_tokens"`
	Cost         float64   `json:"cost"`
	SessionID    string    `json:"session_id,omitempty"`
	ChatID       string    `json:"chat_id,omitempty"`
	// Optional session metadata populated at record time.
	Title       string `json:"title,omitempty"`
	WorkingDir  string `json:"working_dir,omitempty"`
	LastUpdated string `json:"last_updated,omitempty"` // RFC3339 timestamp
}

// CostStore handles persisting and querying cost records
type CostStore struct {
	mu          sync.RWMutex
	persistMu   sync.Mutex
	records     []CostRecord
	filePath    string
	lastPersist time.Time
}

// Global cost store instance with thread-safe initialization
var (
	costStore     *CostStore
	costStoreOnce sync.Once
)

// GetCostStore returns the singleton cost store instance
func GetCostStore() *CostStore {
	costStoreOnce.Do(func() {
		configDir, err := configuration.GetConfigDir()
		if err != nil {
			log.Printf("coststore: failed to get config dir: %v", err)
			// Fallback to home directory
			homeDir, _ := os.UserHomeDir()
			configDir = filepath.Join(homeDir, ".sprout")
		}
		costStore = &CostStore{
			filePath: filepath.Join(configDir, "cost_history.json"),
		}
		if err := costStore.load(); err != nil {
			log.Printf("coststore: failed to load existing records: %v", err)
		}
	})
	return costStore
}

// RecordCost adds a new cost record
func (cs *CostStore) RecordCost(provider, model, sessionID, chatID string, promptTokens, outputTokens int, cost float64) {
	cs.RecordCostWithSession(provider, model, sessionID, chatID, "", "", promptTokens, outputTokens, cost)
}

// RecordCostWithSession adds a new cost record with optional session metadata.
func (cs *CostStore) RecordCostWithSession(provider, model, sessionID, chatID, title, workingDir string, promptTokens, outputTokens int, cost float64) {
	if cost <= 0 {
		return
	}
	record := CostRecord{
		Timestamp:    time.Now(),
		Provider:     provider,
		Model:        model,
		PromptTokens: promptTokens,
		OutputTokens: outputTokens,
		Cost:         cost,
		SessionID:    sessionID,
		ChatID:       chatID,
		Title:        title,
		WorkingDir:   workingDir,
		LastUpdated:  time.Now().Format(time.RFC3339),
	}

	cs.mu.Lock()
	cs.records = append(cs.records, record)

	// Persist every 10 records or every 30 seconds
	if len(cs.records)%10 == 0 || time.Since(cs.lastPersist) > 30*time.Second {
		recordsCopy := make([]CostRecord, len(cs.records))
		copy(recordsCopy, cs.records)
		cs.mu.Unlock()
		go cs.persistRecords(recordsCopy)
	} else {
		cs.mu.Unlock()
	}
}

// GetSummary returns cost summary for a date range
func (cs *CostStore) GetSummary(startDate, endDate time.Time) (totalCost float64, byProvider map[string]float64, byModel map[string]float64) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	byProvider = make(map[string]float64)
	byModel = make(map[string]float64)

	for _, r := range cs.records {
		if r.Timestamp.After(startDate) && r.Timestamp.Before(endDate.Add(24*time.Hour)) {
			totalCost += r.Cost
			byProvider[r.Provider] += r.Cost
			key := r.Provider + ":" + r.Model
			byModel[key] += r.Cost
		}
	}
	return
}

// GetDailyCosts returns daily cost breakdown
func (cs *CostStore) GetDailyCosts(days int) []DailyCost {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -days)

	dailyMap := make(map[string]DailyCost)

	for _, r := range cs.records {
		if r.Timestamp.After(startDate) {
			dateKey := r.Timestamp.Format("2006-01-02")
			if dc, ok := dailyMap[dateKey]; ok {
				dc.TotalCost += r.Cost
				dc.ByProvider[r.Provider] += r.Cost
				dailyMap[dateKey] = dc
			} else {
				dailyMap[dateKey] = DailyCost{
					Date:       dateKey,
					TotalCost:  r.Cost,
					ByProvider: map[string]float64{r.Provider: r.Cost},
				}
			}
		}
	}

	result := make([]DailyCost, 0, len(dailyMap))
	for _, dc := range dailyMap {
		result = append(result, dc)
	}
	return result
}

// DailyCost represents cost for a single day
type DailyCost struct {
	Date       string             `json:"date"`
	TotalCost  float64            `json:"total_cost"`
	ByProvider map[string]float64 `json:"by_provider,omitempty"`
}

// SessionCostRow represents a single session's aggregated cost data
type SessionCostRow struct {
	SessionID   string  `json:"session_id"`
	Title       string  `json:"title"`
	WorkingDir  string  `json:"working_dir"`
	TotalCost   float64 `json:"total_cost"`
	LastUpdated string  `json:"last_updated"` // RFC3339 timestamp
}

// CostSummary represents aggregated cost data
type CostSummary struct {
	TotalCost            float64            `json:"total_cost"`
	ByProvider           map[string]float64 `json:"by_provider"`
	ByModel              map[string]float64 `json:"by_model"`
	ByProviderThisMonth  map[string]float64 `json:"by_provider_this_month"`
	ByProviderLastMonth  map[string]float64 `json:"by_provider_last_month"`
	Last30Days           float64            `json:"last_30_days"`
	Last7Days            float64            `json:"last_7_days"`
	ThisMonth            float64            `json:"this_month"`
	LastMonth            float64            `json:"last_month"`
	TopSessions          []SessionCostRow   `json:"top_sessions"`
}

// GetCostSummary returns overall cost summary.
// When start and end are both zero, all records are included (all-time).
// When start/end are set, TopSessions is filtered to that range.
func (cs *CostStore) GetCostSummary(start, end time.Time) CostSummary {
	now := time.Now()
	summary := CostSummary{
		ByProvider:           make(map[string]float64),
		ByModel:              make(map[string]float64),
		ByProviderThisMonth:  make(map[string]float64),
		ByProviderLastMonth:  make(map[string]float64),
	}

	// Get last 30 days
	start30 := now.AddDate(0, 0, -30)
	start7 := now.AddDate(0, 0, -7)
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	startOfLastMonth := startOfMonth.AddDate(0, -1, 1)

	// Check if a date range was requested
	hasRange := !start.IsZero() && !end.IsZero()

	cs.mu.RLock()
	defer cs.mu.RUnlock()

	// Per-session aggregation for TopSessions
	type sessionAccum struct {
		totalCost  float64
		title      string
		workingDir string
		lastUpdate string
	}
	sessionMap := make(map[string]*sessionAccum)

	for _, r := range cs.records {
		// Skip records outside the requested range for TopSessions
		inRange := true
		if hasRange {
			if !r.Timestamp.After(start) || !r.Timestamp.Before(end.Add(24 * time.Hour)) {
				inRange = false
			}
		}

		if inRange && r.SessionID != "" {
			acc, ok := sessionMap[r.SessionID]
			if !ok {
				acc = &sessionAccum{}
				sessionMap[r.SessionID] = acc
			}
			acc.totalCost += r.Cost
			if acc.title == "" && r.Title != "" {
				acc.title = r.Title
			}
			if acc.workingDir == "" && r.WorkingDir != "" {
				acc.workingDir = r.WorkingDir
			}
			if r.LastUpdated != "" && (acc.lastUpdate == "" || r.LastUpdated > acc.lastUpdate) {
				acc.lastUpdate = r.LastUpdated
			} else if acc.lastUpdate == "" {
				acc.lastUpdate = r.Timestamp.Format(time.RFC3339)
			}
		}

		// Always add to totals (all-time)
		summary.TotalCost += r.Cost
		summary.ByProvider[r.Provider] += r.Cost
		key := r.Provider + ":" + r.Model
		summary.ByModel[key] += r.Cost

		// Last 30 days
		if r.Timestamp.After(start30) {
			summary.Last30Days += r.Cost
		}
		// Last 7 days
		if r.Timestamp.After(start7) {
			summary.Last7Days += r.Cost
		}
		// This month
		if r.Timestamp.After(startOfMonth) {
			summary.ThisMonth += r.Cost
			summary.ByProviderThisMonth[r.Provider] += r.Cost
		}
		// Last month
		if r.Timestamp.After(startOfLastMonth) && r.Timestamp.Before(startOfMonth) {
			summary.LastMonth += r.Cost
			summary.ByProviderLastMonth[r.Provider] += r.Cost
		}
	}

	// Build TopSessions: sort by cost desc, take top 10
	summary.TopSessions = make([]SessionCostRow, 0, len(sessionMap))
	for sid, acc := range sessionMap {
		summary.TopSessions = append(summary.TopSessions, SessionCostRow{
			SessionID:   sid,
			Title:       acc.title,
			WorkingDir:  acc.workingDir,
			TotalCost:   acc.totalCost,
			LastUpdated: acc.lastUpdate,
		})
	}
	// Sort descending by cost
	sort.Slice(summary.TopSessions, func(i, j int) bool {
		return summary.TopSessions[i].TotalCost > summary.TopSessions[j].TotalCost
	})
	// Cap at 10
	if len(summary.TopSessions) > 10 {
		summary.TopSessions = summary.TopSessions[:10]
	}

	return summary
}

// persistRecords saves the given records to disk atomically using temp file + rename.
// The caller is responsible for providing a snapshot of the records; this method
// acquires cs.mu only briefly to update lastPersist after a successful write.
func (cs *CostStore) persistRecords(records []CostRecord) error {
	cs.persistMu.Lock()
	defer cs.persistMu.Unlock()

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	// Write to temp file first, then rename atomically to avoid
	// data loss if the process crashes mid-write.
	dir := filepath.Dir(cs.filePath)
	tmpFile, err := os.CreateTemp(dir, "cost_history_*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, cs.filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	cs.mu.Lock()
	cs.lastPersist = time.Now()
	cs.mu.Unlock()
	return nil
}

// load loads records from disk
func (cs *CostStore) load() error {
	data, err := os.ReadFile(cs.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No file yet
		}
		return err
	}

	if err := json.Unmarshal(data, &cs.records); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	cs.lastPersist = time.Now()
	return nil
}

// ForcePersist forces immediate persistence (for graceful shutdown)
func (cs *CostStore) ForcePersist() error {
	cs.mu.Lock()
	recordsCopy := make([]CostRecord, len(cs.records))
	copy(recordsCopy, cs.records)
	cs.mu.Unlock()
	return cs.persistRecords(recordsCopy)
}
