//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"syscall/js"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// runPlanFunc is runAgent with the planning-specific system prompt
// installed. Inputs mirror runAgent's. Returns the same Promise shape.
//
//	args[0] (string)  — provider name
//	args[1] (string)  — model id (or "" for default)
//	args[2] (string)  — initial planning query
//	args[3] (func?)   — onEvent callback
//
// Matches the behavior of the native `sprout plan` command's
// createPlanningAgent → ProcessQuery flow, minus the interactive readline
// loop (which doesn't apply in a browser). Host pages drive the
// multi-turn flow by calling runPlan again with the user's follow-up
// response, treating it like a chat thread.
//
// The planning prompt embeds the "create todos as you plan" behavior;
// disabling that knob isn't exposed here yet (always enabled to match
// the native default).
func runPlanFunc(_ js.Value, args []js.Value) interface{} {
	provider := argString(args, 0, "")
	model := argString(args, 1, "")
	query := argString(args, 2, "")

	var onEvent js.Value
	if len(args) > 3 && args[3].Type() == js.TypeFunction {
		onEvent = args[3]
	}

	return asPromiseWithTimeout(agentTimeout, func(ctx context.Context) (interface{}, error) {
		if provider == "" {
			return nil, fmt.Errorf("provider is required (first arg)")
		}
		if query == "" {
			return nil, fmt.Errorf("query is required (third arg)")
		}

		client, err := factory.CreateProviderClient(api.ClientType(provider), model)
		if err != nil {
			return nil, fmt.Errorf("create client: %w", err)
		}
		injectWasmStreamingClient(client)

		configMgr, err := configuration.NewManagerSilent()
		if err != nil {
			return nil, fmt.Errorf("init configuration: %w", err)
		}

		ag, err := agent.NewAgentWithClient(client, api.ClientType(provider), configMgr)
		if err != nil {
			return nil, fmt.Errorf("init agent: %w", err)
		}

		// Enable streaming so tokens appear in real-time (same as runAgentFunc).
		ag.SetStreamingEnabled(true)

		// Same ask_user wiring as runAgentFunc (see that function's comment).
		ag.InjectWebUIManagers(nil, wasmAskUserMgr)
		ag.SetHasActiveWebUIClients(func() bool { return true })

		planningPrompt, err := agent.GetEmbeddedPlanningPrompt(true)
		if err != nil {
			return nil, fmt.Errorf("load planning prompt: %w", err)
		}
		ag.SetSystemPrompt(planningPrompt)

		var unsubscribe func()
		if !onEvent.IsUndefined() && !onEvent.IsNull() {
			unsubscribe = wireAgentEventForwarding(ag, onEvent)
			defer unsubscribe()
		}

		response, err := ag.ProcessQuery(query)
		if err != nil {
			return nil, fmt.Errorf("process query: %w", err)
		}

		return map[string]interface{}{
			"response": response,
			"provider": provider,
			"model":    ag.GetModel(),
			"mode":     "plan",
		}, nil
	})
}
