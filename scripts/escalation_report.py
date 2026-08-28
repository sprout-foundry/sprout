#!/usr/bin/env python3
"""escalation_report.py — replays the agent-session WASM-shell command log
against the wasmshell allowlist (before vs. after the read-only expansion)
to project the per-session txn escalation rate.

Inputs:
  --corpus <files...>  JSONL session logs (messages[].tool_calls[].function
                       name shell_command → arguments.command)
  --out <path>         report destination (markdown)

The "before" allowlist is the wasmshell builtin set as of ETH-2 (~34 pure
file/text builtins; gittool: handled separately, not part of shell syntax).
The "after" allowlist adds: read-only git subcommands, && / || / ; chains,
/dev/null + 2>&1 + fused redirects, grep/find/ls/wc/cat/head/tail flag
coverage. A command "runs in-browser" when every segment of the full
chain/pipeline is answerable in-browser; otherwise it 127s into a txn.
"""

import argparse
import json
import re
from collections import Counter

BUILTIN_BEFORE = {
    "ls", "cd", "pwd", "cat", "mkdir", "rm", "rmdir", "cp", "mv", "touch",
    "echo", "head", "tail", "wc", "grep", "sort", "find", "tree", "clear",
    "help", "date", "whoami", "env", "export", "which", "type", "history",
    "println", "basename", "dirname", "realpath", "tr", "uniq", "cut", "tee",
}

READONLY_GIT = {
    "status", "diff", "log", "show", "branch", "remote", "ls-files",
    "rev-list", "rev-parse", "blame", "describe", "shortlog", "tag",
    "symbolic-ref", "config", "cat-file",
}

# grep flags modeled after the expansion
GREP_OK = re.compile(r"^-[aiEvncors]+$|--include(=|$)|^-[ABC]\d+$")
# find predicates modeled after the expansion
FIND_OK = {"-name", "-path", "-type", "-maxdepth", "-not", "-o", "-or", "!", "-a", "-and"}


def segments(cmd: str):
    parts = re.split(r";|&&|\|\||\|", cmd)
    return [p.strip() for p in parts if p.strip()]


def seg_head(seg: str):
    m = re.match(r"([A-Za-z_][A-Za-z0-9_.-]*)", seg)
    return m.group(1) if m else ""


def git_sub(seg: str):
    m = re.match(r"git\s+(?:-[A-Za-z-]+\s+\S+\s+)?([a-z-]+)", seg)
    return m.group(1) if m else None


def runnable_before(seg: str) -> bool:
    name = seg_head(seg)
    if name not in BUILTIN_BEFORE:
        return False
    return flags_supported_before(seg, name)


def flags_supported_before(seg: str, name: str) -> bool:
    toks = seg.split()
    flags = [t for t in toks[1:] if t.startswith("-")]
    if name == "grep":
        return all(t in {"-i", "-v", "-n", "-c", "-e"} for t in flags)
    if name == "find":
        rest = toks[1:]
        if rest and not rest[0].startswith("-"):
            rest = rest[1:]
        return all(t in {"-name", "-type"} or t.startswith("-name") or t.startswith("-type") for t in rest)
    return True


def runnable_after(seg: str) -> bool:
    name = seg_head(seg)
    if name == "git":
        sub = git_sub(seg)
        return sub in READONLY_GIT
    if name not in BUILTIN_BEFORE:
        return False
    toks = seg.split()
    flags = [t for t in toks[1:] if t.startswith("-")]
    if name == "find":
        rest = toks[1:]
        if rest and not rest[0].startswith("-"):
            rest = rest[1:]
        return all(t in FIND_OK or not t.startswith("-") for t in rest)
    if name == "grep":
        return all(GREP_OK.match(t) for t in flags)
    return True


def load_corpus(paths):
    cmds = []
    for path in paths:
        with open(path, encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    conv = json.loads(line)
                except json.JSONDecodeError:
                    continue
                for msg in conv.get("messages", []):
                    for tc in msg.get("tool_calls") or []:
                        fn = tc.get("function", {})
                        if fn.get("name") in ("shell_command", "shell"):
                            raw = fn.get("arguments")
                            if isinstance(raw, dict):
                                args = raw
                            else:
                                try:
                                    args = json.loads(raw or "{}")
                                except (json.JSONDecodeError, TypeError):
                                    continue
                            c = args.get("command")
                            if isinstance(c, str) and c.strip():
                                cmds.append(c)
    return cmds


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--corpus", nargs="+", required=True)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    cmds = load_corpus(args.corpus)
    sessions = len(args.corpus)

    before_ok = sum(1 for c in cmds if all(runnable_before(s) for s in segments(c)))
    after_ok = sum(1 for c in cmds if all(runnable_after(s) for s in segments(c)))
    n = len(cmds)

    blocked_after = [c for c in cmds if not all(runnable_after(s) for s in segments(c))]
    blockers = Counter(seg_head(s) for c in blocked_after for s in segments(c) if not runnable_after(s))

    lines = []
    a = lines.append
    a("# WASM shell escalation-rate projection")
    a("")
    a(f"- Corpus: {sessions} session file(s), {n} shell_command invocations")
    a("- Before: ~34 pure file/text builtins (ETH-2 baseline); git absent → 127 → txn")
    a("- After: read-only git subcommands + &&/||/; chains + /dev/null/2>&1 +")
    a("  grep/find/ls/wc/cat/head/tail flag coverage (this change)")
    a("")
    a("## Escalation rate (txns per command invocation)")
    a("")
    a("| metric | before | after | delta |")
    a("|---|---|---|---|")
    b_rate = (n - before_ok) / n if n else 0
    a_rate = (n - after_ok) / n if n else 0
    a(f"| in-browser runnable | {before_ok}/{n} ({before_ok/n:.1%}) | {after_ok}/{n} ({after_ok/n:.1%}) | +{after_ok-before_ok} cmds |" if n else "| n/a |")
    a(f"| escalations (127 → txn) | {n-before_ok} | {n-after_ok} | −{(n-before_ok)-(n-after_ok)} |")
    a(f"| escalation rate | {b_rate:.1%} | {a_rate:.1%} | −{(b_rate-a_rate)*100:.1f} pts |")
    a("")
    a("## Remaining 127 drivers (top segments)")
    a("")
    a("| command | blocked segments |")
    a("|---|---|")
    for name, count in blockers.most_common(15):
        a(f"| {name} | {count} |")
    a("")
    a("## Method")
    a("")
    a("Each logged `shell_command` invocation is split into chain/pipeline")
    a("segments; the invocation runs in-browser only when every segment is")
    a("answerable by the wasmshell allowlist (a single unknown segment 127s")
    a("the whole invocation into a container txn). Flag support is checked")
    a("per command against the before/after flag matrices above.")

    with open(args.out, "w", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")

    print(f"wrote {args.out}: {before_ok}/{n} → {after_ok}/{n} in-browser "
          f"({b_rate:.1%} → {a_rate:.1%} escalation)")


if __name__ == "__main__":
    main()
