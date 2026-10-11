#!/usr/bin/env bash
# Rejects AI-tool attribution in commit messages: Co-Authored-By trailers that
# name an AI assistant or agent, and "Generated with …" footers. Coding tools
# are tools; commits carry no tool attribution, whichever tool helped.
#
# Usage:
#   scripts/check-commit-attribution.sh --message-file <path>   # commit-msg hook
#   scripts/check-commit-attribution.sh <rev-range>             # e.g. origin/main..HEAD
set -euo pipefail

AI_NAMES='claude|anthropic|openai|chatgpt|gpt-?[0-9]|copilot|gemini|cursor|codex|aider|devin|windsurf'
CO_AUTHOR_RE="^[[:space:]]*co-authored-by:.*(${AI_NAMES})"
FOOTER_RE='generated (with|by) .*(claude|chatgpt|copilot|gemini|cursor|codex|aider|devin|windsurf)'

offending_lines() {
  grep -inE "${CO_AUTHOR_RE}|${FOOTER_RE}" || true
}

if [[ "${1:-}" == "--message-file" ]]; then
  file="${2:?message file required}"
  hits=$(grep -v '^#' "$file" | offending_lines)
  if [[ -n "$hits" ]]; then
    echo "commit message carries AI-tool attribution; remove it:" >&2
    echo "$hits" >&2
    exit 1
  fi
  exit 0
fi

range="${1:?usage: $0 --message-file <path> | <rev-range>}"
status=0
while read -r sha; do
  [[ -z "$sha" ]] && continue
  hits=$(git log -1 --format=%B "$sha" | offending_lines)
  if [[ -n "$hits" ]]; then
    echo "commit $(git log -1 --format='%h %s' "$sha") carries AI-tool attribution:" >&2
    echo "$hits" >&2
    status=1
  fi
done < <(git rev-list --no-merges "$range")
if [[ $status -ne 0 ]]; then
  echo "Commits carry no tool attribution. Reword them (and drop the trailer) before merging." >&2
fi
exit $status
