# WASM shell escalation-rate projection

- Corpus: 3 session file(s), 15312 shell_command invocations
- Before: ~34 pure file/text builtins (ETH-2 baseline); git absent → 127 → txn
- After: read-only git subcommands + &&/||/; chains + /dev/null/2>&1 +
  grep/find/ls/wc/cat/head/tail flag coverage (this change)

## Escalation rate (txns per command invocation)

| metric | before | after | delta |
|---|---|---|---|
| in-browser runnable | 2419/15312 (15.8%) | 4630/15312 (30.2%) | +2211 cmds |
| escalations (127 → txn) | 12893 | 10682 | −2211 |
| escalation rate | 84.2% | 69.8% | −14.4 pts |

## Remaining 127 drivers (top segments)

| command | blocked segments |
|---|---|
|  | 3674 |
| go | 1412 |
| python3 | 677 |
| git | 552 |
| do | 468 |
| ps | 424 |
| python | 346 |
| cargo | 342 |
| grep | 341 |
| ruby | 306 |
| curl | 302 |
| timeout | 300 |
| find | 270 |
| source | 261 |
| cmake | 259 |

## Method

Each logged `shell_command` invocation is split into chain/pipeline
segments; the invocation runs in-browser only when every segment is
answerable by the wasmshell allowlist (a single unknown segment 127s
the whole invocation into a container txn). Flag support is checked
per command against the before/after flag matrices above.
