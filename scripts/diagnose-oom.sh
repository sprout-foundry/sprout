#!/usr/bin/env bash
#
# diagnose-oom.sh — OOM-killer forensic helper
#
# SP-104: During a vitest worker pool runaway, the root-cause search was
# slowed because kernel OOM traces were in /var/log/syslog (rsyslog's
# kernel facility), NOT /var/log/kern.log (which had a rotation gap).
# This script checks all the places OOM traces can land and presents a
# unified summary.
#
# Usage:
#   scripts/diagnose-oom.sh                 # scan current boot
#   scripts/diagnose-oom.sh --boot -1       # scan previous boot
#   scripts/diagnose-oom.sh --since "1 hour ago"
#   scripts/diagnose-oom.sh --json          # machine-readable output
#
set -euo pipefail

BOOT=""
SINCE=""
JSON=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --boot) BOOT="$2"; shift 2 ;;
    --since) SINCE="$2"; shift 2 ;;
    --json) JSON=true; shift ;;
    -h|--help)
      echo "Usage: $0 [--boot N] [--since 'TIME'] [--json]"
      echo "  --boot N     Scan boot N (0=current, -1=previous). Default: current."
      echo "  --since TIME Show events since TIME (journalctl --since syntax)."
      echo "  --json       Output JSON for programmatic consumption."
      exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

# ── Sources to check ────────────────────────────────────────────────
# The kernel OOM-killer writes to the kernel ring buffer, which is then
# captured by journald (kern facility) and/or rsyslog. We check all
# three sources because any one may be missing:
#   - journald:    primary on systemd hosts
#   - /var/log/syslog: rsyslog's combined facility (often has traces kern.log misses)
#   - /var/log/kern.log: rsyslog's kernel-only log (rotation gaps can miss recent events)

JOURNAL_OPTS=()
[[ -n "$BOOT" ]] && JOURNAL_OPTS+=(--boot "$BOOT")
[[ -n "$SINCE" ]] && JOURNAL_OPTS+=(--since "$SINCE")

oom_count=0
oom_details=()

scan_journal() {
  local out
  if out=$(journalctl "${JOURNAL_OPTS[@]}" -k --no-pager \
             -g "invoked oom-killer|Out of memory|Killed process" 2>/dev/null); then
    local count
    count=$(echo "$out" | grep -c "invoked oom-killer" || true)
    if [[ $count -gt 0 ]]; then
      oom_count=$((oom_count + count))
      oom_details+=("SOURCE: journald (kernel facility)")
      oom_details+=("$out")
    fi
  fi
}

scan_syslog() {
  for f in /var/log/syslog /var/log/messages; do
    [[ -r "$f" ]] || continue
    local out
    if out=$(grep -h "invoked oom-killer\|Out of memory\|Killed process" "$f" 2>/dev/null); then
      local count
      count=$(echo "$out" | grep -c "invoked oom-killer" || true)
      if [[ $count -gt 0 ]]; then
        oom_count=$((oom_count + count))
        oom_details+=("SOURCE: $f")
        oom_details+=("$out")
      fi
    fi
  done
}

scan_kernlog() {
  for f in /var/log/kern.log /var/log/kern; do
    [[ -r "$f" ]] || continue
    local out
    if out=$(grep -h "invoked oom-killer\|Out of memory\|Killed process" "$f" 2>/dev/null); then
      local count
      count=$(echo "$out" | grep -c "invoked oom-killer" || true)
      if [[ $count -gt 0 ]]; then
        oom_count=$((oom_count + count))
        oom_details+=("SOURCE: $f")
        oom_details+=("$out")
      fi
    fi
  done
}

scan_dmesg() {
  # dmesg only covers the current boot's ring buffer
  if [[ -z "$BOOT" || "$BOOT" == "0" ]]; then
    local out
    if out=$(dmesg 2>/dev/null | grep "invoked oom-killer\|Out of memory\|Killed process"); then
      local count
      count=$(echo "$out" | grep -c "invoked oom-killer" || true)
      if [[ $count -gt 0 ]]; then
        oom_count=$((oom_count + count))
        oom_details+=("SOURCE: dmesg (kernel ring buffer)")
        oom_details+=("$out")
      fi
    fi
  fi
}

scan_journal
scan_syslog
scan_kernlog
scan_dmesg

# ── Output ──────────────────────────────────────────────────────────
if $JSON; then
  echo "{"
  echo "  \"oom_events\": $oom_count,"
  echo "  \"boot\": \"${BOOT:-current}\","
  echo "  \"since\": \"${SINCE:-all}\","
  echo "  \"sources_checked\": [\"journald\", \"/var/log/syslog\", \"/var/log/kern.log\", \"dmesg\"]"
  echo "}"
else
  if [[ $oom_count -eq 0 ]]; then
    echo "No OOM-killer events found."
    echo ""
    echo "Sources checked: journald, /var/log/syslog, /var/log/kern.log, dmesg"
    [[ -n "$BOOT" ]] && echo "Boot: $BOOT"
    [[ -n "$SINCE" ]] && echo "Since: $SINCE"
  else
    echo "=== OOM-Killer Events: $oom_count ==="
    [[ -n "$BOOT" ]] && echo "Boot: $BOOT"
    [[ -n "$SINCE" ]] && echo "Since: $SINCE"
    echo ""
    printf '%s\n' "${oom_details[@]}"
    echo ""
    echo "=== Current memory state ==="
    free -h
    echo ""
    echo "=== Top 15 processes by RSS ==="
    ps -eo pid,rss,comm --sort -rss | head -16
  fi
fi
