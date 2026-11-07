package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// A confined persona (one declaring write_paths, e.g. the planner) is held to
// its catalog entry at execution time, not only in the tool list the model is
// offered. Three rules apply, in order:
//
//  1. A tool outside the persona's allowed_tools is refused. The offered list
//     is already filtered, but a model can still name a tool it was never
//     offered, and the low-context root path keeps its own allowlist.
//  2. A file write (write_file, edit_file, the structured writers, and a
//     browse_url screenshot_path) must land under one of write_paths,
//     resolved against the workspace root with symlinks followed.
//  3. browse_url only reads a page: no scripted steps, interact/inspect
//     actions, persistent sessions, or file:// URLs.
//
// Unconfined personas skip all of this.

// confinedPathWriteTools maps each file-writing tool to the argument naming
// the file it writes.
var confinedPathWriteTools = map[string]string{
	"write_file":            "path",
	"edit_file":             "path",
	"write_structured_file": "path",
	"patch_structured_file": "path",
}

// confinedBrowseDeniedArgs are browse_url arguments a confined persona may not
// set: each one turns a page read into an interaction, a persistent session,
// or a local-file read.
var confinedBrowseDeniedArgs = []string{"steps", "persist_session", "session_id", "close_session", "allow_file_url"}

// activeConfinedPersona returns the active persona when it is confined, or nil.
func (a *Agent) activeConfinedPersona() *configuration.SubagentType {
	if a == nil {
		return nil
	}
	persona := a.activePersonaConfig()
	if !persona.IsConfined() {
		return nil
	}
	return persona
}

// personaAllowsTool reports whether a confined persona may call the named
// tool. It is the execution-time counterpart of the roster: `allowed_tools`
// plus, for a custom persona, the host tools its `host_tools` selects, the two
// skill tools when `skills` is set, and the MCP tools its `mcp` selectors name
// (SP-175). mcpResolver splits an advertised MCP tool name into its server and
// tool; nil disables the MCP check (no server is registered).
func personaAllowsTool(persona *configuration.SubagentType, toolName string, mcpResolver func(toolName string) (server, tool string, err error)) bool {
	if persona == nil {
		return false
	}
	if containsTrimmed(persona.AllowedTools, toolName) {
		return true
	}
	if !isCustomPersonaSource(persona.Source) {
		return false
	}
	for _, name := range persona.HostTools {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if trimmed == toolName {
			return true
		}
		if trimmed == "*" && tools.IsHostProvidedTool(toolName) {
			return true
		}
	}
	if len(persona.Skills) > 0 && (toolName == "activate_skill" || toolName == "list_skills") {
		return true
	}
	if mcpResolver != nil && strings.HasPrefix(toolName, "mcp_") && len(persona.MCP) > 0 {
		server, tool, err := mcpResolver(toolName)
		if err == nil && mcpSelectorAllows(persona.MCP, server, tool) {
			return true
		}
	}
	return false
}

// checkPersonaConfinement refuses a tool call that a confined active persona
// is not allowed to make. It returns nil for unconfined personas.
func (a *Agent) checkPersonaConfinement(toolName string, args map[string]interface{}) error {
	persona := a.activeConfinedPersona()
	if persona == nil {
		return nil
	}
	return confinementViolation(persona, a.currentWorkspaceRoot(), toolName, args, a.resolveMCPToolIdentity)
}

// confinementViolation applies the three confinement rules for one call.
func confinementViolation(persona *configuration.SubagentType, workspaceRoot, toolName string, args map[string]interface{}, mcpResolver func(toolName string) (server, tool string, err error)) error {
	if !personaAllowsTool(persona, toolName, mcpResolver) {
		return agenterrors.NewPermission(
			fmt.Sprintf("tool %s is not available to the %s persona", toolName, persona.ID),
			map[string]any{"tool": toolName, "persona": persona.ID})
	}

	if argName, ok := confinedPathWriteTools[toolName]; ok {
		path, _ := args[argName].(string)
		return checkConfinedWritePath(persona, workspaceRoot, toolName, path)
	}

	if toolName == "browse_url" {
		for _, key := range confinedBrowseDeniedArgs {
			if argIsSet(args[key]) {
				return agenterrors.NewPermission(
					fmt.Sprintf("browse_url %s is not available to the %s persona; it may only read a page (text, dom, or screenshot)", key, persona.ID),
					map[string]any{"tool": toolName, "persona": persona.ID, "arg": key})
			}
		}
		if action, _ := args["action"].(string); action == "interact" || action == "inspect" {
			return agenterrors.NewPermission(
				fmt.Sprintf("browse_url action %q is not available to the %s persona; use text, dom, or screenshot", action, persona.ID),
				map[string]any{"tool": toolName, "persona": persona.ID, "arg": "action"})
		}
		if shot, _ := args["screenshot_path"].(string); strings.TrimSpace(shot) != "" {
			return checkConfinedWritePath(persona, workspaceRoot, toolName, shot)
		}
	}
	return nil
}

// checkConfinedWritePath refuses a write outside the persona's write_paths.
// An empty path is left to the tool's own validation.
func checkConfinedWritePath(persona *configuration.SubagentType, workspaceRoot, toolName, path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if confinedPathAllowed(workspaceRoot, path, persona.WritePaths) {
		return nil
	}
	return agenterrors.NewPermission(
		fmt.Sprintf("%s refused: the %s persona may only write under %s (got %s)",
			toolName, persona.ID, strings.Join(persona.WritePaths, ", "), path),
		map[string]any{"tool": toolName, "persona": persona.ID, "path": path})
}

// confinedPathAllowed reports whether path, resolved against workspaceRoot,
// lies strictly inside one of the workspace-relative write roots. Symlinks
// on the existing part of the path are followed, so a link inside an allowed
// directory cannot redirect a write elsewhere.
func confinedPathAllowed(workspaceRoot, path string, writeRoots []string) bool {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return false
		}
		root = wd
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	root = resolveExistingPrefix(root)

	target := filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target = resolveExistingPrefix(target)

	for _, wr := range writeRoots {
		wr = strings.TrimSpace(wr)
		if wr == "" || filepath.IsAbs(wr) {
			continue
		}
		allowed := resolveExistingPrefix(filepath.Join(root, filepath.Clean(wr)))
		rel, err := filepath.Rel(allowed, target)
		if err != nil {
			continue
		}
		if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return true
	}
	return false
}

// resolveExistingPrefix follows symlinks on the longest existing ancestor of
// p and re-attaches the not-yet-existing remainder. Where symlinks cannot be
// evaluated it returns the lexically cleaned path.
func resolveExistingPrefix(p string) string {
	p = filepath.Clean(p)
	rest := ""
	cur := p
	for {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			if rest == "" {
				return resolved
			}
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		if rest == "" {
			rest = filepath.Base(cur)
		} else {
			rest = filepath.Join(filepath.Base(cur), rest)
		}
		cur = parent
	}
}

func containsTrimmed(list []string, name string) bool {
	for _, item := range list {
		if strings.TrimSpace(item) == name {
			return true
		}
	}
	return false
}

// argIsSet reports whether a tool argument carries a meaningful value: a
// non-empty string, a true bool, or a non-empty array/object.
func argIsSet(v interface{}) bool {
	switch val := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(val) != ""
	case bool:
		return val
	case []interface{}:
		return len(val) > 0
	case map[string]interface{}:
		return len(val) > 0
	default:
		return true
	}
}
