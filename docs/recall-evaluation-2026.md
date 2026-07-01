# Semantic-Recall Evaluation (2026)

**Status:** Templated — to be populated after the 2-week observation window
elapses (SP-095-2). The instrumentation that produces the input data was
shipped in SP-095-1 (commit `cac85e39`).

## Source data

`~/.config/sprout/recall_metrics.jsonl` — one JSONL record per agent turn that
fired `InjectSemanticRecall`. Schema (defined in
`pkg/agent/semantic_recall_instrumentation.go::RecallMetricsRecord`):

| Field | Type | Meaning |
|---|---|---|
| `timestamp` | string (RFC3339) | When the recall ran. |
| `session_id` | string | Agent session that triggered recall. |
| `items_recalled` | int | How many `RecallItem`s came back. 0 = recall found nothing. |
| `top_similarity` | float | Highest cosine similarity in the result set. |
| `used_in_response` | bool | Was the recall set cited by the agent's response? |
| `cited_file_paths` | []string | File paths from the recall set (for post-hoc matching against logged responses). |
| `cited_session_ids` | []string | Session IDs from the recall set. |
| `recall_latency_ms` | int64 | Wall time for the `Recall()` call. |
| `recall_query` | string | First 200 chars of the recall query, for debugging. |

The metrics sink is a fire-and-forget JSONL appender. Disk I/O never blocks
the agent loop. Failures are silent at the agent level.

## Metrics to compute (run after window closes)

```bash
# Hit-rate: how often recall returns ≥1 item
awk -F'"items_recalled":' '{print $2}' ~/.config/sprout/recall_metrics.jsonl \
  | awk -F'[,\n]' '{print $1}' \
  | awk 'BEGIN{h=0;t=0} {t++; if ($1+0 > 0) h++} END{printf "hit-rate: %.1f%% (%d/%d)\n", h*100/t, h, t}'

# Use-rate: how often a surfaced item was actually cited by the agent
awk -F'"used_in_response":' '{print $2}' ~/.config/sprout/recall_metrics.jsonl \
  | awk -F'[,\n}]' '{print $1}' \
  | awk 'BEGIN{u=0;t=0} {t++; if ($1=="true") u++} END{printf "use-rate: %.1f%% (%d/%d)\n", u*100/t, u, t}'

# Average top-similarity when items are returned
awk -F'"top_similarity":' '{print $2}' ~/.config/sprout/recall_metrics.jsonl \
  | awk -F'[,\n}]' '{sum+=$1; n++} END{printf "avg top-similarity (when items>0): %.3f (n=%d)\n", sum/n, n}'

# Recall latency percentiles
awk -F'"recall_latency_ms":' '{print $2}' ~/.config/sprout/recall_metrics.jsonl \
  | awk -F'[,\n}]' '{print $1}' \
  | sort -n \
  | awk 'BEGIN{c=0} {a[c++]=$1} END{printf "p50=%dms p95=%dms p99=%dms max=%dms\n", a[int(c*0.5)], a[int(c*0.95)], a[int(c*0.99)], a[c-1]}'
```

(Replace the awk scripts with a small Go program if the JSONL grows large.)

## Per-tool breakdown

Once a baseline is established, also break down hit-rate by tool that
triggered recall (e.g. `read_file` is more likely to trigger useful recall
than `ls`). Join against the agent's tool-call log; the recall record's
`session_id` is the join key.

## Recommendation template

Once the metrics are populated, fill in:

- **Hit-rate**: ___% (target: ≥30%)
- **Use-rate**: ___% (target: ≥20%)
- **Avg top-similarity (when items>0)**: ___ (target: ≥0.65)
- **p95 latency**: ___ms (target: ≤250ms)

Recommendation (one of):

1. **Keep** — recall is hitting both bar targets and the cost (embed+query on
   every turn) is justified.
2. **Narrow** — hit-rate is acceptable but use-rate is low; restrict recall
   to only `read_file`/`grep` triggers, or only fire when similarity > 0.7.
3. **Kill** — neither hit-rate nor use-rate clears the threshold; the
   feature is dead weight and should be removed.
4. **Expand** — hit-rate is high but use-rate is low; the agent isn't
   reading the surfaced items. Add proactive hints in the steer panel
   showing the top-3 surfaced items so the user (and agent) sees them.

## Status

- [x] SP-095-1 instrumentation shipped.
- [ ] SP-095-2 2-week observation window (manual calendar step).
- [ ] SP-095-3 metrics populated, report written, recommendation made.