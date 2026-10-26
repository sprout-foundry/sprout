// Package git: commit-message trailer helpers.
//
// A git trailer is a "Key: Value" line in the final paragraph of a commit
// message (see `git interpret-trailers`). Sprout appends trailers the same
// way git does: a new trailer block is separated from the body by exactly one
// blank line, a message that already ends in a trailer block is joined rather
// than duplicated, and an identical trailer (case-insensitive key, trimmed
// value) is never added twice.
package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// trailerKeyRe matches a git trailer key: a token of alphanumerics and
// hyphens, as git interpret-trailers recognises it.
var trailerKeyRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

// agentsMdHeadingRe matches the AGENTS.md section heading under which default
// commit trailers are declared: a "Commit Trailers" (or "Default Commit
// Trailers") heading at any markdown level.
var agentsMdHeadingRe = regexp.MustCompile(`(?i)^#{1,6}\s*(?:default\s+)?commit\s+trailers\s*:?\s*$`)

// agentsMdTrailerItemRe matches a single trailer line inside that section:
// "Key: value", optionally as a markdown list item ("- Key: value").
var agentsMdTrailerItemRe = regexp.MustCompile(`^\s*(?:[-*+]\s+)?([A-Za-z0-9][A-Za-z0-9-]*)\s*:\s*(\S.*?)\s*$`)

// FormatTrailerLine normalises a user-supplied "Key: Value" trailer into
// git's canonical "Key: value" form (a single space after the colon) and
// reports whether it is a well-formed single-line trailer.
func FormatTrailerLine(s string) (string, bool) {
	key, value, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return "", false
	}
	key = strings.TrimSpace(key)
	if !trailerKeyRe.MatchString(key) {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\n\r") {
		return "", false
	}
	return key + ": " + value, true
}

// DedupTrailers normalises each entry to "Key: value", drops malformed
// entries, and removes duplicates (case-insensitive key, trimmed value)
// while preserving first-seen order.
func DedupTrailers(trailers []string) []string {
	seen := make(map[string]bool, len(trailers))
	out := make([]string, 0, len(trailers))
	for _, t := range trailers {
		line, ok := FormatTrailerLine(t)
		if !ok {
			continue
		}
		key := normalizeTrailer(line)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, line)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// lastParagraphStart returns the index of the first line of the last
// whitespace-separated paragraph of lines (0 when there is no blank line).
func lastParagraphStart(lines []string) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			return i + 1
		}
	}
	return 0
}

// AppendTrailers appends each trailer to message with git interpret-trailers
// semantics: when message ends in a trailer block the new trailers join it,
// otherwise they start a new block after exactly one blank line. Identical
// trailers (case-insensitive key, trimmed value) are never added twice —
// neither against each other nor against trailers already present in the
// message. Malformed entries are skipped. A message with nothing to add is
// returned unchanged.
func AppendTrailers(message string, trailers ...string) string {
	new := DedupTrailers(trailers)
	if len(new) == 0 {
		return message
	}
	body := strings.TrimRight(message, " \t\r\n")
	if body == "" {
		return message
	}
	lines := strings.Split(body, "\n")

	start := lastParagraphStart(lines)
	hasBlock := start > 0 && isTrailerBlock(lines[start:])

	existing := make(map[string]bool)
	if hasBlock {
		for _, l := range lines[start:] {
			if k := trailerDedupKey(l); k != "" {
				existing[k] = true
			}
		}
	}

	add := make([]string, 0, len(new))
	for _, t := range new {
		if existing[normalizeTrailer(t)] {
			continue
		}
		existing[normalizeTrailer(t)] = true
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

// trailerDedupKey returns the dedup key for a line that starts a trailer
// (a "Key: value" line), or "" for continuation lines and non-trailers.
func trailerDedupKey(line string) string {
	if !trailerLineRe.MatchString(line) {
		return ""
	}
	return normalizeTrailer(line)
}

// DefaultCommitTrailers returns the default git trailers to append to a commit
// made in commitDir: the config's commit_trailers list followed by any
// trailers declared in the repository's AGENTS.md "Commit Trailers"
// section, deduplicated and normalised. The AGENTS.md is looked up at the
// repository top level (git rev-parse --show-toplevel from commitDir,
// falling back to commitDir itself).
func DefaultCommitTrailers(cfg *configuration.Config, commitDir string) []string {
	var out []string
	if cfg != nil {
		out = append(out, cfg.CommitTrailers...)
	}
	if commitDir != "" {
		out = append(out, AgentsMdCommitTrailers(AgentsMdPath(commitDir))...)
	}
	return DedupTrailers(out)
}

// AgentsMdPath returns the path of the repository's AGENTS.md for a commit
// made in dir: the git top level of dir (a worktree's root when dir is inside
// one), or dir itself when no repository is reachable.
func AgentsMdPath(dir string) string {
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output(); err == nil && len(out) > 0 { //nolint:gosec // G204: fixed subcommand, dir is the commit workdir
		return filepath.Join(strings.TrimSpace(string(out)), "AGENTS.md")
	}
	return filepath.Join(dir, "AGENTS.md")
}

// AgentsMdCommitTrailers reads path (typically the repo's AGENTS.md) and
// returns the git trailers declared in its "Commit Trailers" section: every
// "Key: value" line (optionally a markdown list item) up to the next heading
// of the same or higher level. Returns nil when the file or section is absent.
func AgentsMdCommitTrailers(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	var out []string
	inSection := false
	sectionLevel := 0
	for _, raw := range lines {
		line := strings.TrimLeft(raw, " \t")
		if strings.HasPrefix(line, "#") {
			level := 0
			for level < len(line) && line[level] == '#' {
				level++
			}
			if agentsMdHeadingRe.MatchString(line) {
				inSection = true
				sectionLevel = level
			} else if inSection && level <= sectionLevel {
				break
			}
			continue
		}
		if !inSection {
			continue
		}
		if m := agentsMdTrailerItemRe.FindStringSubmatch(raw); m != nil {
			out = append(out, m[1]+": "+strings.TrimSpace(m[2]))
		}
	}
	return out
}
