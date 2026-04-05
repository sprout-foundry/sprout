package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
)

const (
	SkillFileName = "SKILL.md"
)

type SkillInfo struct {
	ID          string
	Name        string
	Description string
	Path        string
	Content     string
}

func LoadSkill(skillID string, config *configuration.Config) (*SkillInfo, error) {
	skill := config.GetSkill(skillID)
	if skill == nil {
		return nil, fmt.Errorf("skill not found or disabled: %s", skillID)
	}

	skillPath := resolveSkillPath(skill.Path)
	skillFile := filepath.Join(skillPath, SkillFileName)

	content, err := os.ReadFile(skillFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read skill file %s: %w", skillFile, err)
	}

	return &SkillInfo{
		ID:          skill.ID,
		Name:        skill.Name,
		Description: skill.Description,
		Path:        skillPath,
		Content:     string(content),
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

func resolveSkillPath(relativePath string) string {
	if filepath.IsAbs(relativePath) {
		return relativePath
	}

	exeDir := filepath.Dir(os.Args[0])
	if exeDir == "." {
		var err error
		exeDir, err = os.Getwd()
		if err != nil {
			return relativePath
		}
	}

	return filepath.Join(exeDir, relativePath)
}

func handleListSkills(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	configManager := a.GetConfigManager()
	config := configManager.GetConfig()

	skills := ListSkills(config)

	if len(skills) == 0 {
		return "No skills available.", nil
	}

	activeSkills := make(map[string]bool)
	for _, id := range a.activeSkills {
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

func handleActivateSkill(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	skillID, err := getStringArg(args, "skill_id")
	if err != nil {
		// Try alternative parameter names
		if skillID, err = getStringArg(args, "skill"); err != nil {
			return "", fmt.Errorf("skill_id is required")
		}
	}

	configManager := a.GetConfigManager()
	config := configManager.GetConfig()

	// Check if already active
	for _, id := range a.activeSkills {
		if id == skillID {
			return fmt.Sprintf("Skill '%s' is already active.", skillID), nil
		}
	}

	// Load the skill
	skillInfo, err := LoadSkill(skillID, config)
	if err != nil {
		return "", fmt.Errorf("failed to activate skill: %w", err)
	}

	// Add to active skills
	a.activeSkills = append(a.activeSkills, skillID)

	// Add skill content to conversation context
	skillMessage := fmt.Sprintf("[Skill Activated: %s]\n\n%s", skillInfo.Name, skillInfo.Content)
	a.messages = append(a.messages, api.Message{
		Role:    "system",
		Content: skillMessage,
	})

	return fmt.Sprintf("Activated skill '%s' (%s).\n\nDescription: %s\n\nInstructions loaded into context.", skillInfo.Name, skillID, skillInfo.Description), nil
}

func getStringArg(args map[string]interface{}, name string) (string, error) {
	val, ok := args[name]
	if !ok {
		return "", fmt.Errorf("missing required argument: %s", name)
	}
	str, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("argument '%s' must be a string", name)
	}
	return str, nil
}
