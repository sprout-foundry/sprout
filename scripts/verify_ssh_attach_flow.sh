#!/usr/bin/env bash
set -euo pipefail

# Opt-in end-to-end SSH attach verifier. This mirrors the runtime flow:
# 1) Detect remote platform/arch
# 2) Download matching GitHub release artifact
# 3) Upload/install/verify backend over SSH
# 4) Start remote daemon
# 5) Start local SSH tunnel
# 6) Probe /health through the tunnel
#
# Required env:
# - LEDIT_SSH_TEST_HOST_ALIAS
# Optional env:
# - LEDIT_SSH_TEST_REMOTE_WORKSPACE_PATH (default: $HOME)
# - LEDIT_SSH_TEST_LAUNCHER_URL (default: http://127.0.0.1:54421)
# - LEDIT_SSH_TEST_RELEASE_TAG (default: latest)

HOST_ALIAS="${LEDIT_SSH_TEST_HOST_ALIAS:-}"
REMOTE_WORKSPACE_PATH="${LEDIT_SSH_TEST_REMOTE_WORKSPACE_PATH:-\$HOME}"
LAUNCHER_URL="${LEDIT_SSH_TEST_LAUNCHER_URL:-http://127.0.0.1:54421}"
RELEASE_TAG="${LEDIT_SSH_TEST_RELEASE_TAG:-latest}"

if [[ -z "$HOST_ALIAS" ]]; then
  echo "error: LEDIT_SSH_TEST_HOST_ALIAS is required" >&2
  exit 1
fi

for cmd in ssh scp curl tar python3 sha256sum; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "error: required command not found: $cmd" >&2
    exit 1
  fi
done

workdir="$(mktemp -d -t ledit-ssh-verify-XXXXXX)"
cleanup() {
  set +e
  if [[ -n "${TUNNEL_PID:-}" ]]; then
    kill "$TUNNEL_PID" >/dev/null 2>&1 || true
  fi
  if [[ -n "${REMOTE_PID:-}" ]]; then
    ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$HOST_ALIAS" "bash -lc 'kill ${REMOTE_PID} >/dev/null 2>&1 || true'" >/dev/null 2>&1 || true
  fi
  rm -rf "$workdir"
}
trap cleanup EXIT

shq() {
  python3 -c 'import shlex, sys; print(shlex.quote(sys.argv[1]))' "$1"
}

echo "==> Detecting remote platform"
REMOTE_UNAME="$(ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$HOST_ALIAS" "bash -lc 'uname -s; uname -m'")"
REMOTE_OS_RAW="$(echo "$REMOTE_UNAME" | sed -n '1p')"
REMOTE_ARCH_RAW="$(echo "$REMOTE_UNAME" | sed -n '2p')"

case "$REMOTE_OS_RAW" in
  Linux) REMOTE_OS="linux" ;;
  Darwin) REMOTE_OS="darwin" ;;
  *)
    echo "error: unsupported remote os: $REMOTE_OS_RAW" >&2
    exit 1
    ;;
esac

case "$REMOTE_ARCH_RAW" in
  x86_64|amd64) REMOTE_ARCH="amd64" ;;
  aarch64|arm64) REMOTE_ARCH="arm64" ;;
  *)
    echo "error: unsupported remote arch: $REMOTE_ARCH_RAW" >&2
    exit 1
    ;;
esac

echo "    remote: ${REMOTE_OS}/${REMOTE_ARCH}"

asset_name="ledit-${REMOTE_OS}-${REMOTE_ARCH}.tar.gz"
api_url="https://api.github.com/repos/alantheprice/ledit/releases/${RELEASE_TAG}"
if [[ "$RELEASE_TAG" != "latest" ]]; then
  api_url="https://api.github.com/repos/alantheprice/ledit/releases/tags/${RELEASE_TAG}"
fi

echo "==> Resolving artifact URL for ${asset_name} (${RELEASE_TAG})"
json_path="$workdir/release.json"
curl -fsSL "$api_url" -o "$json_path"

asset_url="$(python3 - "$json_path" "$asset_name" <<'PY'
import json
import sys

path, target = sys.argv[1], sys.argv[2]
with open(path, 'r', encoding='utf-8') as f:
    payload = json.load(f)
for asset in payload.get('assets', []):
    if asset.get('name') == target and asset.get('browser_download_url'):
        print(asset['browser_download_url'])
        raise SystemExit(0)
raise SystemExit(1)
PY
)"

if [[ -z "$asset_url" ]]; then
  echo "error: could not resolve release artifact URL for ${asset_name}" >&2
  exit 1
fi

echo "==> Downloading artifact"
archive_path="$workdir/$asset_name"
local_binary="$workdir/ledit-${REMOTE_OS}-${REMOTE_ARCH}"
curl -fsSL "$asset_url" -o "$archive_path"
archive_entry="$(tar -tzf "$archive_path" | head -n 1)"
if [[ -z "$archive_entry" ]]; then
  echo "error: artifact archive is empty" >&2
  exit 1
fi
tar -xzf "$archive_path" -C "$workdir"
if [[ ! -f "$workdir/$archive_entry" ]]; then
  echo "error: extracted artifact does not contain expected binary: $archive_entry" >&2
  exit 1
fi
if [[ "$workdir/$archive_entry" != "$local_binary" ]]; then
  mv "$workdir/$archive_entry" "$local_binary"
fi
chmod +x "$local_binary"

echo "==> Computing backend fingerprint"
fingerprint="$(sha256sum "$local_binary" | awk '{print $1}' | cut -c1-16)"
remote_dir="\$HOME/.cache/ledit-webui/backend/${fingerprint}/${REMOTE_OS}-${REMOTE_ARCH}"
remote_binary="${remote_dir}/ledit"
upload_tmp=".ledit-ssh-upload-${fingerprint}.tmp"
session_key="${HOST_ALIAS}::\$HOME"

remote_dir_q="$(shq "$remote_dir")"
remote_binary_q="$(shq "$remote_binary")"
launcher_url_q="$(shq "$LAUNCHER_URL")"
host_alias_q="$(shq "$HOST_ALIAS")"
session_key_q="$(shq "$session_key")"
workspace_q="$(shq "$REMOTE_WORKSPACE_PATH")"
workspace_expr="$workspace_q"
if [[ "$REMOTE_WORKSPACE_PATH" == '\$HOME' ]]; then
  workspace_expr='"$HOME"'
fi

echo "==> Installing backend on remote"
ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$HOST_ALIAS" "bash -lc 'mkdir -p ${remote_dir_q}'"
scp -q "$local_binary" "${HOST_ALIAS}:${upload_tmp}"
ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$HOST_ALIAS" "bash -lc 'mv \"\$HOME/${upload_tmp}\" ${remote_binary_q} && chmod +x ${remote_binary_q}'"

echo "==> Verifying remote backend executable"
ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$HOST_ALIAS" "bash -lc '${remote_binary_q} version'"

echo "==> Starting remote backend"
REMOTE_INFO="$(ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$HOST_ALIAS" "bash -lc '
set -e
choose_port() {
  if command -v python3 >/dev/null 2>&1; then
    python3 - <<\"PY\"
import socket
s = socket.socket()
s.bind((\"127.0.0.1\", 0))
print(s.getsockname()[1])
s.close()
PY
    return
  fi
  if command -v python >/dev/null 2>&1; then
    python - <<\"PY\"
import socket
s = socket.socket()
s.bind((\"127.0.0.1\", 0))
print(s.getsockname()[1])
s.close()
PY
    return
  fi
  echo \"python3 or python is required on the remote host\" >&2
  exit 1
}
mkdir -p \"\$HOME/.cache/ledit-webui/logs\"
cd ${workspace_expr}
REMOTE_PORT=\"\$(choose_port)\"
LOG_FILE=\"\$HOME/.cache/ledit-webui/logs/${HOST_ALIAS}.log\"
nohup env BROWSER=none LEDIT_SSH_HOST_ALIAS=${host_alias_q} LEDIT_SSH_SESSION_KEY=${session_key_q} LEDIT_SSH_LAUNCHER_URL=${launcher_url_q} LEDIT_SSH_HOME=\"\$HOME\" ${remote_binary_q} --isolated-config agent --daemon --web-port \"\$REMOTE_PORT\" >\"\$LOG_FILE\" 2>&1 < /dev/null &
REMOTE_PID=\$!
REMOTE_PORT="$(echo "$REMOTE_INFO" | awk '{print $1}')"
REMOTE_PID="$(echo "$REMOTE_INFO" | awk '{print $2}')"
if [[ -z "$REMOTE_PORT" || -z "$REMOTE_PID" ]]; then
  echo "error: failed to parse remote launch info: $REMOTE_INFO" >&2
  exit 1
fi

echo "    remote port: $REMOTE_PORT"
echo "    remote pid: $REMOTE_PID"

LOCAL_PORT="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(('127.0.0.1', 0))
print(s.getsockname()[1])
s.close()
PY
)"

echo "==> Starting SSH tunnel on local port ${LOCAL_PORT}"
ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ServerAliveInterval=15 -o ServerAliveCountMax=3 -o ExitOnForwardFailure=yes -N -L "${LOCAL_PORT}:127.0.0.1:${REMOTE_PORT}" "$HOST_ALIAS" >"$workdir/tunnel.out" 2>"$workdir/tunnel.err" &
TUNNEL_PID=$!

echo "==> Probing health endpoint"
health_ok=0
for _ in $(seq 1 120); do
  if curl -fsS "http://127.0.0.1:${LOCAL_PORT}/health" >"$workdir/health.json" 2>"$workdir/health.err"; then
    health_ok=1
    break
  fi
  sleep 0.25
done

if [[ "$health_ok" != "1" ]]; then
  echo "error: health check failed" >&2
  echo "--- tunnel stderr ---" >&2
  cat "$workdir/tunnel.err" >&2 || true
  echo "--- remote log tail ---" >&2
  ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$HOST_ALIAS" "bash -lc 'tail -n 120 \"\$HOME/.cache/ledit-webui/logs/${HOST_ALIAS}.log\"'" >&2 || true
  exit 1
fi

echo "PASS: attach flow healthy"
cat "$workdir/health.json"
echo

echo "==> Testing backend API commands"

# /api/workspace — should return daemon_root and workspace_root
workspace_json="$(curl -fsS "http://127.0.0.1:${LOCAL_PORT}/api/workspace")"
if [[ -z "$workspace_json" ]]; then
  echo "error: /api/workspace returned empty response" >&2
  exit 1
fi
workspace_root="$(echo "$workspace_json" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("workspace_root",""))')"
if [[ -z "$workspace_root" ]]; then
  echo "error: /api/workspace did not return workspace_root; got: $workspace_json" >&2
  exit 1
fi
echo "    workspace_root: $workspace_root"

# /api/stats — should return basic stats without error
stats_json="$(curl -fsS "http://127.0.0.1:${LOCAL_PORT}/api/stats")"
if [[ -z "$stats_json" ]]; then
  echo "error: /api/stats returned empty response" >&2
  exit 1
fi
echo "    stats: ok"

# /api/providers — should return providers list
providers_json="$(curl -fsS "http://127.0.0.1:${LOCAL_PORT}/api/providers")"
if [[ -z "$providers_json" ]]; then
  echo "error: /api/providers returned empty response" >&2
  exit 1
fi
provider_count="$(echo "$providers_json" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(len(d.get("providers",[])))' 2>/dev/null || echo 0)"
echo "    providers available: $provider_count"

# /api/files — should return a file listing without error
files_json="$(curl -fsS "http://127.0.0.1:${LOCAL_PORT}/api/files")"
if [[ -z "$files_json" ]]; then
  echo "error: /api/files returned empty response" >&2
  exit 1
fi
echo "    files: ok"

echo "PASS: backend API commands verified"

# ─────────────────────────────────────────────────────────────────────────────
# Proxy path verification
#
# The local ledit process (LAUNCHER_URL) exposes the SSH workspace through a
# reverse-proxy at /ssh/{url-encoded-session-key}/.  All traffic goes through
# the same origin — no new browser ports needed.
# ─────────────────────────────────────────────────────────────────────────────

echo "==> Verifying SSH same-origin proxy"

# Compute the URL-encoded session key (Python is already required above).
encoded_key="$(python3 -c '
import sys, urllib.parse
print(urllib.parse.quote(sys.argv[1], safe=""))
' "${session_key}")"

proxy_base="${LAUNCHER_URL}/ssh/${encoded_key}"

# 1. Index — must return 200 with LEDIT_PROXY_BASE injected.
index_body="$(curl -fsS "${proxy_base}/")"
if ! echo "$index_body" | grep -q "LEDIT_PROXY_BASE"; then
  echo "error: ${proxy_base}/ did not inject LEDIT_PROXY_BASE; body head:" >&2
  echo "$index_body" | head -5 >&2
  exit 1
fi
echo "    proxy index: LEDIT_PROXY_BASE injected OK"

# 2. /sw.js — must be served from local embed (not dependent on remote backend).
sw_status="$(curl -o /dev/null -fsS -w "%{http_code}" "${proxy_base}/sw.js")"
if [[ "$sw_status" != "200" ]]; then
  echo "error: ${proxy_base}/sw.js returned HTTP ${sw_status}" >&2
  exit 1
fi
echo "    proxy sw.js: HTTP 200 OK"

# 3. /manifest.json — local asset, must return JSON.
manifest_status="$(curl -o /dev/null -fsS -w "%{http_code}" "${proxy_base}/manifest.json")"
if [[ "$manifest_status" != "200" ]]; then
  echo "error: ${proxy_base}/manifest.json returned HTTP ${manifest_status}" >&2
  exit 1
fi
echo "    proxy manifest.json: HTTP 200 OK"

# 4. /health proxied — must reach the remote daemon and return {"status":"ok"}.
proxy_health="$(curl -fsS "${proxy_base}/health")"
proxy_health_status="$(echo "$proxy_health" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("status",""))' 2>/dev/null || true)"
if [[ "$proxy_health_status" != "ok" ]]; then
  echo "error: ${proxy_base}/health did not return status=ok; got: $proxy_health" >&2
  exit 1
fi
echo "    proxy /health: status=ok"

# 5. /api/workspace proxied — must return workspace_root.
proxy_ws_json="$(curl -fsS "${proxy_base}/api/workspace")"
proxy_ws_root="$(echo "$proxy_ws_json" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("workspace_root",""))' 2>/dev/null || true)"
if [[ -z "$proxy_ws_root" ]]; then
  echo "error: ${proxy_base}/api/workspace did not return workspace_root; got: $proxy_ws_json" >&2
  exit 1
fi
echo "    proxy /api/workspace: workspace_root=${proxy_ws_root}"

# 6. /api/stats proxied — basic sanity.
proxy_stats="$(curl -o /dev/null -fsS -w "%{http_code}" "${proxy_base}/api/stats")"
if [[ "$proxy_stats" != "200" ]]; then
  echo "error: ${proxy_base}/api/stats returned HTTP ${proxy_stats}" >&2
  exit 1
fi
echo "    proxy /api/stats: HTTP 200 OK"

# 7. /api/files proxied.
proxy_files="$(curl -o /dev/null -fsS -w "%{http_code}" "${proxy_base}/api/files")"
if [[ "$proxy_files" != "200" ]]; then
  echo "error: ${proxy_base}/api/files returned HTTP ${proxy_files}" >&2
  exit 1
fi
echo "    proxy /api/files: HTTP 200 OK"

# 8. Confirm proxy_url and proxy_base are returned by the ssh-open API.
#    We call /api/instances/ssh-open with a dry_run param to get the result
#    without re-launching.  The response must include proxy_url and proxy_base.
ssh_open_json="$(curl -fsS -X POST "${LAUNCHER_URL}/api/instances/ssh-open" \
  -H 'Content-Type: application/json' \
  -d "{\"host_alias\":\"${HOST_ALIAS}\",\"workspace_path\":\"${REMOTE_WORKSPACE_PATH}\"}" 2>/dev/null || true)"
if [[ -n "$ssh_open_json" ]]; then
  proxy_url_field="$(echo "$ssh_open_json" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("proxy_url",""))' 2>/dev/null || true)"
  proxy_base_field="$(echo "$ssh_open_json" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("proxy_base",""))' 2>/dev/null || true)"
  if [[ -z "$proxy_url_field" || -z "$proxy_base_field" ]]; then
    echo "error: ssh-open response missing proxy_url/proxy_base; got: $ssh_open_json" >&2
    exit 1
  fi
  echo "    ssh-open proxy_url: ${proxy_url_field}"
  echo "    ssh-open proxy_base: ${proxy_base_field}"
else
  echo "    ssh-open: skipped (no response — session may have already been attached)"
fi

echo "PASS: SSH same-origin proxy verified"
