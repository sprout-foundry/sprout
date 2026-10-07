# Agent benchmark — 2026-10-04 (sprout v0.0.0-golden)

Models: prov-a/alpha, prov-b/beta (3 runs per task)

## Pass rate per starter per model

| Starter | Model | Tasks | Runs | Passed | Pass rate |
|---|---|---|---|---|---|
| fixture | prov-a/alpha | 2 | 6 | 3 | 3/6 (50%) |
| fixture | prov-b/beta | 2 | 6 | 2 | 2/6 (33%) |

## Failure categories

| Category | Failed runs |
|---|---|
| build | 4 |
| error | 1 |
| interaction | 1 |
| page | 1 |
| stopped_by_rule | 1 |
| test | 1 |

## Task detail

### add-badge — fixture — prov-a/alpha

| Run | Result | Turns | Tokens | Cost | Repair rounds | Wall |
|---|---|---|---|---|---|---|
| 1 | pass | 1 | 250 | $0.0011 | 0 | 1.234s |
| 2 | pass | 1 | 310 | $0.0013 | 0 | 2.5s |
| 3 | pass | 1 | 290 | $0.0012 | 0 | 2.015s |

### dark-mode — fixture — prov-a/alpha

| Run | Result | Turns | Tokens | Cost | Repair rounds | Wall |
|---|---|---|---|---|---|---|
| 1 | fail | 1 | 500 | $0.0031 | 0 | 5.6s |
| 2 | fail | 1 | 520 | $0.0033 | 0 | 6.1s |
| 3 | error: benchmark: instantiate starter "fixture" for task dark-mode run 3: harness: simulate setup failure | 0 | 0 | $0.0000 | 0 | 500ms |

### add-badge — fixture — prov-b/beta

| Run | Result | Turns | Tokens | Cost | Repair rounds | Wall |
|---|---|---|---|---|---|---|
| 1 | pass | 1 | 340 | $0.0021 | 0 | 2.05s |
| 2 | pass | 1 | 355 | $0.0022 | 0 | 3.075s |
| 3 | fail | 1 | 420 | $0.0027 | 1 | 4.4s |

### dark-mode — fixture — prov-b/beta

| Run | Result | Turns | Tokens | Cost | Repair rounds | Wall |
|---|---|---|---|---|---|---|
| 1 | fail | 1 | 480 | $0.0029 | 0 | 3.3s |
| 2 | fail | 1 | 495 | $0.0030 | 0 | 4.9s |
| 3 | fail | 1 | 510 | $0.0032 | 0 | 5.2s |

