package orchestration

import (
	"encoding/json"
	"os"

	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/orchestration/types"
)

const requirementsFile = ".ledit/requirements.json"

// LoadOrchestrationPlan loads the orchestration plan from the requirements file.
func LoadOrchestrationPlan() (*types.OrchestrationPlan, error) { // MODIFIED: Exported function
	// filesystem.ReadFile is assumed to return a string, based on the compilation error.
	// json.Unmarshal expects a []byte, so we convert the string to []byte.
	data, err := filesystem.ReadFile(requirementsFile)
	if err != nil {
		return nil, err
	}
	var plan types.OrchestrationPlan
	if err := json.Unmarshal([]byte(data), &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

// SaveOrchestrationPlan saves the orchestration plan to the requirements file.
func SaveOrchestrationPlan(plan *types.OrchestrationPlan) error { // MODIFIED: Exported function
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(requirementsFile, data, 0644)
}