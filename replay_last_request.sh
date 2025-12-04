#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: replay_last_request.sh [options]

Replay the last request that ledit sent to an LLM provider by re-sending
the contents of lastRequest.json to the provider endpoint defined in the
matching config file.

Options:
  -p, --provider <name>       Provider short name (default: zai)
  -c, --config <path>         Path to provider config JSON (defaults to pkg/agent_providers/configs/${provider}.json)
  -f, --request-file <path>   Request payload file (default: lastRequest.json)
  -h, --help                  Show this help message

Environment:
  <AUTH_ENV_VAR>              The provider config defines which environment variable contains the API key.
                              For example, ZAI uses ZAI_API_KEY.
  LEDIT_COPY_LOGS_TO_CWD=1    (optional) when replaying after a run that enabled log copying.

This script sends the request payload exactly as it appears in the log file,
so it is useful for debugging the provider outside of the agent.
EOF
}

provider="zai"
request_file="lastRequest.json"
config_path=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -p|--provider)
      provider="$2"
      shift 2
      ;;
    -c|--config)
      config_path="$2"
      shift 2
      ;;
    -f|--request-file)
      request_file="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ -z "$config_path" ]]; then
  config_path="pkg/agent_providers/configs/${provider}.json"
fi

if [[ ! -f "$request_file" ]]; then
  echo "Request file not found: $request_file" >&2
  exit 1
fi

if [[ ! -f "$config_path" ]]; then
  echo "Provider config not found: $config_path" >&2
  exit 1
fi

endpoint=$(jq -r '.endpoint' "$config_path")
auth_type=$(jq -r '.auth.type' "$config_path")
auth_env=$(jq -r '.auth.env_var' "$config_path")

if [[ "$endpoint" == "" || "$endpoint" == "null" ]]; then
  echo "Provider config does not define an endpoint in $config_path" >&2
  exit 1
fi

if [[ "$auth_type" == "bearer" || "$auth_type" == "api_key" ]]; then
  if [[ -z "$auth_env" || "$auth_env" == "null" ]]; then
    echo "Config does not specify auth.env_var" >&2
    exit 1
  fi
  if [[ -z "${!auth_env-}" ]]; then
    echo "Environment variable $auth_env must be set for provider ${provider}" >&2
    exit 1
  fi
  auth_header="Authorization: Bearer ${!auth_env}"
else
  echo "Unsupported auth type: $auth_type" >&2
  exit 1
fi

echo "Replaying ${request_file} to ${endpoint} (provider=${provider})"

curl -sSL \
  -H "Content-Type: application/json" \
  -H "$auth_header" \
  -o /tmp/provider-response.json \
  -w "\n\nHTTP Status: %{http_code}\nResponse saved: /tmp/provider-response.json\n" \
  -X POST "$endpoint" \
  --data-binary @"$request_file"

