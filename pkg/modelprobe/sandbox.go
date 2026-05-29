package modelprobe

import (
	"encoding/json"
	"sort"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// exec runs one complex-stage tool call against the in-memory sandbox and
// returns the tool-result content to feed back to the model. The complex stage
// exposes read_file, list_dir, and submit_todos.
func (s *sandbox) exec(tc api.ToolCall) string {
	switch tc.Function.Name {
	case "read_file":
		var a struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &a); err != nil {
			return "error: invalid arguments: " + err.Error()
		}
		if c, ok := s.files[normPath(a.Path)]; ok {
			s.reads = append(s.reads, normPath(a.Path))
			return c
		}
		return "error: file not found: " + a.Path

	case "list_dir":
		var a struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &a); err != nil {
			return "error: invalid arguments: " + err.Error()
		}
		dir := normPath(a.Path)
		children := listChildren(s.files, dir)
		if len(children) == 0 {
			return "error: no such directory: " + a.Path
		}
		s.listed = append(s.listed, dir)
		return strings.Join(children, "\n")

	case "submit_todos":
		var a struct {
			Summary string `json:"summary"`
			Todos   string `json:"todos"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &a); err != nil {
			return "error: invalid arguments: " + err.Error()
		}
		s.todos = strings.TrimSpace(a.Summary + "\n" + a.Todos)
		return "ok: todos received"

	default:
		return "error: unknown tool " + tc.Function.Name
	}
}

// normPath canonicalizes a sandbox path: trims spaces, a leading "./", and
// surrounding slashes so "/internal/", "./internal", and "internal" all match.
// A lone "." or "" normalizes to "" (the project root).
func normPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.Trim(p, "/")
	if p == "." {
		return ""
	}
	return p
}

// listChildren returns the immediate children of dir within the flat file map,
// directories suffixed with "/". An empty dir means the project root.
func listChildren(files map[string]string, dir string) []string {
	seen := map[string]bool{}
	var out []string
	for p := range files {
		rest := p
		if dir != "" {
			if !strings.HasPrefix(p, dir+"/") {
				continue
			}
			rest = strings.TrimPrefix(p, dir+"/")
		}
		if before, _, found := strings.Cut(rest, "/"); found {
			name := before + "/"
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		} else if !seen[rest] {
			seen[rest] = true
			out = append(out, rest)
		}
	}
	sort.Strings(out)
	return out
}

// strParam is a string tool parameter shorthand shared across stages.
func strParam(desc string) api.ToolParameter {
	return api.ToolParameter{Type: "string", Description: desc}
}
