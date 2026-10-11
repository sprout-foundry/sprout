#!/usr/bin/env bash
#
# check-commit-attribution_test.sh — Tests for scripts/check-commit-attribution.sh
#
# Run: bash scripts/check-commit-attribution_test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECK="$SCRIPT_DIR/check-commit-attribution.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

PASS=0
FAIL=0

# expect <accept|reject> <name> <message>
expect() {
  local want="$1" name="$2" msg="$3" got
  printf '%s\n' "$msg" >"$TMP/msg"
  if "$CHECK" --message-file "$TMP/msg" 2>"$TMP/err"; then
    got=accept
  else
    got=reject
  fi
  if [[ "$got" == "$want" ]]; then
    PASS=$((PASS + 1))
  else
    FAIL=$((FAIL + 1))
    echo "FAIL: $name: want $want, got $got" >&2
    sed 's/^/  /' "$TMP/err" >&2
  fi
}

expect accept "plain message" 'fix: handle empty input

Body text.'

expect accept "sprout trailer" 'feat: add thing

Assisted-by: sprout/0.30.0 (deepinfra/deepseek-ai/DeepSeek-V4.1-Flash)'

expect accept "dev build, several models, other trailers" 'feat: add thing

Signed-off-by: Jane Doe <jane@example.com>
Assisted-by: sprout/dev (deepinfra/deepseek-ai/DeepSeek-V4.1-Flash)
Assisted-by: sprout/dev (ollama/llama3:8b)'

expect accept "tool without version" 'docs: rule

Assisted-by: Claude Code (anthropic/claude-opus-5-5)'

expect accept "human co-author" 'fix: x

Co-Authored-By: Jane Doe <jane@example.com>'

expect accept "comment lines ignored" 'fix: x
# Co-Authored-By: Claude <noreply@anthropic.com>'

expect reject "AI co-author" 'fix: x

Co-Authored-By: Claude <noreply@anthropic.com>'

expect reject "AI co-author, lower case" 'fix: x

co-authored-by: GitHub Copilot <copilot@github.com>'

expect reject "generated footer" 'fix: x

Generated with [Claude Code](https://claude.com/claude-code)'

expect reject "co-author next to valid trailer" 'fix: x

Assisted-by: sprout/0.30.0 (openai/gpt-5)
Co-Authored-By: Codex <codex@openai.com>'

expect reject "missing provider/model" 'fix: x

Assisted-by: sprout/0.30.0'

expect reject "model without provider" 'fix: x

Assisted-by: sprout/0.30.0 (gpt-5)'

expect reject "no tool" 'fix: x

Assisted-by: (openai/gpt-5)'

expect reject "space in model" 'fix: x

Assisted-by: sprout/0.30.0 (openai/gpt 5)'

expect reject "trailing junk" 'fix: x

Assisted-by: sprout/0.30.0 (openai/gpt-5) and friends'

expect reject "wrong key case" 'fix: x

ASSISTED-BY: sprout/0.30.0 (openai/gpt-5)'

expect reject "duplicate trailer" 'fix: x

Assisted-by: sprout/0.30.0 (openai/gpt-5)
Assisted-by: sprout/0.30.0 (openai/gpt-5)'

# Rev-range mode: a throwaway repo with one clean and one bad commit.
repo="$TMP/repo"
git init -q "$repo"
git -C "$repo" -c user.name=t -c user.email=t@example.com commit -q --allow-empty -m 'chore: base'
git -C "$repo" -c user.name=t -c user.email=t@example.com commit -q --allow-empty \
  -m 'feat: ok' -m 'Assisted-by: sprout/dev (openai/gpt-5)'
if (cd "$repo" && "$CHECK" HEAD~1..HEAD 2>/dev/null); then
  PASS=$((PASS + 1))
else
  FAIL=$((FAIL + 1)); echo "FAIL: range with valid trailer rejected" >&2
fi
git -C "$repo" -c user.name=t -c user.email=t@example.com commit -q --allow-empty \
  -m 'feat: bad' -m 'Assisted-by: sprout'
if (cd "$repo" && "$CHECK" HEAD~2..HEAD 2>/dev/null); then
  FAIL=$((FAIL + 1)); echo "FAIL: range with malformed trailer accepted" >&2
else
  PASS=$((PASS + 1))
fi

echo "check-commit-attribution: $PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]
