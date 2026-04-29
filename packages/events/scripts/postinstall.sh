#!/usr/bin/env bash
# Post-install script for @sprout/events
# Replaces react and @types/react with symlinks to the nearest consumer's
# installation to prevent dual-React issues.
set -euo pipefail

[ -d "node_modules" ] || exit 0

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PKG_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Find the webui's node_modules/react by walking up from the monorepo root.
# We look for a sibling "webui" directory with node_modules/react.
consumer_react=""
dir="$PKG_DIR"
while [ "$dir" != "/" ]; do
  if [ -f "$dir/webui/node_modules/react/package.json" ]; then
    consumer_react="$dir/webui/node_modules/react"
    break
  fi
  dir="$(dirname "$dir")"
done

[ -n "$consumer_react" ] || exit 0

# Symlink react
if [ -e "node_modules/react" ] && [ ! -L "node_modules/react" ]; then
  rm -rf node_modules/react
  ln -s "$(realpath --relative-to="$PKG_DIR/node_modules" "$consumer_react")" node_modules/react
fi

# Symlink @types/react (peer of react)
consumer_types="$consumer_react/../@types/react"
if [ -d "$consumer_types" ] && [ -e "node_modules/@types/react" ] && [ ! -L "node_modules/@types/react" ]; then
  rm -rf node_modules/@types/react
  ln -s "$(realpath --relative-to="$PKG_DIR/node_modules/@types" "$consumer_types")" node_modules/@types/react
fi
