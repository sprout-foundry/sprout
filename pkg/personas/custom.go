package personas

// custom.go — custom personas (SP-175).
//
// The embedded catalog stays the immutable core. Everything here reads
// persona definitions a creator owns: the person's files under
// `<config dir>/personas/` and a repository's files under
// `<workspace>/.sprout/personas/`. The host's own definitions are not files
// — they arrive through the headless agent API — so they are loaded by the
// caller and only validated here.
//
// Two rules shape the loader:
//
//   - A custom persona never shadows a core one. Reusing a core id or alias
//     is an error for that persona (named, with its file), never an override.
//   - One bad file never stops the rest. A malformed or colliding persona is
//     reported and skipped; the other personas still load.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Source names where a persona definition came from. Core is the embedded
// catalog; the rest are custom scopes, ordered by precedence.
type Source string

const (
	SourceCore    Source = "core"
	SourceHost    Source = "host"
	SourceUser    Source = "user"
	SourceProject Source = "project"
)

// IsCustom reports whether the source is one a creator owns.
func (s Source) IsCustom() bool { return s != SourceCore && s != "" }

// CustomPersona is a definition from a non-core source, with its provenance.
// The embedded Definition is the persona itself; the fields beside it say
// where it came from, which file, and which revision of that file.
type CustomPersona struct {
	Definition
	Source Source
	// File is the absolute path the definition was read from. Empty for a
	// host-scope definition, which lives only in memory.
	File string
	// Hash identifies the file's content (hex sha256). Project-scope
	// acceptance is recorded against it, so editing the file withdraws the
	// acceptance until the person accepts the new content.
	Hash string
}

// ProblemKind classifies a Problem.
type ProblemKind string

const (
	// ProblemError means the persona was skipped.
	ProblemError ProblemKind = "error"
	// ProblemWarning means the persona loaded but something is off.
	ProblemWarning ProblemKind = "warning"
)

// Problem is one finding about one persona file. It always names the file
// and, when it is known, the persona, so a user can act on it.
type Problem struct {
	Kind    ProblemKind
	Source  Source
	File    string
	ID      string
	Message string
}

func (p Problem) String() string {
	where := p.File
	if where == "" {
		where = string(p.Source) + " scope"
	}
	if p.ID != "" {
		return fmt.Sprintf("%s: persona %q: %s", where, p.ID, p.Message)
	}
	return fmt.Sprintf("%s: %s", where, p.Message)
}

// LoadOptions names the directories to read. An empty directory is skipped.
type LoadOptions struct {
	// UserDir is `<config dir>/personas`.
	UserDir string
	// ProjectDir is `<workspace>/.sprout/personas`.
	ProjectDir string
	// AcceptedProjectHashes maps a project persona id to the content hash
	// the person accepted. A project persona whose file hash is not the
	// accepted one is reported as pending and is not loaded. Nil means
	// nothing project-scoped is accepted.
	AcceptedProjectHashes map[string]string
}

// LoadResult is what the loader found.
type LoadResult struct {
	// Personas are the usable custom personas, host precedence aside: user
	// before project, and a project persona shadowed by a user one is left
	// out of this list and reported as a Problem with kind warning.
	Personas []CustomPersona
	// Problems are per-persona errors and warnings.
	Problems []Problem
	// PendingProject lists project personas that are valid but not accepted
	// (or whose file changed since they were accepted). They are usable only
	// after the person accepts them.
	PendingProject []CustomPersona
}

// LoadCustomPersonas reads user- and project-scope persona files and returns
// the definitions that may be used, plus every problem found. `core` is the
// embedded catalog, used only for the collision check — core definitions are
// never replaced.
func LoadCustomPersonas(core map[string]Definition, opts LoadOptions) LoadResult {
	var result LoadResult

	coreIDs, coreAliases := coreNames(core)

	seen := make(map[string]Source) // normalized id/alias → the scope that claimed it

	for _, scope := range []struct {
		source Source
		dir    string
	}{
		{SourceUser, opts.UserDir},
		{SourceProject, opts.ProjectDir},
	} {
		if strings.TrimSpace(scope.dir) == "" {
			continue
		}
		files, err := personaFiles(scope.dir)
		if err != nil {
			result.Problems = append(result.Problems, Problem{
				Kind:    ProblemWarning,
				Source:  scope.source,
				File:    scope.dir,
				Message: fmt.Sprintf("cannot read persona directory: %v", err),
			})
			continue
		}
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				result.Problems = append(result.Problems, Problem{
					Kind: ProblemError, Source: scope.source, File: file,
					Message: fmt.Sprintf("cannot read file: %v", err),
				})
				continue
			}
			hash := ContentHash(data)
			defs, problems := parsePersonaFile(data, file, scope.source)
			result.Problems = append(result.Problems, problems...)
			for _, def := range defs {
				persona := CustomPersona{
					Definition: def,
					Source:     scope.source,
					File:       file,
					Hash:       hash,
				}
				if msg := collidesWithCore(def, coreIDs, coreAliases); msg != "" {
					result.Problems = append(result.Problems, Problem{
						Kind: ProblemError, Source: scope.source, File: file, ID: def.ID, Message: msg,
					})
					continue
				}
				if prior, claimed := seen[def.ID]; claimed {
					if scope.source == SourceProject && prior == SourceUser {
						// A user persona wins over a project one with the same
						// id. Reported, never silent.
						result.Problems = append(result.Problems, Problem{
							Kind: ProblemWarning, Source: scope.source, File: file, ID: def.ID,
							Message: fmt.Sprintf("shadowed by the %s-scope persona with the same id; this file is not used", prior),
						})
						continue
					}
					result.Problems = append(result.Problems, Problem{
						Kind: ProblemError, Source: scope.source, File: file, ID: def.ID,
						Message: fmt.Sprintf("duplicate persona id, already declared in %s scope", prior),
					})
					continue
				}
				if msg := collidesWithCustomAlias(def, seen); msg != "" {
					result.Problems = append(result.Problems, Problem{
						Kind: ProblemError, Source: scope.source, File: file, ID: def.ID, Message: msg,
					})
					continue
				}

				// Claim the id and its aliases before the acceptance check, so
				// a second file declaring the same id is a duplicate even when
				// both are still pending acceptance.
				seen[def.ID] = scope.source
				for _, alias := range def.Aliases {
					if normalized := normalizeID(alias); normalized != "" && normalized != def.ID {
						seen[normalized] = scope.source
					}
				}

				if scope.source == SourceProject && !acceptedHashMatches(opts.AcceptedProjectHashes, def.ID, hash) {
					result.PendingProject = append(result.PendingProject, persona)
					continue
				}
				result.Personas = append(result.Personas, persona)
			}
		}
	}

	return result
}

// ContentHash returns the hex sha256 of a persona file's bytes. Acceptance of
// a project persona is recorded against it.
func ContentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// personaFiles lists the .json files in dir, sorted so a load is
// deterministic.
func personaFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}

// personaFileJSON is a Definition plus a tri-state `enabled`, so a file that
// omits the key gets the custom-persona default (true) rather than the zero
// value. The outer Enabled shadows the embedded one at unmarshal time.
type personaFileJSON struct {
	Definition
	Enabled *bool `json:"enabled"`
}

type personaCatalogJSON struct {
	Personas []personaFileJSON `json:"personas"`
}

// parsePersonaFile decodes one file: either a `{"personas": [...]}` catalog
// or a bare definition object. Problems are per-file and per-persona; a
// malformed file yields no definitions.
func parsePersonaFile(data []byte, file string, source Source) ([]Definition, []Problem) {
	var catalog personaCatalogJSON
	if err := json.Unmarshal(data, &catalog); err == nil && len(catalog.Personas) > 0 {
		return validateAll(catalog.Personas, file, source)
	}

	// A bare definition object. Decode it the same way so `enabled` behaves
	// identically; an object with no `personas` key fails the catalog decode
	// above only when `personas` is absent, so this is the single-persona
	// form.
	var single personaFileJSON
	if err := json.Unmarshal(data, &single); err != nil {
		return nil, []Problem{{
			Kind: ProblemError, Source: source, File: file,
			Message: fmt.Sprintf("malformed JSON: %v", err),
		}}
	}
	if strings.TrimSpace(single.ID) == "" {
		return nil, []Problem{{
			Kind: ProblemError, Source: source, File: file,
			Message: "no persona found: expected a definition with an \"id\" or a {\"personas\": [...]} catalog",
		}}
	}
	return validateAll([]personaFileJSON{single}, file, source)
}

// validateAll normalizes and checks each definition, keeping the ones that
// pass. A definition missing a required field is reported and skipped.
func validateAll(raw []personaFileJSON, file string, source Source) ([]Definition, []Problem) {
	var (
		defs     []Definition
		problems []Problem
	)
	for _, entry := range raw {
		def := entry.Definition
		def.ID = normalizeID(def.ID)
		def.Name = strings.TrimSpace(def.Name)
		def.Description = strings.TrimSpace(def.Description)
		if entry.Enabled != nil {
			def.Enabled = *entry.Enabled
		} else {
			// Custom personas are enabled unless their creator says otherwise.
			def.Enabled = true
		}

		var missing []string
		if def.ID == "" {
			missing = append(missing, "id")
		}
		if def.Name == "" {
			missing = append(missing, "name")
		}
		if def.Description == "" {
			missing = append(missing, "description")
		}
		if len(missing) > 0 {
			problems = append(problems, Problem{
				Kind: ProblemError, Source: source, File: file, ID: def.ID,
				Message: "missing required field(s): " + strings.Join(missing, ", "),
			})
			continue
		}
		if !SafePersonaID(def.ID) {
			problems = append(problems, Problem{
				Kind: ProblemError, Source: source, File: file, ID: def.ID,
				Message: fmt.Sprintf("id %q is not safe to use as a filename: use letters, digits, and underscores", def.ID),
			})
			continue
		}

		if warning := selectorWithoutToolSet(def); warning != "" {
			problems = append(problems, Problem{
				Kind: ProblemWarning, Source: source, File: file, ID: def.ID, Message: warning,
			})
		}

		defs = append(defs, def)
	}
	return defs, problems
}

// selectorWithoutToolSet warns when a custom persona names host tools, skills
// or MCP servers but leaves `allowed_tools` empty. Those selectors are
// additive: an empty allowed_tools means "the default agent's tools", which
// already includes them, so the declaration reads as a restriction and is
// not one.
func selectorWithoutToolSet(def Definition) string {
	if len(Named(def.AllowedTools)) > 0 {
		return ""
	}
	var declared []string
	if len(def.HostTools) > 0 {
		declared = append(declared, "host_tools")
	}
	if len(def.Skills) > 0 {
		declared = append(declared, "skills")
	}
	if len(def.MCP) > 0 {
		declared = append(declared, "mcp")
	}
	if len(declared) == 0 {
		return ""
	}
	return fmt.Sprintf("%s is additive and adds nothing while allowed_tools is empty "+
		"(an empty allowed_tools means the default agent's tools); list the built-in tools to restrict the set",
		strings.Join(declared, ", "))
}

// coreNames returns the normalized core ids and the set of normalized
// aliases, for the collision check.
func coreNames(core map[string]Definition) (map[string]struct{}, map[string]struct{}) {
	ids := make(map[string]struct{}, len(core))
	aliases := make(map[string]struct{})
	for id, def := range core {
		ids[normalizeID(id)] = struct{}{}
		if normalized := normalizeID(def.ID); normalized != "" {
			ids[normalized] = struct{}{}
		}
		for _, alias := range def.Aliases {
			if normalized := normalizeID(alias); normalized != "" {
				aliases[normalized] = struct{}{}
			}
		}
	}
	return ids, aliases
}

// collidesWithCore reports why a custom definition may not use its id, or ""
// when it is free. Core personas are immutable, so a collision is an error
// for the custom persona rather than an override.
func collidesWithCore(def Definition, coreIDs, coreAliases map[string]struct{}) string {
	if _, ok := coreIDs[def.ID]; ok {
		return fmt.Sprintf("id %q is a core persona id; core personas are immutable and cannot be redefined", def.ID)
	}
	if _, ok := coreAliases[def.ID]; ok {
		return fmt.Sprintf("id %q is a core persona alias; core personas are immutable and cannot be redefined", def.ID)
	}
	for _, alias := range def.Aliases {
		normalized := normalizeID(alias)
		if normalized == "" {
			continue
		}
		if _, ok := coreIDs[normalized]; ok {
			return fmt.Sprintf("alias %q is a core persona id; a custom persona cannot shadow a core persona", normalized)
		}
		if _, ok := coreAliases[normalized]; ok {
			return fmt.Sprintf("alias %q is already a core persona alias", normalized)
		}
	}
	return ""
}

// collidesWithCustomAlias reports why a definition's aliases may not be used
// because another custom persona already claims the name, or "" when they are
// free. Without this a persona could shadow another custom persona's id with
// an alias, which is the same silent override the core check prevents.
func collidesWithCustomAlias(def Definition, seen map[string]Source) string {
	for _, alias := range def.Aliases {
		normalized := normalizeID(alias)
		if normalized == "" || normalized == def.ID {
			continue
		}
		if prior, claimed := seen[normalized]; claimed {
			return fmt.Sprintf("alias %q is already declared in %s scope", normalized, prior)
		}
	}
	return ""
}

// acceptedHashMatches reports whether the accepted hash for id equals the
// file's current hash.
func acceptedHashMatches(accepted map[string]string, id, hash string) bool {
	if len(accepted) == 0 {
		return false
	}
	want, ok := accepted[id]
	return ok && want == hash
}

// ValidateDefinition checks one host-scope definition. It returns the errors
// that make the definition unusable and the warnings that do not, so a host
// gets the same answers a file does.
func ValidateDefinition(core map[string]Definition, def Definition) (errors []string, warnings []string) {
	normalized := def
	normalized.ID = normalizeID(def.ID)
	normalized.Name = strings.TrimSpace(def.Name)
	normalized.Description = strings.TrimSpace(def.Description)

	var missing []string
	if normalized.ID == "" {
		missing = append(missing, "id")
	}
	if normalized.Name == "" {
		missing = append(missing, "name")
	}
	if normalized.Description == "" {
		missing = append(missing, "description")
	}
	if len(missing) > 0 {
		errors = append(errors, "missing required field(s): "+strings.Join(missing, ", "))
	}
	if normalized.ID != "" && !SafePersonaID(normalized.ID) {
		errors = append(errors, fmt.Sprintf("id %q is not safe to use as a filename: use letters, digits, and underscores", normalized.ID))
	}

	coreIDs, coreAliases := coreNames(core)
	if msg := collidesWithCore(normalized, coreIDs, coreAliases); msg != "" {
		errors = append(errors, msg)
	}
	if msg := selectorWithoutToolSet(normalized); msg != "" {
		warnings = append(warnings, msg)
	}
	return errors, warnings
}

// HasWildcard reports whether a selector list contains "*".
func HasWildcard(list []string) bool {
	for _, entry := range list {
		if strings.TrimSpace(entry) == "*" {
			return true
		}
	}
	return false
}

// SafePersonaID reports whether an id is safe to use as a filename segment:
// it must not contain path separators or dot segments, so a crafted id can
// never escape the personas directory.
func SafePersonaID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	return !strings.ContainsAny(id, `/\`) && !strings.Contains(id, "..")
}

// Named returns the non-empty, non-wildcard entries of a selector list.
func Named(list []string) []string {
	out := make([]string, 0, len(list))
	for _, entry := range list {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" || trimmed == "*" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// LoadCustomPersonasFromFile validates one persona file without the core
// collision check being able to see other scopes. It is the `sprout persona
// validate <path>` seam.
func LoadCustomPersonasFromFile(path string) LoadResult {
	var result LoadResult
	data, err := os.ReadFile(path)
	if err != nil {
		result.Problems = append(result.Problems, Problem{
			Kind: ProblemError, Source: SourceUser, File: path,
			Message: fmt.Sprintf("cannot read file: %v", err),
		})
		return result
	}
	core, coreErr := DefaultDefinitions()
	if coreErr != nil {
		core = nil
	}
	coreIDs, coreAliases := coreNames(core)
	hash := ContentHash(data)
	defs, problems := parsePersonaFile(data, path, SourceUser)
	result.Problems = append(result.Problems, problems...)
	for _, def := range defs {
		if msg := collidesWithCore(def, coreIDs, coreAliases); msg != "" {
			result.Problems = append(result.Problems, Problem{
				Kind: ProblemError, Source: SourceUser, File: path, ID: def.ID, Message: msg,
			})
			continue
		}
		result.Personas = append(result.Personas, CustomPersona{
			Definition: def, Source: SourceUser, File: path, Hash: hash,
		})
	}
	return result
}

// LoadCustomPersonasFromDir validates every .json file in a directory.
func LoadCustomPersonasFromDir(dir string) LoadResult {
	files, err := personaFiles(dir)
	if err != nil {
		return LoadResult{Problems: []Problem{{
			Kind: ProblemError, Source: SourceUser, File: dir,
			Message: fmt.Sprintf("cannot read directory: %v", err),
		}}}
	}
	var result LoadResult
	for _, file := range files {
		one := LoadCustomPersonasFromFile(file)
		result.Personas = append(result.Personas, one.Personas...)
		result.Problems = append(result.Problems, one.Problems...)
	}
	return result
}

// NormalizeID exposes the loader's id normalization so callers resolve a
// persona the same way files are keyed.
func NormalizeID(raw string) string { return normalizeID(raw) }
