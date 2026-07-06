package agent

import (
	"context"
	"os"

	core "github.com/sprout-foundry/seed/core"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// convertHandlerToSeedToolConfig converts a ToolHandler into a seed
// core.ToolConfig, dispatching through the new interface-based handler
// path (h.Execute) rather than the legacy func-style handler closures.
//
// The seed handler closure:
//   - Creates a tools.ToolEnv from the agent
//   - Calls h.Execute(ctx, env, args)
//   - Converts tools.ToolResult back to (string, error) for the seed handler signature
//   - Applies the standard post-processing pipeline:
//     logToolExecution → h.Execute → handleToolError → postProcessResult
//     plus security block clearing on success
func convertHandlerToSeedToolConfig(h tools.ToolHandler, agent *Agent) core.ToolConfig {
	name := h.Name()
	def := h.Definition()

	// Map parameters
	requiredSet := make(map[string]struct{}, len(def.Required))
	for _, rn := range def.Required {
		requiredSet[rn] = struct{}{}
	}
	seedParams := make([]core.ParameterConfig, len(def.Parameters))
	for i, pd := range def.Parameters {
		req := pd.Required
		if !req {
			_, req = requiredSet[pd.Name]
		}
		seedParams[i] = core.ParameterConfig{
			Name:        pd.Name,
			Type:        pd.Type,
			Required:    req,
			Description: pd.Description,
		}
	}

	seed := core.ToolConfig{
		Name:            name,
		Description:     def.Description,
		Parameters:      seedParams,
		Aliases:         h.Aliases(),
		Timeout:         h.Timeout(),
		MaxResultSize:   h.MaxResultSize(),
		SafeForParallel: h.SafeForParallel(),
	}

	// Build the handler closure that dispatches through the new interface.
	handler := h // closure capture
	seed.Handler = func(ctx context.Context, args map[string]interface{}) (string, error) {
		logToolExecution(agent, name)

		// Build ToolEnv from agent context.
		env := buildToolEnvFromAgent(agent)

		handlerArgs := make(map[string]any, len(args))
		for k, v := range args {
			handlerArgs[k] = v
		}

		// Execute via the new interface-based handler.
		res, err := handler.Execute(ctx, env, handlerArgs)
		if err != nil {
			return handleToolError(agent, err, name)
		}

		// Handle tool-level error states (IsError flag).
		if res.IsError {
			errMsg := res.Output
			if errMsg == "" {
				errMsg = "tool returned error state"
			}
			return handleToolError(agent, toolsErr(errMsg), name)
		}

		// Success — clear the security block counter for this tool+args
		// so the circuit breaker only tracks *consecutive* failures.
		if agent != nil {
			agent.clearSecurityBlock(name, args)
		}

		return postProcessResult(ctx, agent, name, args, res.Output), nil
	}

	// Handle image-capable tools (like read_file for PDFs).
	seed.HandlerWithImages = func(ctx context.Context, args map[string]interface{}) ([]core.ImageData, string, error) {
		logToolExecution(agent, name)

		env := buildToolEnvFromAgent(agent)

		handlerArgs := make(map[string]any, len(args))
		for k, v := range args {
			handlerArgs[k] = v
		}

		res, err := handler.Execute(ctx, env, handlerArgs)
		if err != nil {
			msg, wrappedErr := handleToolError(agent, err, name)
			return nil, msg, wrappedErr
		}

		if res.IsError {
			errMsg := res.Output
			if errMsg == "" {
				errMsg = "tool returned error state"
			}
			msg, wrappedErr := handleToolError(agent, toolsErr(errMsg), name)
			return nil, msg, wrappedErr
		}

		if agent != nil {
			agent.clearSecurityBlock(name, args)
		}

		// Convert tools.ImageData to core.ImageData.
		var images []core.ImageData
		if len(res.Images) > 0 {
			images = make([]core.ImageData, len(res.Images))
			for i, img := range res.Images {
				images[i] = core.ImageData{
					URL:    img.URI,
					Base64: img.Base64,
					Type:   img.MIMEType,
				}
			}
		}

		return images, postProcessResult(ctx, agent, name, args, res.Output), nil
	}

	return seed
}

// buildToolEnvFromAgent constructs a tools.ToolEnv from an *Agent instance.
// Mirrors the ToolEnv construction in tool_security.go:ExecuteTool so that
// handler-dispatched tools get the same execution context regardless of
// whether they run via the legacy seed path or the new handler path.
func buildToolEnvFromAgent(agent *Agent) tools.ToolEnv {
	var env tools.ToolEnv
	if agent == nil {
		env.OutputWriter = os.Stdout
		env.MaxTokensFunc = func() int { return 0 }
		return env
	}

	env.EventBus = agent.GetEventBus()
	env.WorkspaceRoot = agent.effectiveCwd()
	// Gate on verbose mode — mirrors the gate in tool_security.go:ExecuteTool.
	// In default/compact mode, raw tool output is suppressed so the user
	// doesn't see read_file contents or full shell stdout dumped to terminal.
	if cfg := agent.GetConfig(); cfg != nil && cfg.OutputVerbosity == configuration.OutputVerbosityVerbose {
		env.OutputWriter = agent.OutputRouter()
	}
	env.Agent = agent
	env.MaxTokensFunc = func() int { return agent.GetMaxContextTokens() }
	env.ConfigManager = agent.GetConfigManager()
	env.AskUser = newAgentAskUserService(agent)
	env.TodoManager = agent.GetTodoManager()
	env.IsInteractiveCLI = !agent.HasActiveWebUIClients() && !isNonInteractive()
	env.ApprovalManager = newToolsApprovalAdapter(agent)
	env.EmbeddingMgr = agent.GetEmbeddingManager()
	env.VisionProcessor = agent.GetVisionProcessor()
	env.WebBrowser = tools.NewBrowserAdapter()
	env.SkillLoader = newSkillLoaderAdapter(agent)
	env.SearchEngine = newSearchEngineAdapter(agent)
	env.RawArgsJSON = "" // seed registry doesn't have raw JSON args
	env.Notifier = agent
	env.SubagentDepth = agent.subagentDepth
	return env
}

// toolsErr wraps a plain error string as a simple error for handleToolError.
func toolsErr(msg string) error {
	return &handlerToolError{msg: msg}
}

// handlerToolError is a simple error type for tool-level error states.
type handlerToolError struct {
	msg string
}

func (e *handlerToolError) Error() string { return e.msg }
