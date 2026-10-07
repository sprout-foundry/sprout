package health

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// goModRequires reads the Go module requirements from root/go.mod. It parses
// the file directly (the module manifest is the source of truth) rather than
// shelling out to the Go toolchain, so a health scan stays fast and works with
// no toolchain or network present. A project without a go.mod yields an empty
// slice and no error — the dependency check simply has nothing to inspect.
func goModRequires(root string) ([]Requirement, error) {
	path := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("health: read %s: %w", path, err)
	}
	return parseGoModRequires(string(data)), nil
}

// parseGoModRequires extracts the require directives from go.mod content.
// It handles both the block form (`require ( ... )`) and the single-line form
// (`require path version`), and marks any requirement pinned by a replace
// directive on the returned Requirement. Comments are stripped; a line with an
// "// indirect" trailing comment marks the requirement indirect.
func parseGoModRequires(content string) []Requirement {
	replaced := parseGoModReplaces(content)

	var out []Requirement
	inBlock := false

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		// Strip a trailing line comment, remembering whether it marked the
		// requirement indirect.
		indirect := false
		if idx := strings.Index(line, "//"); idx >= 0 {
			comment := strings.TrimSpace(line[idx+2:])
			if comment == "indirect" {
				indirect = true
			}
			line = strings.TrimSpace(line[:idx])
			if line == "" {
				continue
			}
		}

		// A require directive opens a block on its own line: `require (`.
		if !inBlock {
			if !strings.HasPrefix(line, "require") {
				continue
			}
			rest := strings.TrimSpace(strings.TrimPrefix(line, "require"))
			if rest == "(" {
				inBlock = true
				continue
			}
			if req, ok := parseRequireLine(rest, indirect); ok {
				req.Replaced = replaced[req.Module]
				out = append(out, req)
			}
			continue
		}

		// Inside a block, a lone ")" closes it.
		if line == ")" {
			inBlock = false
			continue
		}
		if req, ok := parseRequireLine(line, indirect); ok {
			req.Replaced = replaced[req.Module]
			out = append(out, req)
		}
	}
	return out
}

// parseGoModReplaces returns the set of module paths pinned by a replace
// directive, in either the block form (`replace ( ... )`) or the single-line
// form (`replace old => new`). Only the left-hand module path matters: a
// replaced module's listed version is not the one built.
func parseGoModReplaces(content string) map[string]bool {
	out := map[string]bool{}
	inBlock := false
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}

		if !inBlock {
			if !strings.HasPrefix(line, "replace") {
				continue
			}
			rest := strings.TrimSpace(strings.TrimPrefix(line, "replace"))
			if rest == "(" {
				inBlock = true
				continue
			}
			if mod := replaceLeftModule(rest); mod != "" {
				out[mod] = true
			}
			continue
		}

		if line == ")" {
			inBlock = false
			continue
		}
		if mod := replaceLeftModule(line); mod != "" {
			out[mod] = true
		}
	}
	return out
}

// replaceLeftModule returns the module path on the left of a `=>` in a replace
// directive, or "" when the line is not a well-formed replace. The left side
// may carry a version ("path v1.2.3 => ..."); only the path is returned.
func replaceLeftModule(line string) string {
	left, _, ok := strings.Cut(line, "=>")
	if !ok {
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(left))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// parseRequireLine parses "module/path vX.Y.Z" into a Requirement. Lines with
// fewer than two fields (a malformed or non-module line) are ignored.
func parseRequireLine(line string, indirect bool) (Requirement, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return Requirement{}, false
	}
	module, version := fields[0], fields[1]
	if module == "" || !strings.HasPrefix(version, "v") {
		return Requirement{}, false
	}
	return Requirement{Module: module, Version: version, Indirect: indirect}, true
}
