#!/usr/bin/env bash
# Checks AI-tool attribution in commit messages. AI-assisted work is
# recorded with a plain git trailer, never as a co-author:
#
#   Assisted-by: <tool>[/<version>] (<provider>/<model>)
#
# Rejected: Co-Authored-By trailers that name an AI assistant or agent,
# "Generated with …" footers, malformed Assisted-by lines, and a repeated
# Assisted-by line (one per tool+model).
#
# Usage:
#   scripts/check-commit-attribution.sh --message-file <path>   # commit-msg hook
#   scripts/check-commit-attribution.sh <rev-range>             # e.g. origin/main..HEAD
set -euo pipefail

AI_NAMES='claude|anthropic|openai|chatgpt|gpt-?[0-9]|copilot|gemini|cursor|codex|aider|devin|windsurf'
CO_AUTHOR_RE="^[[:space:]]*co-authored-by:.*(${AI_NAMES})"
FOOTER_RE='generated (with|by) .*(claude|chatgpt|copilot|gemini|cursor|codex|aider|devin|windsurf)'
# Tool names may contain spaces ("Claude Code"); the version, provider and
# model may not. The model may contain further slashes and colons.
ASSISTED_KEY_RE='^[[:space:]]*assisted-by:'
ASSISTED_RE='^Assisted-by: [A-Za-z0-9][A-Za-z0-9 ._-]*[A-Za-z0-9._-](/[A-Za-z0-9][A-Za-z0-9._+-]*)? \([A-Za-z0-9][A-Za-z0-9._-]*/[^()[:space:]]+\)[[:space:]]*$'

# check_message reads a commit message on stdin and prints one line per
# problem; no output means the message is clean.
check_message() {
  local msg hits
  msg=$(grep -v '^#' || true)
  hits=$(grep -inE "${CO_AUTHOR_RE}|${FOOTER_RE}" <<<"$msg" || true)
  if [[ -n "$hits" ]]; then
    printf 'AI-tool attribution (use an Assisted-by trailer instead): %s\n' "$hits"
  fi
  local assisted
  assisted=$(grep -iE "${ASSISTED_KEY_RE}" <<<"$msg" || true)
  [[ -z "$assisted" ]] && return 0
  local line
  while IFS= read -r line; do
    if ! grep -qE "${ASSISTED_RE}" <<<"$line"; then
      printf 'malformed trailer, expected "Assisted-by: <tool>[/<version>] (<provider>/<model>)": %s\n' "$line"
    fi
  done <<<"$assisted"
  local dups
  dups=$(sed -E 's/[[:space:]]+$//' <<<"$assisted" | sort | uniq -d)
  if [[ -n "$dups" ]]; then
    printf 'duplicate trailer, one per tool+model: %s\n' "$dups"
  fi
}

if [[ "${1:-}" == "--message-file" ]]; then
  file="${2:?message file required}"
  problems=$(check_message <"$file")
  if [[ -n "$problems" ]]; then
    echo "commit message attribution check failed:" >&2
    echo "$problems" >&2
    exit 1
  fi
  exit 0
fi

range="${1:?usage: $0 --message-file <path> | <rev-range>}"
status=0
while read -r sha; do
  [[ -z "$sha" ]] && continue
  problems=$(git log -1 --format=%B "$sha" | check_message)
  if [[ -n "$problems" ]]; then
    echo "commit $(git log -1 --format='%h %s' "$sha") fails the attribution check:" >&2
    echo "$problems" >&2
    status=1
  fi
done < <(git rev-list --no-merges "$range")
if [[ $status -ne 0 ]]; then
  echo "AI assistance is recorded as an Assisted-by trailer, never Co-Authored-By. Reword the commits before merging." >&2
fi
exit $status
