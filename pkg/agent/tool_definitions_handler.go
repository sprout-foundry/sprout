package agent

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"

	core "github.com/sprout-foundry/seed/core"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// BuildToolConfigsFromHandlers produces a canonical list of ToolConfig entries
// derived from the new handler registry (tools.GetNewToolRegistry().All()).
// Each handler is converted to a ToolConfig via convertHandlerToToolConfig.
// The result is sorted alphabetically by name for deterministic output.
func BuildToolConfigsFromHandlers() []ToolConfig {
	allHandlers := tools.GetNewToolRegistry().All()
	result := make([]ToolConfig, 0, len(allHandlers))
	for _, h := range allHandlers {
		result = append(result, convertHandlerToToolConfig(h))
	}
	slices.SortFunc(result, func(a, b ToolConfig) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return result
}

// convertHandlerToToolConfig converts a single ToolHandler into a
// ToolConfig (the LLM-facing tool definition used by the legacy registry).
func convertHandlerToToolConfig(h tools.ToolHandler) ToolConfig {
	def := h.Definition()

	// Map parameters from ToolDefinition (which has a separate Required []string
	// field) to ParameterConfig (which has Required bool per parameter).
	params := make([]ParameterConfig, len(def.Parameters))
	requiredSet := make(map[string]struct{}, len(def.Required))
	for _, rn := range def.Required {
		requiredSet[rn] = struct{}{}
	}
	for i, pd := range def.Parameters {
		req := pd.Required
		if !req {
			_, req = requiredSet[pd.Name]
		}
		params[i] = ParameterConfig{
			Name:        pd.Name,
			Type:        pd.Type,
			Required:    req,
			Description: pd.Description,
		}
	}

	return ToolConfig{
		Name:            h.Name(),
		Description:     def.Description,
		Parameters:      params,
		Aliases:         h.Aliases(),
		Timeout:         h.Timeout(),
		MaxResultSize:   h.MaxResultSize(),
		SafeForParallel: h.SafeForParallel(),
		Interactive:     h.Interactive(),
	}
}

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

		// Convert args from map[string]interface{} to map[string]any
		// (they're the same type but be explicit for the call).
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
			}}
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
	env.OutputWriter = agent.OutputRouter()
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

// compareToolConfigFields compares two ToolConfig entries and returns a list
// of fields that differ. Used by SP-109 verification to log diffs between
// legacy and handler-derived tool definitions.
func compareToolConfigFields(oldCfg, newCfg ToolConfig) []string {
	var diffs []string

	if oldCfg.Description != newCfg.Description {
		diffs = append(diffs, "Description")
	}
	if oldCfg.Interactive != newCfg.Interactive {
		diffs = append(diffs, "Interactive")
	}
	if oldCfg.SafeForParallel != newCfg.SafeForParallel {
		diffs = append(diffs, "SafeForParallel")
	}
	if oldCfg.Timeout != newCfg.Timeout {
		diffs = append(diffs, "Timeout")
	}
	if oldCfg.MaxResultSize != newCfg.MaxResultSize {
		diffs = append(diffs, "MaxResultSize")
	}

	// Compare aliases
	if !stringSliceEqual(oldCfg.Aliases, newCfg.Aliases) {
		diffs = append(diffs, "Aliases")
	}

	// Compare parameters
	if !parameterConfigsEqual(oldCfg.Parameters, newCfg.Parameters) {
		diffs = append(diffs, "Parameters")
	}

	return diffs
}

// compareSeedToolConfigFields compares two core.ToolConfig entries and
// returns a list of fields that differ.
func compareSeedToolConfigFields(oldCfg, newCfg core.ToolConfig) []string {
	var diffs []string

	if oldCfg.Description != newCfg.Description {
		diffs = append(diffs, "Description")
	}
	if oldCfg.SafeForParallel != newCfg.SafeForParallel {
		diffs = append(diffs, "SafeForParallel")
	}
	if oldCfg.Timeout != newCfg.Timeout {
		diffs = append(diffs, "Timeout")
	}
	if oldCfg.MaxResultSize != newCfg.MaxResultSize {
		diffs = append(diffs, "MaxResultSize")
	}

	if !stringSliceEqual(oldCfg.Aliases, newCfg.Aliases) {
		diffs = append(diffs, "Aliases")
	}

	if !seedParameterConfigsEqual(oldCfg.Parameters, newCfg.Parameters) {
		diffs = append(diffs, "Parameters")
	}

	return diffs
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	// Aliases are semantically a set — order shouldn't matter.
	// Sort copies so callers can reuse the original slices.
	a = slices.Clone(a)
	b = slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func parameterConfigsEqual(a, b []ParameterConfig) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Type != b[i].Type ||
			a[i].Required != b[i].Required || a[i].Description != b[i].Description {
			return false
		}
	}
	return true
}

func seedParameterConfigsEqual(a, b []core.ParameterConfig) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Type != b[i].Type ||
			a[i].Required != b[i].Required || a[i].Description != b[i].Description {
			return false
		}
	}
	return true
}

// handlerToolConfigs returns a map of ToolConfig entries derived from the
// handler registry, keyed by tool name.
func handlerToolConfigs() map[string]ToolConfig {
	result := make(map[string]ToolConfig)
	for _, cfg := range BuildToolConfigsFromHandlers() {
		result[cfg.Name] = cfg
	}
	return result
}

// seedToolConfigsFromHandlers returns a map of seed core.ToolConfig entries
// derived from the handler registry, keyed by tool name.
func seedToolConfigsFromHandlers(agent *Agent) map[string]core.ToolConfig {
	result := make(map[string]core.ToolConfig)
	for _, h := range tools.GetNewToolRegistry().All() {
		result[h.Name()] = convertHandlerToSeedToolConfig(h, agent)
	}
	return result
}

// seedToolConfigsFromLegacy returns a map of seed core.ToolConfig entries
// derived from the legacy ToolConfig registry, keyed by tool name.
func seedToolConfigsFromLegacy(agent *Agent) map[string]core.ToolConfig {
	result := make(map[string]core.ToolConfig)
	for _, cfg := range GetToolRegistry().GetAllToolConfigs() {
		result[cfg.Name] = convertToSeedToolConfig(cfg, agent)
	}
	return result
}

// sp109UseHandlerTools reports whether the SP109_USE_HANDLER_TOOLS env var
// is set to "true". When enabled, the seed tool registry uses handler-derived
// tool definitions instead of the legacy ToolConfig registry.
func sp109UseHandlerTools() bool {
	return os.Getenv("SP109_USE_HANDLER_TOOLS") == "true"
}

// sp109VerifyAndLog compares seed tool configs from both sources (legacy and
// handler-derived) and logs any differences to stderr. Returns the count of
// tools compared, matched, and differing.
func sp109VerifyAndLog(agent *Agent) (compared, matched, differing int) {
	legacyCfgs := seedToolConfigsFromLegacy(agent)
	handlerCfgs := seedToolConfigsFromHandlers(agent)

	for name, legacyCfg := range legacyCfgs {
		handlerCfg, ok := handlerCfgs[name]
		if !ok {
			continue // tool only in legacy, skip
		}
		compared++
		diffs := compareSeedToolConfigFields(legacyCfg, handlerCfg)
		if len(diffs) == 0 {
			matched++
		} else {
			differing++
			for _, field := range diffs {
				var oldVal, newVal string
				switch field {
				case "Description":
					oldVal = legacyCfg.Description
					newVal = handlerCfg.Description
				case "Timeout":
					oldVal = legacyCfg.Timeout.String()
					newVal = handlerCfg.Timeout.String()
				case "MaxResultSize":
					oldVal = strconv.Itoa(legacyCfg.MaxResultSize)
					newVal = strconv.Itoa(handlerCfg.MaxResultSize)
				case "SafeForParallel":
					oldVal = strconv.FormatBool(legacyCfg.SafeForParallel)
					newVal = strconv.FormatBool(handlerCfg.SafeForParallel)
				case "Aliases":
					oldVal = fmt.Sprintf("%v", legacyCfg.Aliases)
					newVal = fmt.Sprintf("%v", handlerCfg.Aliases)
				case "Parameters":
					oldVal = fmt.Sprintf("%d params", len(legacyCfg.Parameters))
					newVal = fmt.Sprintf("%d params", len(handlerCfg.Parameters))
				}
				fmt.Fprintf(os.Stderr, "SP-109 DIFF tool=%q field=%q: new=%q, old=%q\n",
					name, field, newVal, oldVal)
			}
		}
	}
	return compared, matched, differing
}
