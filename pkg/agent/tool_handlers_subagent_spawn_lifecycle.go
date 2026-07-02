// Subagent spawn lifecycle helpers: args parsing, working_dir validation,
// persona parsing, and enhanced prompt building.
//
// Extracted from tool_handlers_subagent_spawn.go as part of SP-075's
// large-file decomposition.

package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// subagentLaunchSpec holds all the pre-run state needed to dispatch a
// single subagent. Populated by prepareSubagentLaunch and consumed by
// the remainder of handleRunSubagent.
type subagentLaunchSpec struct {
	prompt                    string
	context                   string
	files                     []string
	filesStr                  string
	workingDir                string
	persona                   string
	personaExplicitlyProvided bool
	provider                  string
	model                     string
	systemPromptText          string
	enhancedPrompt            string
	subagentWorkspaceRoot     string
}

// prepareSubagentLaunch parses the raw args map, validates paths and
// workspace access, resolves the persona's provider/model, loads any
// system prompt file, and builds the enhanced prompt with context +
// file contents. Returns a subagentLaunchSpec ready for dispatch.
func prepareSubagentLaunch(ctx context.Context, a *Agent, args map[string]interface{}) (*subagentLaunchSpec, error) {
	spec := &subagentLaunchSpec{}

	// --- Parse prompt (required) ---
	var err error
	spec.prompt, err = convertToString(args["prompt"], "prompt")
	if err != nil {
		return nil, agenterrors.NewValidation(fmt.Sprintf("failed to convert prompt parameter: %v", err), nil)
	}
	a.Logger().Debug("Spawning subagent with task: %s\n", truncateString(spec.prompt, 100))

	// --- Parse optional context ---
	if ctxVal, ok := args["context"]; ok && ctxVal != nil {
		if ctxStr, ok := ctxVal.(string); ok && ctxStr != "" {
			spec.context = ctxStr
			a.Logger().Debug("Subagent context provided: %s\n", truncateString(spec.context, 100))
		}
	}

	// --- Parse optional files ---
	if filesVal, ok := args["files"]; ok && filesVal != nil {
		if filesRaw, ok := filesVal.(string); ok && filesRaw != "" {
			rawFiles := strings.Split(filesRaw, ",")
			for _, f := range rawFiles {
				if f = strings.TrimSpace(f); f != "" {
					spec.files = append(spec.files, f)
				}
			}
			spec.filesStr = strings.Join(spec.files, ",")
			a.Logger().Debug("Subagent files provided: %s\n", spec.filesStr)
		}
	}

	// --- Parse optional working_dir ---
	if wdVal, ok := args["working_dir"]; ok && wdVal != nil {
		if wdStr, ok := wdVal.(string); ok && wdStr != "" {
			spec.workingDir = wdStr
			a.Logger().Debug("Subagent working_dir specified: %s\n", spec.workingDir)
		}
	}

	// --- Validate working_dir ---
	spec.workingDir, err = validateWorkingDir(spec.workingDir, a)
	if err != nil {
		return nil, err
	}

	// --- Parse persona ---
	spec.persona, spec.personaExplicitlyProvided, err = parseSubagentPersona(a, args)
	if err != nil {
		return nil, err
	}

	// --- Resolve workspace root ---
	absWorkspaceDir, err := filepath.Abs(a.currentWorkspaceRoot())
	if err != nil {
		return nil, agenterrors.NewConfig("failed to resolve absolute workspace path", err)
	}

	// --- Validate file paths ---
	absFilePaths, outsidePaths, err := validateFilePaths(spec.files, absWorkspaceDir, a)
	if err != nil {
		return nil, err
	}

	// --- Approve external workspace access if needed ---
	spec.subagentWorkspaceRoot = absWorkspaceDir
	if len(outsidePaths) > 0 {
		spec.subagentWorkspaceRoot, err = approveExternalWorkspace(a, outsidePaths, absFilePaths)
		if err != nil {
			return nil, err
		}
	}

	// --- Override workspace root with working_dir if specified ---
	spec.subagentWorkspaceRoot = overrideWorkspaceRoot(spec.subagentWorkspaceRoot, spec.workingDir, absFilePaths, a)

	// --- Build enhanced prompt ---
	spec.enhancedPrompt, err = buildEnhancedPrompt(ctx, spec.prompt, spec.context, spec.files, a)
	if err != nil {
		return nil, err
	}

	// --- Resolve provider/model ---
	spec.provider, spec.model, spec.systemPromptText, err = resolveSubagentProviderModel(a, spec.persona, spec.personaExplicitlyProvided, spec.subagentWorkspaceRoot)
	if err != nil {
		return nil, err
	}

	return spec, nil
}

// validateWorkingDir expands ~, resolves symlinks, checks that the target
// is a directory inside $HOME, and returns the resolved path. Returns the
// original (possibly empty) string unchanged when no working_dir was given.
func validateWorkingDir(workingDir string, a *Agent) (string, error) {
	if workingDir == "" {
		return "", nil
	}

	// Expand ~ to $HOME
	if strings.HasPrefix(workingDir, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", agenterrors.NewConfig("failed to resolve home directory", err)
		}
		workingDir = filepath.Join(homeDir, workingDir[2:])
	}

	// Resolve to absolute path
	absWorkingDir, err := filepath.Abs(workingDir)
	if err != nil {
		return "", agenterrors.NewValidation(fmt.Sprintf("failed to resolve working_dir: %v", err), nil)
	}

	// Resolve symlinks to prevent symlink escape attacks
	resolvedWorkingDir, err := filepath.EvalSymlinks(absWorkingDir)
	if err != nil {
		return "", agenterrors.NewValidation(fmt.Sprintf("failed to resolve working_dir symlinks: %v", err), nil)
	}

	// Verify target exists and is a directory (use resolved path)
	info, err := os.Stat(resolvedWorkingDir)
	if err != nil {
		return "", agenterrors.NewValidation(fmt.Sprintf("working_dir does not exist: %s", resolvedWorkingDir), nil)
	}
	if !info.IsDir() {
		return "", agenterrors.NewValidation(fmt.Sprintf("working_dir is not a directory: %s", resolvedWorkingDir), nil)
	}

	// Verify resolved (symlink-target) path is within $HOME
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", agenterrors.NewConfig("failed to resolve home directory", err)
	}
	resolvedHome, err := filepath.EvalSymlinks(homeDir)
	if err != nil {
		return "", agenterrors.NewConfig("failed to resolve home directory symlinks", err)
	}
	if !isPathInWorkspace(resolvedWorkingDir, resolvedHome) {
		return "", agenterrors.NewPermission(fmt.Sprintf("working_dir resolves outside $HOME via symlink: %s -> %s", absWorkingDir, resolvedWorkingDir), nil)
	}

	return resolvedWorkingDir, nil
}

// parseSubagentPersona extracts the persona from args, applies defaults
// (config default → "general"), and normalizes the slug.
func parseSubagentPersona(a *Agent, args map[string]interface{}) (persona string, explicitlyProvided bool, _ error) {
	if personaVal, ok := args["persona"]; ok && personaVal != nil {
		if personaStr, ok := personaVal.(string); ok && personaStr != "" {
			persona = personaStr
			explicitlyProvided = true
			a.Logger().Debug("Subagent persona specified: %s\n", persona)
		}
	}

	// Default to the configured default persona if not specified, falling back
	// to "general" if no default is set. This lets users redirect default
	// spawns via config without editing the catalog.
	if persona == "" {
		if cfg := a.GetConfig(); cfg != nil && strings.TrimSpace(cfg.DefaultSubagentPersona) != "" {
			persona = strings.TrimSpace(cfg.DefaultSubagentPersona)
		} else {
			persona = personas.IDGeneral
		}
		a.Logger().Debug("No persona specified, using default: %s\n", persona)
	}
	persona = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(persona)), "-", "_")

	return persona, explicitlyProvided, nil
}

// buildEnhancedPrompt reads file contents and assembles the enhanced prompt
// with Previous Work Context, Relevant Files, and Task sections. Validates
// the final size against MAX_SUBAGENT_CONTEXT_SIZE.
func buildEnhancedPrompt(ctx context.Context, prompt, context string, files []string, a *Agent) (string, error) {
	enhancedPrompt := new(strings.Builder)

	// Add previous work context section if provided
	if context != "" {
		enhancedPrompt.WriteString("# Previous Work Context\n\n")
		enhancedPrompt.WriteString(context)
		enhancedPrompt.WriteString("\n\n---\n\n")
	}

	// Add relevant files section if provided
	if len(files) > 0 {
		enhancedPrompt.WriteString("# Relevant Files\n\n")
		for _, filePath := range files {
			enhancedPrompt.WriteString(fmt.Sprintf("## File: %s\n\n", filePath))
			content, err := tools.ReadFile(ctx, filePath)
			if err != nil {
				enhancedPrompt.WriteString(fmt.Sprintf("[Error reading file: %v]\n\n", err))
				a.Logger().Debug("Failed to read file %s for subagent context: %v\n", filePath, err)
			} else {
				enhancedPrompt.WriteString(content)
				enhancedPrompt.WriteString("\n\n")
			}
		}
		enhancedPrompt.WriteString("---\n\n")
	}

	// Add task section
	enhancedPrompt.WriteString("# Your Task\n\n")
	enhancedPrompt.WriteString(prompt)

	a.Logger().Debug("Spawning subagent with enhanced prompt (length: %d)\n", enhancedPrompt.Len())

	// Validate enhanced prompt size
	if enhancedPrompt.Len() > MAX_SUBAGENT_CONTEXT_SIZE {
		return "", agenterrors.NewValidation(fmt.Sprintf("enhanced prompt exceeds maximum size of %d bytes", MAX_SUBAGENT_CONTEXT_SIZE), nil)
	}

	return enhancedPrompt.String(), nil
}
