package git

import (
	"regexp"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/buildinfo"
)

// AssistedByKey is the git trailer key that records AI assistance on a
// commit. Sprout never writes Co-Authored-By for a tool: GitHub renders
// those as contributors, while a plain trailer states the fact without
// advertising it.
const AssistedByKey = "Assisted-by"

// SproutToolName is the tool name sprout writes in its own Assisted-by
// trailers.
const SproutToolName = "sprout"

// Assistant identifies one tool+model combination that helped produce a
// commit. It renders as
//
//	Assisted-by: <tool>[/<version>] (<provider>/<model>)
type Assistant struct {
	Tool     string
	Version  string
	Provider string
	Model    string
}

// SproutAssistant returns the Assistant for this sprout build running
// provider/model. Release builds report the semantic version without the
// leading "v"; source builds report "dev".
func SproutAssistant(provider, model string) Assistant {
	return Assistant{
		Tool:     SproutToolName,
		Version:  SproutVersion(),
		Provider: provider,
		Model:    model,
	}
}

// SproutVersion returns the build version used in Assisted-by trailers.
func SproutVersion() string {
	v := strings.TrimSpace(buildinfo.Version)
	if v == "" {
		return "dev"
	}
	return strings.TrimPrefix(v, "v")
}

// unsafeTrailerChars are characters that would break the trailer format
// (whitespace splits tokens; parentheses delimit the provider/model).
var unsafeTrailerChars = regexp.MustCompile(`[\s()]+`)

func sanitizeTrailerPart(s string) string {
	return unsafeTrailerChars.ReplaceAllString(strings.TrimSpace(s), "-")
}

// valid reports whether the assistant carries enough to render an honest
// trailer: a tool, a provider and a model. An unknown model is left out
// rather than guessed.
func (a Assistant) valid() bool {
	p, m := strings.TrimSpace(a.Provider), strings.TrimSpace(a.Model)
	return strings.TrimSpace(a.Tool) != "" &&
		p != "" && m != "" &&
		!strings.EqualFold(p, "unknown") && !strings.EqualFold(m, "unknown")
}

// Value renders the trailer value, e.g.
// "sprout/0.30.0 (deepinfra/deepseek-ai/DeepSeek-V4.1-Flash)".
func (a Assistant) Value() string {
	tool := strings.Join(strings.Fields(a.Tool), " ")
	if v := sanitizeTrailerPart(a.Version); v != "" {
		tool += "/" + v
	}
	return tool + " (" + sanitizeTrailerPart(a.Provider) + "/" + sanitizeTrailerPart(a.Model) + ")"
}

// Trailer renders the full trailer line.
func (a Assistant) Trailer() string {
	return AssistedByKey + ": " + a.Value()
}

// trailerLineRe matches a git trailer line ("Token: value"). Tokens are
// alphanumerics and hyphens, as git interpret-trailers recognises them.
var trailerLineRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*:\s`)

// isTrailerBlock reports whether every line of a paragraph is a trailer or
// a continuation line (leading whitespace) of the previous trailer.
func isTrailerBlock(lines []string) bool {
	if len(lines) == 0 || !trailerLineRe.MatchString(lines[0]) {
		return false
	}
	for _, l := range lines[1:] {
		if trailerLineRe.MatchString(l) {
			continue
		}
		if l != "" && (l[0] == ' ' || l[0] == '\t') {
			continue
		}
		return false
	}
	return true
}

// AppendAssistedBy appends one Assisted-by trailer per distinct assistant to
// message, git-trailer compatible: when the message already ends in a
// trailer block (a final paragraph, after the subject, made only of
// "Token: value" lines) the new trailers join it; otherwise they start a new
// block after a blank line. Trailers already present (an amend or a re-run)
// are not duplicated, and assistants without a known provider/model are
// skipped. A message with no valid assistants is returned unchanged.
func AppendAssistedBy(message string, assistants ...Assistant) string {
	body := strings.TrimRight(message, " \t\r\n")
	if body == "" {
		return message
	}
	lines := strings.Split(body, "\n")

	// Locate the last paragraph; a single-paragraph message is the subject
	// and is never a trailer block.
	start := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			start = i + 1
			break
		}
	}
	hasBlock := start > 0 && isTrailerBlock(lines[start:])

	existing := map[string]bool{}
	if hasBlock {
		for _, l := range lines[start:] {
			existing[normalizeTrailer(l)] = true
		}
	}

	var add []string
	for _, a := range assistants {
		if !a.valid() {
			continue
		}
		t := a.Trailer()
		key := normalizeTrailer(t)
		if existing[key] {
			continue
		}
		existing[key] = true
		add = append(add, t)
	}
	if len(add) == 0 {
		return message
	}
	sep := "\n\n"
	if hasBlock {
		sep = "\n"
	}
	return body + sep + strings.Join(add, "\n") + "\n"
}

// normalizeTrailer compares trailers the way git does: the key is
// case-insensitive, the value is compared after trimming.
func normalizeTrailer(line string) string {
	k, v, ok := strings.Cut(line, ":")
	if !ok {
		return strings.TrimSpace(line)
	}
	return strings.ToLower(strings.TrimSpace(k)) + ":" + strings.TrimSpace(v)
}

// toolAttributionLineRe matches lines sprout must never write into a
// commit message: Co-Authored-By trailers (an LLM cannot know a real
// co-author, so any it emits names a tool) and "Generated with/by <tool>"
// footers.
var toolAttributionLineRe = regexp.MustCompile(`(?i)^\s*(co-authored-by:|\W*generated (with|by) .*(claude|anthropic|openai|chatgpt|gpt-?[0-9]|copilot|gemini|cursor|codex|aider|devin|windsurf|sprout|deepseek|qwen|llama))`)

// StripToolAttribution removes Co-Authored-By trailers and "Generated
// with …" footers from model-generated commit text.
func StripToolAttribution(message string) string {
	lines := strings.Split(message, "\n")
	out := lines[:0]
	for _, l := range lines {
		if toolAttributionLineRe.MatchString(l) {
			continue
		}
		out = append(out, l)
	}
	return strings.TrimRight(strings.Join(out, "\n"), " \t\r\n")
}
