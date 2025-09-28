#!/bin/bash
set -euo pipefail

echo "--- UI Smoke Test (no network) ---"

# Build local binary
if [[ ! -x ./ledit ]]; then
  echo "No local ledit binary found; attempting to use repo root binary"
  if [[ -x ../../ledit ]]; then
    cp ../../ledit ./ledit
  else
    echo "No ledit binary available; aborting smoke test"
    exit 1
  fi
fi

echo "1) ledit --help should show top-level commands"
./ledit --help | grep -E "agent|commit|shell|version" >/dev/null || {
  echo "Help text missing core commands"; exit 1;
}

echo "2) ledit version should print version info"
./ledit version >/dev/null || { echo "Version command failed"; exit 1; }

echo "3) commit --dry-run with no staged changes should not require network"
git init >/dev/null 2>&1 || true
git config user.email "smoke@example.com"
git config user.name "Smoke Test"

# Ignore local artifacts so status is clean
echo -e "ledit\n.ledit/\n*.tmp" > .gitignore
git add .gitignore >/dev/null 2>&1 || true
git commit -m "chore: add .gitignore for smoke" >/dev/null 2>&1 || true

out=$(./ledit commit --dry-run 2>&1 || true)
echo "$out" | grep -E "No staged changes|No changes to commit|No staged changes to commit" >/dev/null || {
  echo "Commit dry-run did not exit cleanly without network"; echo "$out"; exit 1;
}

echo "4) Non-interactive help should not render footer hints"
out2=$(CI=1 ./ledit --help 2>&1 || true)
echo "$out2" | grep -q "Focus:" && { echo "Footer hint leaked into help output"; exit 1; }

echo "✓ UI smoke checks passed"
