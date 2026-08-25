package agent

import (
	"sync"

	"github.com/sprout-foundry/sprout/pkg/personas"
)

// AgentPersonaManager owns 4 sub-interfaces: PersonaStore, ToolGuidanceStore,
// FalseStopStore, and TaskActionStore.
type AgentPersonaManager struct {
	mu sync.RWMutex

	// PersonaStore
	activeSkills  []string
	activePersona string

	// ToolGuidanceStore
	toolCallGuidanceAdded bool

	// FalseStopStore
	falseStopDetectionEnabled bool

	// TaskActionStore — uses its own mutex (callers expect this specific mutex)
	taskActions   []TaskAction
	taskActionsMu sync.RWMutex
}

// NewAgentPersonaManager creates a new AgentPersonaManager with sensible defaults.
func NewAgentPersonaManager() *AgentPersonaManager {
	return &AgentPersonaManager{
		activeSkills:              []string{},
		activePersona:             personas.IDOrchestrator,
		falseStopDetectionEnabled: true,
	}
}

// PersonaStore
func (p *AgentPersonaManager) GetActiveSkills() []string {
	if p == nil {
		return nil
	}
	return p.activeSkills
}
func (p *AgentPersonaManager) SetActiveSkills(skills []string) {
	if p == nil {
		return
	}
	p.activeSkills = skills
}
func (p *AgentPersonaManager) GetActivePersona() string {
	if p == nil {
		return ""
	}
	return p.activePersona
}
func (p *AgentPersonaManager) SetActivePersona(persona string) {
	if p == nil {
		return
	}
	p.activePersona = persona
}

// ToolGuidanceStore
func (p *AgentPersonaManager) IsToolCallGuidanceAdded() bool {
	if p == nil {
		return false
	}
	return p.toolCallGuidanceAdded
}
func (p *AgentPersonaManager) SetToolCallGuidanceAdded(v bool) {
	if p == nil {
		return
	}
	p.toolCallGuidanceAdded = v
}

// FalseStopStore
func (p *AgentPersonaManager) IsFalseStopDetectionEnabled() bool {
	if p == nil {
		return false
	}
	return p.falseStopDetectionEnabled
}
func (p *AgentPersonaManager) SetFalseStopDetectionEnabled(v bool) {
	if p == nil {
		return
	}
	p.falseStopDetectionEnabled = v
}

// TaskActionStore — methods do NOT acquire taskActionsMu internally.
// Callers must acquire p.GetTaskActionsMutex() themselves.
func (p *AgentPersonaManager) GetTaskActions() []TaskAction {
	if p == nil {
		return nil
	}
	return p.taskActions
}
func (p *AgentPersonaManager) SetTaskActions(actions []TaskAction) {
	if p == nil {
		return
	}
	p.taskActions = actions
}
func (p *AgentPersonaManager) AddTaskAction(action TaskAction) {
	if p == nil {
		return
	}
	p.taskActions = append(p.taskActions, action)
}
func (p *AgentPersonaManager) GetTaskActionsMutex() *sync.RWMutex {
	if p == nil {
		return nil
	}
	return &p.taskActionsMu
}
