//go:build !js

package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/skills"
)

const skillUsage = `Manage installed skills.

Usage: /skill <action> [args] [flags]

Actions:
  install <source> [--force] [--ref <ref>]   Install from path/git-url/registry-id
  update [skill-id]      Update an installed skill from its recorded origin
  remove <skill-id>      Uninstall a skill by ID
  list                   List installed skills

<source> for install can be:
  - a local path (/abs/path or relative)
  - a git URL (https://... or git@...)
  - a registry ID (one of the 5 starter skills)

Flags:
  --force        Overwrite an existing install
  --ref <ref>    Git ref (branch/tag) when installing from a git URL

Examples:
  /skill install security-review
  /skill install https://github.com/me/my-skills.git --ref main
  /skill install ./local-skill --force
  /skill update security-review
  /skill remove security-review
  /skill list
`

// SkillCommand exposes the /skill slash command.
type SkillCommand struct{}

func (c *SkillCommand) Name() string { return "skill" }

func (c *SkillCommand) Description() string {
	return "Install, update, remove, or list skills"
}

func (c *SkillCommand) Usage() string { return skillUsage }

func (c *SkillCommand) Execute(args []string, chatAgent *agent.Agent) error {
	return executeSkillCommand(args, os.Stdout, os.Stderr)
}

func (c *SkillCommand) ExecuteWithJSONOutput(args []string, chatAgent *agent.Agent, ctx *CommandContext) error {
	return executeSkillCommandJSON(args, ctx)
}

// executeSkillCommand is the testable dispatcher.
func executeSkillCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "Missing action. Use one of: install, update, remove, list")
		return errors.New("missing action")
	}
	action := args[0]
	rest := args[1:]

	switch action {
	case "list":
		return listSkills(stdout, stderr)
	case "install":
		return runInstall(rest, stdout, stderr)
	case "update":
		return runUpdate(rest, stdout, stderr)
	case "remove":
		return runRemove(rest, stdout, stderr)
	case "help", "--help", "-h":
		fmt.Fprint(stdout, skillUsage)
		return nil
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
}

func listSkills(stdout, stderr io.Writer) error {
	dir, err := skills.DefaultSkillsDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(stdout, "No skills installed.")
			return nil
		}
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	if len(names) == 0 {
		fmt.Fprintln(stdout, "No skills installed.")
		return nil
	}

	fmt.Fprintf(stdout, "%d installed skill(s):\n", len(names))
	for _, n := range names {
		skillDir := filepath.Join(dir, n)
		origin, _ := skills.LoadOrigin(skillDir)
		originStr := "(no origin metadata)"
		if origin.Type != "" {
			originStr = fmt.Sprintf("origin=%s", origin.Type)
			if origin.URL != "" {
				originStr += " url=" + origin.URL
			}
			if origin.RegistryID != "" {
				originStr += " registry_id=" + origin.RegistryID
			}
			if origin.Ref != "" {
				originStr += " ref=" + origin.Ref
			}
		}
		fmt.Fprintf(stdout, "  \u2022 %s  [%s]\n", n, originStr)
	}
	return nil
}

func runInstall(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("install requires a source: path, git URL, or registry ID")
	}
	source := args[0]
	rest := args[1:]

	var opts skills.InstallOptions
	var ref string
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--force":
			opts.Force = true
		case "--ref":
			if i+1 >= len(rest) {
				return fmt.Errorf("--ref requires a value")
			}
			i++
			ref = rest[i]
		default:
			return fmt.Errorf("unknown flag: %s", rest[i])
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var results []skills.InstallResult
	var err error

	isGit := strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") ||
		strings.HasPrefix(source, "git@") || strings.HasSuffix(source, ".git")

	if ref != "" && !isGit {
		fmt.Fprintf(stderr, "warning: --ref is only valid for git URLs, ignoring\n")
	}

	switch {
	case isGit:
		results, err = skills.InstallFromGit(ctx, source, ref, opts)
	case strings.Contains(source, "/") || strings.HasPrefix(source, ".") || filepath.IsAbs(source):
		results, err = skills.InstallFromPath(source, opts)
	default:
		results, err = skills.InstallFromRegistry(ctx, source, opts)
	}
	if err != nil {
		return err
	}
	for _, r := range results {
		fmt.Fprintf(stdout, "\u2713 installed %s \u2192 %s\n", r.SkillID, r.InstallDir)
	}
	return nil
}

func runUpdate(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("update requires a skill ID (or 'all' to update all installed skills)")
	}
	target := args[0]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if target == "all" {
		dir, err := skills.DefaultSkillsDir()
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintln(stdout, "No skills installed.")
				return nil
			}
			return err
		}
		var failed int
		var names []string
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			names = append(names, e.Name())
		}
		sort.Strings(names)
		for _, name := range names {
			results, uerr := skills.Update(ctx, name, skills.InstallOptions{Force: true})
			if uerr != nil {
				fmt.Fprintf(stderr, "\u2717 %s: %v\n", name, uerr)
				failed++
				continue
			}
			for _, r := range results {
				fmt.Fprintf(stdout, "\u2713 updated %s\n", r.SkillID)
			}
		}
		if failed > 0 {
			return fmt.Errorf("%d skill(s) failed to update", failed)
		}
		return nil
	}

	results, err := skills.Update(ctx, target, skills.InstallOptions{Force: true})
	if err != nil {
		return err
	}
	for _, r := range results {
		fmt.Fprintf(stdout, "\u2713 updated %s\n", r.SkillID)
	}
	return nil
}

func runRemove(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("remove requires a skill ID")
	}
	skillID := args[0]
	if err := skills.Uninstall(skillID); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "\u2713 removed %s\n", skillID)
	return nil
}

func executeSkillCommandJSON(args []string, ctx *CommandContext) error {
	if len(args) >= 1 && args[0] == "list" {
		type skillEntry struct {
			ID         string `json:"id"`
			OriginType string `json:"origin_type,omitempty"`
			OriginURL  string `json:"origin_url,omitempty"`
		}
		dir, err := skills.DefaultSkillsDir()
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return WriteJSONToOutput([]skillEntry{})
			}
			return err
		}
		list := make([]skillEntry, 0)
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			origin, _ := skills.LoadOrigin(filepath.Join(dir, e.Name()))
			list = append(list, skillEntry{ID: e.Name(), OriginType: origin.Type, OriginURL: origin.URL})
		}
		sort.Slice(list, func(i, j int) bool {
			return list[i].ID < list[j].ID
		})
		return WriteJSONToOutput(list)
	}
	var buf bytes.Buffer
	if err := executeSkillCommand(args, &buf, &buf); err != nil {
		return err
	}
	return WriteJSONToOutput(map[string]string{"output": buf.String()})
}
