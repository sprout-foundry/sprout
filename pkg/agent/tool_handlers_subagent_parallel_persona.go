package agent

import (
	"context"
	"fmt"
	"path/filepath"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// resolveParallelTaskPersonas resolves persona, provider, model, and system
// prompt for every parallel task, applying the same spawnability checks and
// default persona as run_subagent. Reviewer tasks get the working-tree change
// context prepended; it is captured once and shared so every reviewer in a
// split review sees the same snapshot.
func resolveParallelTaskPersonas(ctx context.Context, a *Agent, tasks []SubagentTask) error {
	if len(tasks) == 0 {
		return nil
	}
	workspaceRoot, err := filepath.Abs(a.currentWorkspaceRoot())
	if err != nil {
		return agenterrors.NewConfig("failed to resolve absolute workspace path", err)
	}

	var reviewContext *string
	for i := range tasks {
		explicit := tasks[i].Persona != ""
		persona, _, _ := parseSubagentPersona(a, map[string]interface{}{"persona": tasks[i].Persona})
		if explicit && a.GetConfig() != nil && a.GetConfig().GetSubagentType(persona) == nil {
			return agenterrors.NewValidation(fmt.Sprintf("task %s: unknown or disabled persona %q", tasks[i].ID, tasks[i].Persona), nil)
		}
		provider, model, systemPrompt, err := resolveSubagentProviderModel(a, persona, explicit, workspaceRoot)
		if err != nil {
			return agenterrors.Wrap(err, fmt.Sprintf("task %s", tasks[i].ID))
		}
		tasks[i].Persona = persona
		tasks[i].Provider = provider
		tasks[i].Model = model
		tasks[i].SystemPrompt = systemPrompt

		if isReviewerPersona(a, persona) {
			if reviewContext == nil {
				rc := buildReviewerChangeContext(ctx, workspaceRoot)
				reviewContext = &rc
			}
			if *reviewContext != "" {
				tasks[i].Prompt = *reviewContext + "# Your Task\n\n" + tasks[i].Prompt
			}
		}
	}
	return nil
}
