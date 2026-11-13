package mockup

// Reading a product's look from its code: the stylesheets that define the
// most of its design (custom properties, a :root), its theme and token files,
// its UI libraries and its components' names go to the model, which answers in
// the kit's terms. The host supplies the files; what is read, asked and
// accepted is the same everywhere.

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/mockup/kit"
)

// Files is a project's files, as its look is read from them.
type Files interface {
	// Paths are its files, relative to its root.
	Paths() []string
	// Read is a file's text; false when it is not a readable text file.
	Read(path string) (string, bool)
}

// lookEvidenceMax bounds what the model reads of a project.
const lookEvidenceMax = 10 << 10

var (
	lookScriptRe    = regexp.MustCompile(`(?i)(^|/)(tailwind\.config\.(js|cjs|mjs|ts)|theme\.(ts|js)|tokens?\.(json|ts|js))$`)
	lookCSSLineRe   = regexp.MustCompile(`(?i)(--[a-z0-9-]+\s*:|font-family\s*:|border-radius\s*:|prefers-color-scheme|data-theme|\.dark\b)`)
	lookVarDefRe    = regexp.MustCompile(`(?m)^\s*--[a-zA-Z0-9-]+\s*:`)
	lookThemeDirRe  = regexp.MustCompile(`(?i)(^|/)(styles?|theme|themes|tokens|design-system|design)(/|$)|design-system`)
	lookComponentRe = regexp.MustCompile(`(?i)(^|/)components?/(.+/)?([A-Z][A-Za-z0-9]+)\.(tsx|jsx|vue|svelte)$`)
	lookSkipDirRe   = regexp.MustCompile(`(?i)(^|/)(node_modules|vendor|dist|build|\.next|\.git|coverage|\.sprout)(/|$)`)
	lookLibraries   = []string{"tailwindcss", "@mui/", "@chakra-ui/", "bootstrap", "antd", "@radix-ui/", "@mantine/", "daisyui", "bulma", "@headlessui/", "styled-components", "@emotion/"}
)

// LookEvidence is what a project's code says of its look, bounded; "" when it
// has no styles to read (a project with nothing to match).
func LookEvidence(f Files) string {
	type scored struct {
		path  string
		score int
		lines []string
	}
	var picked []scored
	var components []string
	for _, p := range f.Paths() {
		if lookSkipDirRe.MatchString(p) {
			continue
		}
		if m := lookComponentRe.FindStringSubmatch(p); m != nil && len(components) < 60 {
			components = append(components, m[3])
		}
		ext := strings.ToLower(path.Ext(p))
		inThemeDir := lookThemeDirRe.MatchString(p)
		switch {
		case ext == ".css" || ext == ".scss" || ext == ".less":
			text, ok := f.Read(p)
			if !ok {
				continue
			}
			score := len(lookVarDefRe.FindAllStringIndex(text, -1))
			if strings.Contains(text, ":root") {
				score += 5
			}
			if inThemeDir {
				score += 3
			}
			var lines []string
			for _, line := range strings.Split(text, "\n") {
				if lookCSSLineRe.MatchString(line) {
					lines = append(lines, strings.TrimSpace(line))
				}
			}
			if score > 0 && len(lines) > 0 {
				picked = append(picked, scored{p, score, lines})
			}
		case lookScriptRe.MatchString(p) || (inThemeDir && (ext == ".ts" || ext == ".js" || ext == ".json")):
			text, ok := f.Read(p)
			if !ok || strings.TrimSpace(text) == "" {
				continue
			}
			picked = append(picked, scored{p, 20, strings.Split(truncate(text, 3000), "\n")})
		}
	}
	if len(picked) == 0 {
		return ""
	}
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].score > picked[j].score })
	var b strings.Builder
	writeLibraries(&b, f)
	for i, s := range picked {
		if i >= 6 || b.Len() >= lookEvidenceMax {
			break
		}
		fmt.Fprintf(&b, "%s:\n%s\n\n", s.path, truncate(strings.Join(s.lines, "\n"), 2500))
	}
	if len(components) > 0 {
		fmt.Fprintf(&b, "Components: %s\n", strings.Join(components, ", "))
	}
	return truncate(b.String(), lookEvidenceMax)
}

// writeLibraries names the UI libraries the project's package.json (at its
// root, or one level down) depends on.
func writeLibraries(b *strings.Builder, f Files) {
	pkg, ok := f.Read("package.json")
	if !ok {
		for _, p := range f.Paths() {
			if strings.HasSuffix(p, "/package.json") && strings.Count(p, "/") == 1 {
				if pkg, ok = f.Read(p); ok {
					break
				}
			}
		}
	}
	if !ok {
		return
	}
	var libs []string
	for _, lib := range lookLibraries {
		if strings.Contains(pkg, `"`+lib) {
			libs = append(libs, strings.TrimSuffix(lib, "/"))
		}
	}
	if len(libs) > 0 {
		fmt.Fprintf(b, "UI libraries: %s\n\n", strings.Join(libs, ", "))
	}
}

// LookPrompt asks a model for the look the evidence shows, in the kit's
// terms, with notes on how the product is laid out.
func LookPrompt(evidence string) string {
	return `You read a product's front-end code to describe its look, so a mockup of a change to it can be drawn in that look.
Answer in JSON only:
{"colors":{"bg":"","surface":"","soft":"","border":"","text":"","muted":"","primary":"","on_primary":"","accent":"","on_accent":""},"font_body":"","font_head":"","radius":"","dark":false,"notes":""}

- colors: hex colours. bg is the page, surface a card or panel on it, soft a tinted area, border the lines, text and muted the words, primary the main action (on_primary the words on it), accent a second highlight. Use the product's own values; where its code does not say, choose what fits the rest. If it has a light and a dark theme, describe the one it opens in.
- font_body, font_head: font stacks as the code names them ("Inter, system-ui, sans-serif"); empty when it does not say.
- radius: the corner radius of its cards, like "8px".
- dark: true for a dark interface.
- notes: one or two sentences on how its interface is laid out and styled — its navigation, density, shapes and what it is built from — from what the code shows.

The code:
` + evidence
}

// ParseLook reads a model's answer to LookPrompt: the look, with every value
// that is not plainly a colour, a font stack or a length left out, and the
// layout notes. The look is nil when nothing usable came back.
func ParseLook(answer string) (*kit.Look, string) {
	answer = strings.TrimSpace(answer)
	if i, j := strings.Index(answer, "{"), strings.LastIndex(answer, "}"); i >= 0 && j > i {
		answer = answer[i : j+1]
	}
	var out struct {
		kit.Look
		Notes string `json:"notes"`
	}
	if err := json.Unmarshal([]byte(answer), &out); err != nil {
		return nil, ""
	}
	look, ok := out.Look.Clean()
	if !ok {
		return nil, ""
	}
	return &look, truncate(strings.TrimSpace(out.Notes), 500)
}

// ReadLook reads a project's look with one model call (ask answers a prompt
// with the model's text). A project with no styles reads as no look, with no
// call made.
func ReadLook(ctx context.Context, f Files, ask func(ctx context.Context, prompt string) (string, error)) (*kit.Look, string, error) {
	evidence := LookEvidence(f)
	if evidence == "" {
		return nil, "", nil
	}
	answer, err := ask(ctx, LookPrompt(evidence))
	if err != nil {
		return nil, "", err
	}
	look, notes := ParseLook(answer)
	return look, notes, nil
}

// truncate bounds s to n bytes on a character boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}
