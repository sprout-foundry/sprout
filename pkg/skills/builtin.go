// Package skills owns the catalogue of skills shipped with sprout. The
// library/ subdirectory is embedded into the binary at compile time; the
// discovery functions in this file are the single source of truth that
// every higher-level consumer routes through:
//
//   - pkg/configuration uses Builtins() to seed Config.Skills
//   - pkg/agent uses ReadContent() to load a skill's body for the LLM
//   - cmd/skill uses Builtins() to render `sprout skill list`
//
// The previous arrangement kept the embed in pkg/agent and the registry
// in pkg/configuration with no cross-reference, so adding a skill on
// disk silently did nothing until a hand-written entry was also added
// to defaultSkills(). New skills now drop in by creating a directory
// under library/ with a valid SKILL.md frontmatter — nothing else.
package skills

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// SkillFileName is the file inside each skill directory whose YAML
// frontmatter supplies the skill's metadata. Exported so callers that
// resolve user/project skills from disk can reuse the same convention.
const SkillFileName = "SKILL.md"

// Builtin is the parsed metadata + body for a single embedded skill.
// Content is the entire SKILL.md including frontmatter; consumers that
// only want the body should strip the frontmatter themselves with a
// shared parser to avoid divergent interpretations of the format.
type Builtin struct {
	ID          string
	Name        string
	Description string
	Path        string // logical path under the repo, e.g. pkg/skills/library/<id>
	Content     string
}

//go:embed library
var libraryFS embed.FS

// libraryRoot is the in-FS prefix for embedded skills; declared once
// so the discovery and read paths can't drift.
const libraryRoot = "library"

// LogicalPath is the repo-relative path of the embedded library, used
// as the Path metadata on Builtin entries. Other layers (configuration
// prune logic, user-facing displays) check for this prefix to identify
// builtins. Exported so those callers don't hardcode a string that
// could go stale if the package moves.
const LogicalPath = "pkg/skills/library"

// LegacyLogicalPath is the pre-refactor location of embedded skills.
// Retained so the configuration prune step (which deletes config.Skills
// entries whose Path matches a builtin prefix but whose ID is no longer
// in the default set) recognises legacy paths persisted in older
// user configs and migrates them cleanly.
const LegacyLogicalPath = "pkg/agent/skills"

// Builtins walks the embedded library and returns one entry per skill
// directory whose SKILL.md frontmatter parses successfully. Directories
// without a SKILL.md, with malformed frontmatter, or with a missing
// name/description are skipped silently — the discovery test in this
// package asserts every shipped skill is valid, so silent skips in
// production runtime can only happen for skills added without going
// through the test gate.
func Builtins() map[string]Builtin {
	entries, err := fs.ReadDir(libraryFS, libraryRoot)
	if err != nil {
		return map[string]Builtin{}
	}
	out := make(map[string]Builtin, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		b, err := loadBuiltin(id)
		if err != nil {
			continue
		}
		out[id] = b
	}
	return out
}

// IDs returns the sorted list of built-in skill IDs. Convenience for
// callers that just want the names (e.g. cmd/skill's list output).
func IDs() []string {
	skills := Builtins()
	ids := make([]string, 0, len(skills))
	for id := range skills {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ReadContent returns the full SKILL.md body for a built-in skill,
// including frontmatter. Callers responsible for activation (pkg/agent)
// pass this directly into the system prompt; the frontmatter is part of
// the message the LLM sees, matching the prior pkg/agent behaviour.
func ReadContent(id string) (string, error) {
	if !validSkillID(id) {
		return "", fmt.Errorf("invalid skill id: %q", id)
	}
	data, err := fs.ReadFile(libraryFS, libraryRoot+"/"+id+"/"+SkillFileName)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// loadBuiltin reads and parses a single embedded skill. Returns an
// error on any of: missing SKILL.md, unparseable frontmatter, missing
// required name/description fields. The discovery test exercises every
// path so a malformed skill fails the build, not the runtime.
func loadBuiltin(id string) (Builtin, error) {
	content, err := ReadContent(id)
	if err != nil {
		return Builtin{}, fmt.Errorf("read %s: %w", id, err)
	}
	meta, err := parseFrontmatter(content)
	if err != nil {
		return Builtin{}, fmt.Errorf("%s: %w", id, err)
	}
	name := strings.TrimSpace(meta["name"])
	description := strings.TrimSpace(meta["description"])
	if name == "" {
		return Builtin{}, fmt.Errorf("%s: SKILL.md missing required 'name' field", id)
	}
	if description == "" {
		return Builtin{}, fmt.Errorf("%s: SKILL.md missing required 'description' field", id)
	}
	return Builtin{
		ID:          id,
		Name:        name,
		Description: description,
		Path:        LogicalPath + "/" + id,
		Content:     content,
	}, nil
}

// parseFrontmatter extracts the YAML-style frontmatter delimited by
// `---` lines at the top of a SKILL.md. Returns the key/value pairs as
// a flat map; values are trimmed but otherwise raw (no nested-YAML
// support, which the SKILL format doesn't use). Mirrors the simple
// parser previously inlined in pkg/agent/skills.go so the behaviour is
// the same — only the location changed.
func parseFrontmatter(content string) (map[string]string, error) {
	lines := strings.Split(content, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return nil, fmt.Errorf("missing frontmatter opening delimiter")
	}
	out := make(map[string]string)
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			return out, nil
		}
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			out[key] = value
		}
	}
	return nil, fmt.Errorf("missing frontmatter closing delimiter")
}

// validSkillID rejects names that contain path separators or relative
// components. The discovery walk only ever passes real directory names,
// but ReadContent is also called by activate_skill with model-supplied
// IDs — a strict check here is the boundary that keeps a malicious
// skill_id from escaping the library/ namespace.
func validSkillID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	if strings.ContainsAny(id, "/\\") {
		return false
	}
	return true
}
