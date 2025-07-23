package editor

import (
	"encoding/json"
	"os"
)

const requirementsFile = ".ledit/requirements.json"

func loadOrchestrationPlan() (*OrchestrationPlan, error) {
	data, err := os.ReadFile(requirementsFile)
	if err != nil {
		return nil, err
	}
	var plan OrchestrationPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func saveOrchestrationPlan(plan *OrchestrationPlan) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(requirementsFile, data, 0644)
}
