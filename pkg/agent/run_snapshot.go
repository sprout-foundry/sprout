package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
)

type runSnapshot struct {
	RunID              string    `json:"run_id"`
	Timestamp          time.Time `json:"timestamp"`
	EditingModel       string    `json:"editing_model"`
	SummaryModel       string    `json:"summary_model"`
	OrchestrationModel string    `json:"orchestration_model"`
	PolicyVersion      string    `json:"policy_version"`
}

func WriteRunSnapshot(cfg *config.Config, runID string) error {
	if runID == "" {
		runID = fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	snap := runSnapshot{
		RunID:              runID,
		Timestamp:          time.Now(),
		EditingModel:       cfg.EditingModel,
		SummaryModel:       cfg.SummaryModel,
		OrchestrationModel: cfg.OrchestrationModel,
		PolicyVersion:      PolicyVersion,
	}
	if err := os.MkdirAll(".ledit", 0755); err != nil {
		return err
	}
	path := filepath.Join(".ledit", fmt.Sprintf("run_%s.json", runID))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(&snap)
}
