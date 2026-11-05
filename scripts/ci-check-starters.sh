#!/usr/bin/env bash
# End-to-end check of the reference starters.
#
# For each user-facing starter this instantiates it into a scratch directory,
# installs its pinned dependencies, runs the build and test commands its
# manifest declares, then starts the manifest's dev server and asserts every
# manifest route answers HTTP 200 before stopping it again.
#
# The commands are never hardcoded here: they are read from the instantiated
# project's .sprout/starter.json, so the starter (not this script) owns how it
# is built, tested and served. This is the job that proves a starter actually
# works — instantiates, builds, passes its own test, and serves its routes —
# rather than only that its tree is well-formed.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
sprout_bin="${SPROUT_BIN:-$repo_root/sprout}"
work_root="${STARTERS_WORKDIR:-$(mktemp -d -t sprout-starters.XXXXXX)}"
# Generous because a cold `npm ci` plus a first Astro/Vite/Wrangler start on a
# shared CI runner is far slower than a warm local one.
dev_start_timeout="${STARTERS_DEV_TIMEOUT:-120}"

if [ ! -x "$sprout_bin" ]; then
  echo "sprout binary not found or not executable at $sprout_bin" >&2
  echo "build it first (make build) or point SPROUT_BIN at it" >&2
  exit 1
fi

mkdir -p "$work_root"

# Starters to exercise. No argument lists the user-facing starters (the
# test-only fixture is withheld); explicit arguments let a caller re-run a
# single starter locally.
if [ "$#" -gt 0 ]; then
  starters=("$@")
else
  # `sprout new` with no starter prints the user-facing catalogue (the
  # test-only fixture is withheld). Collect the ids from its indented lines.
  # No `mapfile` here: the CI runner's bash has it, but macOS's default bash
  # 3.2 — where developers run this script — does not.
  starters=()
  while IFS= read -r id; do
    [ -n "$id" ] && starters+=("$id")
  done < <("$sprout_bin" new 2>/dev/null | sed -n 's/^  \([^ ]*\) (v.*$/\1/p')
  if [ "${#starters[@]}" -eq 0 ]; then
    echo "could not discover any starters from '$sprout_bin new'" >&2
    exit 1
  fi
fi

manifest_string() {
  python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get(sys.argv[2], ""))' "$1" "$2"
}

check_routes() {
  local base_url="$1" routes_json="$2" log_file="$3"
  local routes failed=0
  routes="$(printf '%s' "$routes_json" | python3 -c 'import json,sys; print("\n".join(json.load(sys.stdin)))')"
  while IFS= read -r route; do
    [ -n "$route" ] || continue
    url="${base_url}${route}"
    code=""
    for _ in $(seq 1 30); do
      # A route can answer non-200 while the server is still warming (Wrangler
      # finishes binding a moment after the port opens, and its asset fallback
      # lags the first request); retry briefly rather than judging one probe.
      code="$(curl -s -o /dev/null -w '%{http_code}' "$url" || true)"
      if [ "$code" = "200" ]; then break; fi
      if ! kill -0 "$dev_pid" 2>/dev/null; then
        echo "  $url: dev server exited before it answered (see $log_file)" >&2
        tail -n 30 "$log_file" >&2 || true
        return 1
      fi
      sleep 1
    done
    if [ "$code" = "200" ]; then
      echo "  $url -> 200"
    else
      echo "  $url -> ${code:-<no response>} (expected 200)" >&2
      failed=1
    fi
  done <<< "$routes"
  return "$failed"
}

check_starter() {
  local id="$1"
  local dir="$work_root/$id"
  local log="$work_root/$id.dev.log"
  local manifest="$work_root/$id/.sprout/starter.json"

  echo "=== starter: $id ==="
  rm -rf "$dir"
  "$sprout_bin" new --starter "$id" "$dir"

  if [ ! -f "$manifest" ]; then
    echo "instantiated project has no manifest at $manifest" >&2
    return 1
  fi

  local build_cmd test_cmd dev_cmd dev_port migrate_cmd
  build_cmd="$(manifest_string "$manifest" build)"
  test_cmd="$(manifest_string "$manifest" test)"
  dev_cmd="$(manifest_string "$manifest" dev)"
  dev_port="$(manifest_string "$manifest" dev_port)"
  # Local D1 migrations are a package.json script the starter's README
  # documents (one named db:migrate, when the stack has a database). Run it
  # after the build and before the dev server so a data-backed starter's
  # routes have their tables; starters without one simply skip it.
  migrate_cmd="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("scripts", {}).get("db:migrate", ""))' "$dir/package.json" 2>/dev/null || true)"

  if [ -z "$build_cmd" ] || [ -z "$test_cmd" ] || [ -z "$dev_cmd" ] || [ -z "$dev_port" ]; then
    echo "manifest must declare build, test, dev and dev_port" >&2
    return 1
  fi

  (
    cd "$dir"
    echo "--- npm ci"
    npm ci
    if [ -n "${STARTERS_EXTRA_SETUP:-}" ]; then
      echo "--- extra setup: $STARTERS_EXTRA_SETUP"
      bash -c "$STARTERS_EXTRA_SETUP"
    fi
    echo "--- build: $build_cmd"
    bash -c "$build_cmd"
    if [ -n "$migrate_cmd" ]; then
      echo "--- migrate (local): npm run db:migrate"
      npm run db:migrate
    fi
    echo "--- test: $test_cmd"
    bash -c "$test_cmd"
  )

  echo "--- dev: $dev_cmd (port $dev_port)"
  ( cd "$dir" && exec bash -c "$dev_cmd" ) > "$log" 2>&1 &
  dev_pid=$!

  local base_url="http://localhost:$dev_port"
  local ready=1
  for _ in $(seq 1 "$dev_start_timeout"); do
    # A dev server that binds only one loopback family is not reachable over
    # the other, and curl reports "connection refused" the same way it reports
    # "not up yet" — so probe both before waiting another second.
    if curl -s -o /dev/null "http://127.0.0.1:$dev_port/" 2>/dev/null \
      || curl -s -o /dev/null "http://[::1]:$dev_port/" 2>/dev/null; then
      ready=0
      break
    fi
    if ! kill -0 "$dev_pid" 2>/dev/null; then
      echo "dev server exited before it accepted connections (see $log)" >&2
      tail -n 30 "$log" >&2 || true
      return 1
    fi
    sleep 1
  done
  if [ "$ready" -ne 0 ]; then
    echo "dev server did not accept connections within ${dev_start_timeout}s (see $log)" >&2
    tail -n 30 "$log" >&2 || true
    kill "$dev_pid" 2>/dev/null || true
    return 1
  fi

  routes_json="$(python3 -c 'import json,sys; print(json.dumps(json.load(open(sys.argv[1])).get("routes", [])))' "$manifest")"
  # Retry each route a few times before judging it: on a cold start the SPA
  # client routes of the data-backed starter 404 until the Worker's asset
  # directory is warm, so a single early probe is not a reliable verdict.
  local status=0 attempt
  for attempt in 1 2; do
    status=0
    check_routes "$base_url" "$routes_json" "$log" || status=1
    [ "$status" -eq 0 ] && break
    [ "$attempt" -eq 1 ] && echo "  retrying routes"
  done

  # Kill the whole process group: `npm run dev` spawns the real server as a
  # child, so signalling only the npm wrapper would leave it listening.
  kill "$dev_pid" 2>/dev/null || true
  pkill -P "$dev_pid" 2>/dev/null || true
  wait "$dev_pid" 2>/dev/null || true
  return "$status"
}

# check_hosted serves a starter's BUILD OUTPUT the way the hosted platform
# does, not its sources: the files of the build output with the Worker the
# build bundled (worker.js) as the main module, the project's [assets]
# settings, its data bindings, and the platform's compatibility date and
# flags (HOSTED_COMPAT_DATE follows the platform's upload contract). A
# starter whose Worker never reaches the build output, or whose client routes
# or API do not answer once uploaded, fails here even when `wrangler dev` on
# its sources works. A starter without a Worker (no `main` in wrangler.toml)
# has nothing to check beyond its dev server.
check_hosted() {
  local id="$1" dir="$work_root/$1" manifest="$work_root/$1/.sprout/starter.json"
  local out hosted="$work_root/$1.hosted" log="$work_root/$1.hosted.log"
  [ -f "$dir/wrangler.toml" ] && grep -Eq '^main *=' "$dir/wrangler.toml" || return 0
  out="$(manifest_string "$manifest" build_output)"
  echo "--- hosted: $out/ served as the platform uploads it"
  if [ ! -s "$dir/$out/worker.js" ]; then
    echo "the build did not produce $out/worker.js, so a hosted app would have no API" >&2
    return 1
  fi
  rm -rf "$hosted" && mkdir -p "$hosted/site"
  cp -R "$dir/$out/." "$hosted/site/"
  mv "$hosted/site/worker.js" "$hosted/worker.js"
  rm -f "$hosted/site/.assetsignore"
  [ -d "$dir/drizzle" ] && cp -R "$dir/drizzle" "$hosted/"
  python3 - "$dir/wrangler.toml" "$hosted/wrangler.toml" "${HOSTED_COMPAT_DATE:-2025-01-01}" <<'PY'
import re, sys
src, dst, date = sys.argv[1:4]
text = open(src).read()
text = re.sub(r'(?m)^main *=.*$', 'main = "worker.js"', text)
text = re.sub(r'(?m)^compatibility_date *=.*$', 'compatibility_date = "%s"' % date, text)
text = re.sub(r'(?m)^directory *=.*$', 'directory = "./site"', text)
# The platform uploads with no compatibility flags, so neither does this.
text = re.sub(r'(?m)^compatibility_flags *=.*$', '', text)
open(dst, "w").write(text)
PY
  local wrangler="$dir/node_modules/.bin/wrangler" db
  db="$(sed -n 's/^database_name *= *"\(.*\)"/\1/p' "$hosted/wrangler.toml" | head -1)"
  if [ -n "$db" ]; then
    ( cd "$hosted" && "$wrangler" d1 migrations apply "$db" --local ) >> "$log" 2>&1
  fi
  local port
  port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')"
  ( cd "$hosted" && exec "$wrangler" dev --local --port "$port" ) >> "$log" 2>&1 &
  dev_pid=$!
  local routes_json status=0
  routes_json="$(python3 -c 'import json,sys; print(json.dumps(json.load(open(sys.argv[1])).get("routes", [])))' "$manifest")"
  check_routes "http://127.0.0.1:$port" "$routes_json" "$log" || status=1
  kill "$dev_pid" 2>/dev/null || true
  pkill -P "$dev_pid" 2>/dev/null || true
  wait "$dev_pid" 2>/dev/null || true
  return "$status"
}

trap 'jobs -pr | xargs -r kill 2>/dev/null || true' EXIT

for starter in "${starters[@]}"; do
  check_starter "$starter"
  check_hosted "$starter"
done

echo "All starters passed: ${starters[*]}"
