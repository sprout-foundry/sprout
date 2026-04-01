#!/usr/bin/env python3
"""Iterate through TODO.md items using ledit workflow configs.

For each incomplete TODO item this script:
  1. Loads a workflow JSON and templates the TODO text into the initial prompt
  2. Runs ``ledit agent --workflow-config <config> --skip-prompt --no-web-ui``
  3. Commits staged changes with ``ledit commit --skip-prompt``
  4. Marks the TODO as complete in TODO.md
  5. Advances to the next incomplete item

Usage:
    python3 examples/todo_workflow_pipeline.py --repo /path/to/repo
    python3 examples/todo_workflow_pipeline.py --repo . --single --dry-run
"""

from __future__ import annotations

import argparse
import io
import json
import pathlib
import re
import subprocess
import sys
import tempfile
import threading
import time
from dataclasses import dataclass

# ---------------------------------------------------------------------------
# Data types
# ---------------------------------------------------------------------------

_TODO_RE = re.compile(r"^\[\]\s+[-—]\s+(.+)$")

PLACEHOLDER = "{TODO_TEXT}"


@dataclass
class TodoItem:
    line_idx: int
    text: str


@dataclass
class Opts:
    repo: pathlib.Path
    todo_file: pathlib.Path
    workflow_config: pathlib.Path
    ledit_bin: str
    max_todos: int
    single: bool
    dry_run: bool


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _ts() -> str:
    return time.strftime("%Y-%m-%d %H:%M:%S")


def _log(msg: str) -> None:
    print(f"[{_ts()}] {msg}", file=sys.stderr)


# ---------------------------------------------------------------------------
# TODO.md parsing
# ---------------------------------------------------------------------------


def parse_incomplete_todos(todo_path: pathlib.Path) -> list[TodoItem]:
    """Return all incomplete ``[]`` items from *todo_path*."""
    items: list[TodoItem] = []
    if not todo_path.exists():
        raise FileNotFoundError(f"TODO file not found: {todo_path}")
    lines = todo_path.read_text(encoding="utf-8").splitlines()
    for idx, line in enumerate(lines):
        line = line.strip()
        m = _TODO_RE.match(line)
        if m:
            items.append(TodoItem(line_idx=idx, text=m.group(1).strip()))
    return items


def mark_todo_complete(todo_path: pathlib.Path, todo_text: str) -> None:
    """Replace the first ``[] ... <todo_text>`` line with ``[x] ...``."""
    content = todo_path.read_text(encoding="utf-8")
    lines = content.splitlines()

    found = False
    new_lines: list[str] = []
    for line in lines:
        if not found:
            stripped = line.strip()
            if stripped.startswith("[]"):
                rest = stripped[2:].lstrip()
                if rest.startswith("-") or rest.startswith("—"):
                    desc = rest[1:].strip()
                    if desc == todo_text:
                        new_line = line.replace("[]", "[x]", 1)
                        new_lines.append(new_line)
                        found = True
                        continue
        new_lines.append(line)

    if not found:
        raise ValueError(f"Could not find TODO item matching: {todo_text!r}")

    todo_path.write_text("\n".join(new_lines) + "\n", encoding="utf-8")
    _log(f"Marked complete: {todo_text}")


# ---------------------------------------------------------------------------
# Workflow templating
# ---------------------------------------------------------------------------


def build_templated_workflow(
    workflow_config_path: pathlib.Path,
    todo_text: str,
) -> pathlib.Path:
    """Read the workflow JSON, replace ``{TODO_TEXT}``, write a temp copy."""
    workflow = json.loads(workflow_config_path.read_text(encoding="utf-8"))
    workflow_str = json.dumps(workflow)

    if PLACEHOLDER not in workflow_str:
        raise ValueError(
            f"Workflow config does not contain {PLACEHOLDER!r} placeholder"
        )

    workflow_str = workflow_str.replace(PLACEHOLDER, todo_text)
    tmp = tempfile.NamedTemporaryFile(
        mode="w",
        suffix=".json",
        prefix="ledit_workflow_",
        delete=False,
        encoding="utf-8",
    )
    tmp.write(workflow_str)
    tmp.close()
    return pathlib.Path(tmp.name)


# ---------------------------------------------------------------------------
# Subprocess helpers
# ---------------------------------------------------------------------------


def _truncate_lines(text: str, tail: int) -> str:
    lines = text.strip().splitlines()
    if len(lines) <= tail:
        return text.strip()
    return "... (" + str(len(lines)) + " lines total)\n" + "\n".join(lines[-tail:])


def _stream_process(
    cmd: list[str],
    cwd: pathlib.Path | str,
    timeout: int | None = None,
) -> subprocess.CompletedProcess[str]:
    """Run a command and stream stdout/stderr to the console in real time."""
    stdout_lines: list[str] = []
    stderr_lines: list[str] = []

    proc = subprocess.Popen(
        cmd,
        cwd=str(cwd),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        bufsize=1,  # line-buffered
    )

    def _drain(stream: io.TextIOWrapper, buf: list[str]) -> None:
        assert stream is not None
        for line in stream:
            buf.append(line.rstrip("\n\r"))
            print(line, end="", flush=True)

    stdout_stream = proc.stdout
    stderr_stream = proc.stderr
    if stdout_stream is None:
        stdout_stream = io.StringIO()
    if stderr_stream is None:
        stderr_stream = io.StringIO()

    t_out = threading.Thread(target=_drain, args=(stdout_stream, stdout_lines), daemon=True)
    t_err = threading.Thread(target=_drain, args=(stderr_stream, stderr_lines), daemon=True)
    t_out.start()
    t_err.start()

    try:
        proc.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()
        raise

    t_out.join(timeout=10)
    t_err.join(timeout=10)

    stdout_text = "\n".join(stdout_lines)
    stderr_text = "\n".join(stderr_lines)
    return subprocess.CompletedProcess(
        cmd,
        returncode=proc.returncode,
        stdout=stdout_text,
        stderr=stderr_text,
    )


def run_ledit_agent(
    opts: Opts,
    workflow_config_path: pathlib.Path,
) -> subprocess.CompletedProcess[str]:
    """Run ``ledit agent --workflow-config <path> --skip-prompt --no-web-ui``."""
    cmd = [
        opts.ledit_bin,
        "agent",
        "--workflow-config",
        str(workflow_config_path),
        "--skip-prompt",
        "--no-web-ui",
    ]
    _log(f"Running: {' '.join(cmd)}")
    if opts.dry_run:
        _log("[DRY RUN] Would run ledit agent (skipped)")
        return subprocess.CompletedProcess(cmd, returncode=0, stdout="", stderr="")

    result = _stream_process(cmd, cwd=opts.repo, timeout=3600)

    if result.returncode != 0:
        _log(f"Agent exited with code {result.returncode}")

    return result


def has_staged_changes(repo: pathlib.Path) -> bool:
    """Return True if there are staged changes in *repo*."""
    result = subprocess.run(
        ["git", "diff", "--cached", "--quiet", "--exit-code"],
        cwd=str(repo),
        capture_output=True,
    )
    # exit code 0 = no staged changes; 1 = staged changes
    return result.returncode != 0


def run_ledit_commit(opts: Opts) -> subprocess.CompletedProcess[str]:
    """Run ``ledit commit --skip-prompt`` if there are staged changes."""
    if not has_staged_changes(opts.repo):
        _log("No staged changes – skipping commit")
        return subprocess.CompletedProcess(
            ["ledit", "commit"], returncode=0, stdout="", stderr=""
        )

    cmd = [opts.ledit_bin, "commit", "--skip-prompt"]
    _log(f"Running: {' '.join(cmd)}")
    if opts.dry_run:
        _log("[DRY RUN] Would run ledit commit (skipped)")
        return subprocess.CompletedProcess(cmd, returncode=0, stdout="", stderr="")

    result = _stream_process(cmd, cwd=opts.repo, timeout=300)

    if result.returncode != 0:
        _log(f"Commit exited with code {result.returncode}")

    return result


# ---------------------------------------------------------------------------
# Pipeline
# ---------------------------------------------------------------------------


class TodoPipeline:
    def __init__(self, opts: Opts) -> None:
        self.opts = opts
        self.processed = 0

    def process_one(self, item: TodoItem) -> None:
        _log(f"Processing TODO: {item.text!r}")

        # 1. Build templated workflow config
        tmp_workflow = build_templated_workflow(self.opts.workflow_config, item.text)
        try:
            # 2. Run ledit agent
            agent_result = run_ledit_agent(self.opts, tmp_workflow)
            if agent_result.returncode != 0:
                raise RuntimeError(
                    f"ledit agent failed for TODO {item.text!r} "
                    f"(exit code {agent_result.returncode})"
                )

            # 3. Commit staged changes
            commit_result = run_ledit_commit(self.opts)
            if commit_result.returncode != 0:
                raise RuntimeError(
                    f"ledit commit failed for TODO {item.text!r} "
                    f"(exit code {commit_result.returncode})"
                )

            # 4. Mark TODO as complete
            if not self.opts.dry_run:
                mark_todo_complete(self.opts.todo_file, item.text)
            else:
                _log(f"[DRY RUN] Would mark complete: {item.text!r}")

        finally:
            # Clean up temp workflow file
            if tmp_workflow.exists():
                tmp_workflow.unlink()

        self.processed += 1

    def run(self) -> None:
        _log(f"Starting TODO workflow pipeline – repo={self.opts.repo}")
        _log(f"  TODO file:       {self.opts.todo_file}")
        _log(f"  Workflow config: {self.opts.workflow_config}")
        _log(f"  Ledit binary:    {self.opts.ledit_bin}")
        _log(f"  Max todos:       {self.opts.max_todos or 'unlimited'}")
        _log(f"  Single mode:     {self.opts.single}")
        _log(f"  Dry run:         {self.opts.dry_run}")

        try:
            while True:
                todos = parse_incomplete_todos(self.opts.todo_file)
                if not todos:
                    _log("No incomplete TODOs remain – done")
                    break

                item = todos[0]
                _log(
                    f"Found {len(todos)} incomplete TODO(s); next: {item.text!r}"
                )

                self.process_one(item)

                if self.opts.single:
                    _log("Single mode – stopping after one TODO")
                    break

                if self.opts.max_todos > 0 and self.processed >= self.opts.max_todos:
                    _log(
                        f"Reached max-todos limit ({self.opts.max_todos}) – stopping"
                    )
                    break

        except KeyboardInterrupt:
            _log("Interrupted by user – stopping cleanly")
            sys.exit(130)

        _log(
            f"Pipeline finished – processed {self.processed} TODO(s)"
        )


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Iterate through TODO.md items using ledit workflow configs",
    )
    parser.add_argument(
        "--repo",
        default=".",
        help="Path to the git repository (default: .)",
    )
    parser.add_argument(
        "--todo-file",
        default=None,
        help="Path to TODO.md (default: <repo>/TODO.md)",
    )
    parser.add_argument(
        "--workflow-config",
        default="examples/todo_workflow.json",
        help="Path to the workflow JSON (default: examples/todo_workflow.json)",
    )
    parser.add_argument(
        "--ledit-bin",
        default="ledit",
        help="Path to ledit binary (default: ledit)",
    )
    parser.add_argument(
        "--max-todos",
        type=int,
        default=0,
        help="Max TODOs to process, 0 = unlimited (default: 0)",
    )
    parser.add_argument(
        "--single",
        action="store_true",
        help="Process one TODO then stop",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print what would happen without executing",
    )
    return parser


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()

    repo = pathlib.Path(args.repo).expanduser().resolve()
    todo_file = pathlib.Path(args.todo_file) if args.todo_file else repo / "TODO.md"
    workflow_config = pathlib.Path(args.workflow_config).expanduser().resolve()

    opts = Opts(
        repo=repo,
        todo_file=todo_file,
        workflow_config=workflow_config,
        ledit_bin=args.ledit_bin,
        max_todos=args.max_todos,
        single=args.single,
        dry_run=args.dry_run,
    )

    pipeline = TodoPipeline(opts)
    pipeline.run()


if __name__ == "__main__":
    main()
