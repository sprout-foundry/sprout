package playbooks

// PlanOp is a language-agnostic operation spec
type PlanOp struct {
	FilePath           string
	Description        string
	Instructions       string
	ScopeJustification string
}

// PlanSpec is a language-agnostic plan spec
type PlanSpec struct {
	Files []string
	Ops   []PlanOp
	Scope string
}

// Playbook defines an intent→plan strategy without importing agent types
type Playbook interface {
	Name() string
	Matches(userIntent string, category string) bool
	BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec
}

// ContextAware lets a playbook declare if it requires broader workspace context.
// If not implemented, the agent will assume context is required.
type ContextAware interface {
	RequiresContext() bool
}

var registry []Playbook

// Register adds a playbook to the registry
func Register(pb Playbook) { registry = append(registry, pb) }

// Select returns the first matching playbook
func Select(userIntent string, category string) Playbook {
	for _, pb := range registry {
		if pb.Matches(userIntent, category) {
			return pb
		}
	}
	return nil
}
