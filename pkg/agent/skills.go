package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/skills"
)

// SkillFileName is the conventional name of the markdown file inside
// each skill directory. Re-exported from pkg/skills so callers in this
// package don't need to learn two import paths for the same constant.
const SkillFileName = skills.SkillFileName

type SkillInfo struct {
	ID          string
	Name        string
	Description string
	Path        string
	Content     string
	Source      string // "builtin", "user", or "project"
}

// LoadSkill resolves a skill by ID: built-ins come from the embedded
// pkg/skills library (the single source of truth that also seeds
// Config.Skills), user/project skills come from disk via skill.Path.
// The config registry is still the gate — a skill that isn't registered
// or is explicitly disabled cannot be activated, even if its content
// happens to be embedded.
func LoadSkill(skillID string, config *configuration.Config) (*SkillInfo, error) {
	return LoadSkillInWorkspace(skillID, config, "")
}

// LoadSkillInWorkspace is the workspace-aware variant of LoadSkill.
// Project-level skills (e.g., .sprout/skills/) are resolved relative to
// workspaceRoot instead of os.Getwd(). This is critical in daemon mode
// where the process CWD differs from the workspace being served.
func LoadSkillInWorkspace(skillID string, config *configuration.Config, workspaceRoot string) (*SkillInfo, error) {
	skill := config.GetSkill(skillID)
	if skill == nil {
		return nil, agenterrors.NewNotFound(fmt.Sprintf("skill %q", skillID))
	}

	if content, err := skills.ReadContent(skillID); err == nil {
		return &SkillInfo{
			ID:          skill.ID,
			Name:        skill.Name,
			Description: skill.Description,
			Path:        skill.Path,
			Content:     content,
			Source:      "builtin",
		}, nil
	}

	// Fall back to filesystem for project/user skills
	skillPath := resolveSkillPathInWorkspace(skill.Path, workspaceRoot)
	skillFile := filepath.Join(skillPath, SkillFileName)

	content, err := os.ReadFile(skillFile)
	if err != nil {
		return nil, agenterrors.Wrapf(err, "failed to read skill file %s", skillFile)
	}

	return &SkillInfo{
		ID:          skill.ID,
		Name:        skill.Name,
		Description: skill.Description,
		Path:        skillPath,
		Content:     string(content),
		Source:      skill.Metadata["source"],
	}, nil
}

func ListSkills(config *configuration.Config) []SkillInfo {
	skills := config.GetAllEnabledSkills()
	result := make([]SkillInfo, 0, len(skills))

	for id, skill := range skills {
		result = append(result, SkillInfo{
			ID:          id,
			Name:        skill.Name,
			Description: skill.Description,
			Path:        skill.Path,
			Content:     "",
		})
	}

	return result
}

func GetSkillManifest(content string) (map[string]string, string, error) {
	lines := strings.Split(content, "\n")

	var inFrontMatter bool
	var frontMatterLines []string
	var bodyLines []string
	var inBody bool

	for _, line := range lines {
		if line == "---" {
			if !inFrontMatter {
				inFrontMatter = true
				continue
			} else {
				inFrontMatter = false
				inBody = true
				continue
			}
		}

		if inFrontMatter {
			frontMatterLines = append(frontMatterLines, line)
		} else if inBody {
			bodyLines = append(bodyLines, line)
		}
	}

	manifest := make(map[string]string)
	for _, line := range frontMatterLines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			manifest[key] = value
		}
	}

	body := strings.Join(bodyLines, "\n")
	return manifest, body, nil
}

// resolveSkillPath resolves a skill path for filesystem-based skills (project/user).
// Builtin skills are served from the embedded filesystem and don't use this.
func resolveSkillPath(relativePath string) string {
	return resolveSkillPathInWorkspace(relativePath, "")
}

// resolveSkillPathInWorkspace resolves a skill path relative to the given
// workspace root. If workspaceRoot is empty, falls back to os.Getwd().
func resolveSkillPathInWorkspace(relativePath, workspaceRoot string) string {
	if filepath.IsAbs(relativePath) {
		return relativePath
	}

	// Project skills (e.g., .sprout/skills/...) are relative to the workspace root.
	base := strings.TrimSpace(workspaceRoot)
	if base == "" {
		wd, err := os.Getwd()
		if err != nil {
			return relativePath
		}
		base = wd
	}
	return filepath.Join(base, relativePath)
}

func handleListSkills(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	configManager := a.GetConfigManager()
	config := configManager.GetConfig()

	skills := ListSkills(config)

	if len(skills) == 0 {
		return "No skills available.", nil
	}

	activeSkills := make(map[string]bool)
	for _, id := range a.state.GetActiveSkills() {
		activeSkills[id] = true
	}

	var sb strings.Builder
	sb.WriteString("## Available Skills\n\n")

	for _, skill := range skills {
		status := ""
		if activeSkills[skill.ID] {
			status = " **(active)**"
		}
		sb.WriteString(fmt.Sprintf("- **%s** (%s)%s\n  - %s\n\n", skill.Name, skill.ID, status, skill.Description))
	}

	sb.WriteString("Use `activate_skill` to load a skill's instructions into context.")

	return sb.String(), nil
}

// activateSkillByID is the skill-activation core shared by the
// activate_skill tool and turn-start auto-activation: load the skill, add
// its ID to the active-skill set, and fold its instructions into the
// system prompt.
//
// Idempotence is decided by the prompt, not the active-skill set: persona
// switches, model refreshes and per-query prompt overrides rebuild the
// prompt without clearing that set, and trusting it told the agent a skill
// was "already active" while none of its instructions were in context. A
// skill whose fold is present gets a short note; one whose fold was lost is
// folded again.
//
// Errors are returned unwrapped: the tool path wraps them in a user-facing
// tool error, while best-effort callers (auto-activation) log and continue.
func (a *Agent) activateSkillByID(skillID string) (string, error) {
	if strings.TrimSpace(skillID) == "" {
		return "", errors.New("skill_id is required")
	}
	config := a.GetConfigManager().GetConfig()
	skillInfo, err := LoadSkillInWorkspace(skillID, config, a.GetWorkspaceRoot())
	if err != nil {
		return "", err
	}

	if !slices.Contains(a.state.GetActiveSkills(), skillID) {
		a.state.SetActiveSkills(append(slices.Clone(a.state.GetActiveSkills()), skillID))
	}

	header := fmt.Sprintf("[Skill Activated: %s (", skillInfo.Name)
	if strings.Contains(a.systemPrompt, header) {
		return fmt.Sprintf("Skill '%s' is already active; its instructions are in your system prompt.", skillID), nil
	}

	sourceLabel := skillInfo.Source
	if sourceLabel == "" {
		sourceLabel = "unknown"
	}
	skillMessage := fmt.Sprintf("%ssource: %s)]\n\n%s", header, sourceLabel, skillInfo.Content)
	if missing := a.unavailableSkillTools(skillInfo.Content); len(missing) > 0 {
		skillMessage += fmt.Sprintf("\n\n> **Not available in this environment:** `%s`. Skip the steps that need them and tell the user which checks were not run.",
			strings.Join(missing, "`, `"))
	}
	if strings.TrimSpace(a.systemPrompt) != "" {
		a.systemPrompt = a.systemPrompt + "\n\n---\n\n" + skillMessage
	} else {
		a.systemPrompt = skillMessage
	}
	return fmt.Sprintf("Activated skill '%s' (%s).\n\nDescription: %s\n\nInstructions loaded into context.", skillInfo.Name, skillID, skillInfo.Description), nil
}

func handleActivateSkill(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	skillID, err := getStringArg(args, "skill_id")
	if err != nil {
		// Try alternative parameter names
		if skillID, err = getStringArg(args, "skill"); err != nil {
			return "", agenterrors.NewTool("skills", "skill_id is required", err)
		}
	}

	result, err := a.activateSkillByID(skillID)
	if err != nil {
		return "", agenterrors.NewTool("skills", "failed to activate skill", err)
	}
	return result, nil
}

func getStringArg(args map[string]interface{}, name string) (string, error) {
	val, ok := args[name]
	if !ok {
		return "", agenterrors.NewInvalidInputError(fmt.Sprintf("missing required argument: %s", name), nil)
	}
	str, ok := val.(string)
	if !ok {
		return "", agenterrors.NewInvalidInputError(fmt.Sprintf("argument '%s' must be a string", name), nil)
	}
	return str, nil
}

// skillDeclaredTools reads the `tools:` line of a skill's frontmatter — the
// tools its workflow relies on — as `tools: a, b` or `tools: [a, b]`.
func skillDeclaredTools(content string) []string {
	body, ok := strings.CutPrefix(strings.TrimLeft(content, "\ufeff \t\r\n"), "---")
	if !ok {
		return nil
	}
	end := strings.Index(body, "\n---")
	if end < 0 {
		return nil
	}
	for _, line := range strings.Split(body[:end], "\n") {
		value, ok := strings.CutPrefix(strings.TrimSpace(line), "tools:")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "[]")
		var names []string
		for _, name := range strings.Split(value, ",") {
			if name = strings.Trim(strings.TrimSpace(name), `"'`); name != "" {
				names = append(names, name)
			}
		}
		return names
	}
	return nil
}

// unavailableSkillTools lists the skill's declared tools this agent cannot
// call, so a skill written for every host stays honest on a host that lacks
// some of them (a low-context profile, a persona allowlist).
func (a *Agent) unavailableSkillTools(content string) []string {
	declared := skillDeclaredTools(content)
	if len(declared) == 0 {
		return nil
	}
	visible := make(map[string]bool)
	for _, tool := range a.getOptimizedToolDefinitions(nil) {
		visible[tool.Function.Name] = true
	}
	var missing []string
	for _, name := range declared {
		if !visible[name] {
			missing = append(missing, name)
		}
	}
	return missing
}
