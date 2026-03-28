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
printf \"%s %s\\n\" \"\$REMOTE_PORT\" \"\$REMOTE_PID\"'" )"

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
