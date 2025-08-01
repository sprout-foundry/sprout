package editor

import (
	"encoding/json"
	"os"

	"github.com/alantheprice/ledit/pkg/orchestration/types" // NEW IMPORT: Import orchestration types
)

const requirementsFile = ".ledit/requirements.json"

func loadOrchestrationPlan() (*types.OrchestrationPlan, error) {
	data, err := os.ReadFile(requirementsFile)
	if err != nil {
		return nil, err
	}
	var plan types.OrchestrationPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func saveOrchestrationPlan(plan *types.OrchestrationPlan) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(requirementsFile, data, 0644)
}
