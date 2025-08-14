package orchestration

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/alantheprice/ledit/pkg/orchestration/types"
	ui "github.com/alantheprice/ledit/pkg/ui"
)

// LoadState loads an orchestration plan state from the given path
func LoadState(path string) (*types.MultiAgentOrchestrationPlan, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var plan types.MultiAgentOrchestrationPlan
	if err := json.Unmarshal(b, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

// PrintStateSummary prints a concise summary of the current orchestration state
func PrintStateSummary(plan *types.MultiAgentOrchestrationPlan) {
	if plan == nil {
		ui.Out().Print("No orchestration state loaded\n")
		return
	}
	ui.Out().Printf("Goal: %s\n", plan.Goal)
	ui.Out().Printf("Status: %s\n", plan.Status)
	ui.Out().Printf("Created: %s\n", plan.CreatedAt)
	if strings.TrimSpace(plan.CompletedAt) != "" {
		ui.Out().Printf("Completed: %s\n", plan.CompletedAt)
	}
	// Steps progress
	total := len(plan.Steps)
	completed := 0
	for _, s := range plan.Steps {
		if s.Status == "completed" {
			completed++
		}
	}
	ui.Out().Printf("Progress: %d/%d steps completed\n\n", completed, total)

	// Agents table
	type row struct {
		Name, Status, Step string
		Tokens             int
		Cost               float64
	}
	var rows []row
	for id, st := range plan.AgentStatuses {
		name := id
		for _, a := range plan.Agents {
			if a.ID == id && strings.TrimSpace(a.Name) != "" {
				name = a.Name
				break
			}
		}
		rows = append(rows, row{Name: name, Status: st.Status, Step: st.CurrentStep, Tokens: st.TokenUsage, Cost: st.Cost})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	ui.Out().Printf("%-24s %-12s %-22s %8s %10s\n", "Agent", "Status", "Current Step", "Tokens", "Cost($)")
	ui.Out().Printf("%s\n", strings.Repeat("-", 80))
	for _, r := range rows {
		ui.Out().Printf("%-24s %-12s %-22s %8d %10.4f\n", r.Name, r.Status, r.Step, r.Tokens, r.Cost)
	}
	ui.Out().Print("\n")
}
