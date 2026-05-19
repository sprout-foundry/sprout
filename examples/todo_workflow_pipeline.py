#!/usr/bin/env python3
"""Iterate through TODO.md items using sprout workflow configs.

For each incomplete TODO item this script:
  1. Loads a workflow JSON and templates the TODO text into the initial prompt
  2. Runs ``sprout agent --workflow-config <config> --skip-prompt --no-web-ui``
  3. Commits staged changes with ``sprout commit --skip-prompt``
  4. Marks the TODO as complete in TODO.md
  5. Advances to the next incomplete item

Timeout & Recovery:
  - Monitors agent output for signs of progress (tool calls, LLM calls, etc.)
  - If no progress for ``--stale-timeout`` seconds, logs a warning and optionally kills
  - On hard timeout, saves partial state and skips the TODO (``--skip-on-timeout``)
  - Writes a checkpoint file so re-runs resume after the last processed item

Usage:
    python3 examples/todo_workflow_pipeline.py --repo /path/to/repo
    python3 examples/todo_workflow_pipeline.py --repo . --single --dry-run
    python3 examples/todo_workflow_pipeline.py --repo . --timeout 10800
    python3 examples/todo_workflow_pipeline.py --repo . --skip-on-timeout
"""

from __future__ import annotations

import argparse
import json
import pathlib
import re
import subprocess
import sys
import tempfile
import threading
import time
from dataclasses import dataclass, field

# ---------------------------------------------------------------------------
# Data types
# ---------------------------------------------------------------------------

_TODO_RE = re.compile(r"^\[\]\s+[-—]\s+(.+)$")

PLACEHOLDER = "{TODO_TEXT}"

# Patterns that indicate the agent is still making progress
_PROGRESS_PATTERNS = [
    # Tool execution
    r"tool_call",
    r"running subagent",
    r"spawn.*subagent",
    r"executing tool",
    r"tool result",
    # LLM activity
    r"llm call",
    r"tokens_",
    r"streaming",
    r"token",
    # Agent lifecycle
    r"iteration",
    r"compacting",
    r"preparing messages",
    r"process_query",
    r"finish_reason",
    # Git/build activity
    r"staged changes",
    r"commit",
    r"git add",
    r"build",
    r"test",
    r"review",
    # Sprout-specific output markers
    r"\[OK\]",          # Completion success
    r"\[WARN\]",        # Warnings during execution
    r"\[STOP\]",        # Stop/interrupt messages
    r"\[FAIL\]",        # Errors (still activity)
    r"\[~\]",           # Pruning/compaction
    r"\[>>\]",          # Direct mode processing
    r"\[web\]",         # Web UI messages
    r"\[bot\]",         # Interactive mode
    r"\[chart\]",       # Provider/model info
    r"\[Detected",      # Command detection
    r"\[Auto-",         # Auto-execution
    r"\[!]",            # Fast path
    r"\[\|",            # Workflow yield
    r"\[debug\]",       # Debug output
    r"\[credentials\]", # Credential messages
    r"\[mcp",           # MCP messages
    r"\[coder\]|running subagent",  # Subagent persona output
]
_PROGRESS_RE = re.compile("|".join(_PROGRESS_PATTERNS), re.IGNORECASE)

CHECKPOINT_FILE = "examples/.todo_pipeline_checkpoint.json"


@dataclass
class TodoItem:
    line_idx: int
    text: str


@dataclass
class Opts:
    repo: pathlib.Path
    todo_file: pathlib.Path
    workflow_config: pathlib.Path
    sprout_bin: str
    max_todos: int
    single: bool
    dry_run: bool
    timeout: int
    skip_on_timeout: bool
    stale_timeout: int
    keep_on_timeout: bool


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _ts() -> str:
    return time.strftime("%Y-%m-%d %H:%M:%S")


def _log(msg: str) -> None:
    print(f"[{_ts()}] {msg}", file=sys.stderr)


# ---------------------------------------------------------------------------
# Checkpoint management
# ---------------------------------------------------------------------------


def load_checkpoint(checkpoint_path: pathlib.Path) -> list[str]:
    """Load the list of already-processed TODO texts from the checkpoint file."""
    if not checkpoint_path.exists():
        return []
    try:
        data = json.loads(checkpoint_path.read_text(encoding="utf-8"))
        return data.get("processed", [])
    except (json.JSONDecodeError, KeyError):
        return []


def save_checkpoint(
    checkpoint_path: pathlib.Path, processed: list[str], skipped: list[str] | None = None
) -> None:
    """Save processed (and optionally skipped) TODO texts to the checkpoint file."""
    checkpoint_path.parent.mkdir(parents=True, exist_ok=True)
    data = {
        "processed": processed,
        "skipped": skipped or [],
        "updated_at": time.strftime("%Y-%m-%d %H:%M:%S"),
    }
    checkpoint_path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")


def clear_checkpoint(checkpoint_path: pathlib.Path) -> None:
    """Remove the checkpoint file."""
    if checkpoint_path.exists():
        checkpoint_path.unlink()
        _log("Checkpoint cleared")


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
    """Replace the first matching ``[]`` or ``[x]`` TODO line — idempotent."""
    content = todo_path.read_text(encoding="utf-8")
    lines = content.splitlines()

    found = False
    already_done = False
    new_lines: list[str] = []
    for line in lines:
        if not found:
            stripped = line.strip()
            # Match both [] (still open) and [x] (agent may have already marked it)
            for marker in ("[]", "[x]", "[X]"):
                if stripped.startswith(marker):
                    rest = stripped[len(marker):].lstrip()
                    if rest.startswith("-") or rest.startswith("—"):
                        desc = rest[1:].strip()
                        prefix_len = min(len(desc), len(todo_text), 40)
                        if (
                            desc == todo_text
                            or desc[:prefix_len] == todo_text[:prefix_len]
                        ):
                            if marker in ("[x]", "[X]"):
                                already_done = True
                            else:
                                new_line = line.replace("[]", "[x]", 1)
                                new_lines.append(new_line)
                            found = True
                            continue
                    break
        new_lines.append(line)

    if already_done:
        _log(f"Already marked complete: {todo_text}")
        return

    if not found:
        raise ValueError(f"Could not find TODO item matching: {todo_text!r}")

    todo_path.write_text("\n".join(new_lines) + "\n", encoding="utf-8")
    _log(f"Marked complete: {todo_text}")


# ---------------------------------------------------------------------------
# Workflow templating
# ---------------------------------------------------------------------------


def _replace_in_obj(
    obj: object,
    placeholder: str,
    replacement: str,
) -> object:
    """Recursively replace *placeholder* in all string values of a JSON-like object."""
    if isinstance(obj, str):
        return obj.replace(placeholder, replacement)
    if isinstance(obj, dict):
        return {k: _replace_in_obj(v, placeholder, replacement) for k, v in obj.items()}
    if isinstance(obj, list):
        return [_replace_in_obj(item, placeholder, replacement) for item in obj]
    return obj


def build_templated_workflow(
    workflow_config_path: pathlib.Path,
    todo_text: str,
) -> pathlib.Path:
    """Read the workflow JSON, replace ``{TODO_TEXT}``, write a temp copy."""
    workflow = json.loads(workflow_config_path.read_text(encoding="utf-8"))
    workflow_str_before = json.dumps(workflow)

    if PLACEHOLDER not in workflow_str_before:
        raise ValueError(
            f"Workflow config does not contain {PLACEHOLDER!r} placeholder"
        )

    workflow = _replace_in_obj(workflow, PLACEHOLDER, todo_text)
    workflow_str = json.dumps(workflow)

    tmp = tempfile.NamedTemporaryFile(
        mode="w",
        suffix=".json",
        prefix="sprout_workflow_",
        delete=False,
        encoding="utf-8",
    )
    tmp.write(workflow_str)
    tmp.close()
    return pathlib.Path(tmp.name)


# ---------------------------------------------------------------------------
# Subprocess helpers
# ---------------------------------------------------------------------------


class ProgressMonitor:
    """Monitor subprocess output for signs of progress."""

    def __init__(self, stale_timeout: int = 600) -> None:
        self.stale_timeout = stale_timeout
        self.last_progress_time = time.time()
        self.stale_warnings = 0
        self.total_bytes = 0

    def feed(self, data: str) -> None:
        """Process output data and update progress tracking."""
        self.total_bytes += len(data)
        # Always reset on any output — the process is alive if it's producing bytes
        if data.strip():
            self.last_progress_time = time.time()
        # Reset stale warnings only on recognized progress indicators
        if _PROGRESS_RE.search(data):
            self.stale_warnings = 0

    def is_stale(self) -> bool:
        """Return True if no progress detected within stale_timeout."""
        if self.stale_timeout <= 0:
            return False
        return (time.time() - self.last_progress_time) > self.stale_timeout

    def status(self) -> str:
        elapsed_since_progress = time.time() - self.last_progress_time
        return (
            f"bytes={self.total_bytes}, "
            f"last_progress={elapsed_since_progress:.0f}s ago, "
            f"warnings={self.stale_warnings}"
        )


def _tee_output(
    source,  # subprocess stdout/stderr pipe
    monitor: ProgressMonitor,
    label: str = "",
) -> str:
    """Read from pipe in chunks (not line-buffered) so partial output
    during LLM streaming is detected by the progress monitor.

    Tee to stderr line-by-line for readability, but feed the monitor
    on every chunk so we catch activity even when no newlines arrive.
    """
    collected: list[str] = []
    prefix = f"[{label}] " if label else ""
    buf = ""
    while True:
        chunk = source.read(4096)
        if not chunk:
            break
        if isinstance(chunk, bytes):
            chunk = chunk.decode("utf-8", errors="replace")
        buf += chunk
        monitor.feed(chunk)
        # Tee complete lines to stderr for readability
        while "\n" in buf:
            line, buf = buf.split("\n", 1)
            line += "\n"
            collected.append(line)
            if line.strip():
                print(f"{prefix}{line}", end="", file=sys.stderr)
    # Remaining partial line (no trailing newline)
    if buf:
        collected.append(buf)
        if buf.strip():
            print(f"{prefix}{buf}", end="", file=sys.stderr)
    return "".join(collected)


def _run_process(
    cmd: list[str],
    cwd: pathlib.Path | str,
    timeout: int | None = None,
    stale_timeout: int = 600,
) -> subprocess.CompletedProcess[str]:
    """Run a command with stdout/stderr flowing directly to the console.

    Monitors output for progress indicators. Warns if output stalls.
    On timeout, kills the process gracefully and returns a CompletedProcess
    with returncode=-1 (special sentinel for timeout).
    """
    monitor = ProgressMonitor(stale_timeout)
    stale_warning_interval = max(stale_timeout // 2, 60)  # warn at most every N seconds

    proc = subprocess.Popen(
        cmd,
        cwd=str(cwd),
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )

    start_time = time.time()
    last_stale_warning = 0.0
    stdout_text = ""
    stderr_text = ""

    try:
        # Use threads to read stdout/stderr concurrently while monitoring
        stdout_done = threading.Event()
        stderr_done = threading.Event()

        def read_stdout():
            try:
                nonlocal stdout_text
                stdout_text = _tee_output(proc.stdout, monitor, "stdout")
            finally:
                proc.stdout.close()
                stdout_done.set()

        def read_stderr():
            try:
                nonlocal stderr_text
                stderr_text = _tee_output(proc.stderr, monitor, "stderr")
            finally:
                proc.stderr.close()
                stderr_done.set()

        t_out = threading.Thread(target=read_stdout, daemon=True)
        t_err = threading.Thread(target=read_stderr, daemon=True)
        t_out.start()
        t_err.start()

        while True:
            elapsed = time.time() - start_time

            # Check timeout
            if timeout and elapsed >= timeout:
                _log(f"TIMEOUT after {elapsed:.0f}s – killing agent")
                _log(f"  Progress: {monitor.status()}")
                proc.kill()
                proc.wait(timeout=10)
                stdout_done.wait(timeout=5)
                stderr_done.wait(timeout=5)
                return subprocess.CompletedProcess(
                    cmd,
                    returncode=-1,  # sentinel: timeout
                    stdout=stdout_text,
                    stderr=stderr_text,
                )

            # Check for staleness (no output progress)
            # Only count as a "warning" if the process has actually exited.
            # If the process is still running, it's likely in a long LLM call
            # (extended thinking) or synchronous tool execution — these produce
            # no stdout but are NOT stuck. Log a heartbeat instead.
            if monitor.is_stale():
                if proc.poll() is not None:
                    # Process has exited — output is genuinely stale
                    if elapsed - last_stale_warning > stale_warning_interval:
                        monitor.stale_warnings += 1
                        _log(
                            f"STALE output (warning #{monitor.stale_warnings}): "
                            f"process exited, no output for {stale_timeout}s. "
                            f"{monitor.status()}"
                        )
                        last_stale_warning = elapsed
                elif elapsed - last_stale_warning > stale_warning_interval:
                    # Process still alive but silent (e.g., LLM extended thinking)
                    _log(
                        f"[heartbeat] Process alive, no output for {stale_timeout}s+. "
                        f"{monitor.status()}"
                    )
                    last_stale_warning = elapsed

            # Check if process finished
            ret = proc.poll()
            if ret is not None:
                stdout_done.wait(timeout=5)
                stderr_done.wait(timeout=5)
                return subprocess.CompletedProcess(
                    cmd,
                    returncode=ret,
                    stdout=stdout_text,
                    stderr=stderr_text,
                )

            time.sleep(2)  # poll interval

    except KeyboardInterrupt:
        _log("Interrupted – killing agent process")
        proc.kill()
        proc.wait(timeout=10)
        stdout_done.wait(timeout=5)
        stderr_done.wait(timeout=5)
        raise


def run_sprout_agent(
    opts: Opts,
    workflow_config_path: pathlib.Path,
) -> subprocess.CompletedProcess[str]:
    """Run ``sprout agent --workflow-config <path> --skip-prompt --no-web-ui``."""
    cmd = [
        opts.sprout_bin,
        "agent",
        "--workflow-config",
        str(workflow_config_path),
        "--skip-prompt",
        "--no-web-ui",
        "--no-connection-check",
    ]
    _log(f"Running: {' '.join(cmd)}")
    if opts.dry_run:
        _log("[DRY RUN] Would run sprout agent (skipped)")
        return subprocess.CompletedProcess(cmd, returncode=0, stdout="", stderr="")

    result = _run_process(
        cmd,
        cwd=opts.repo,
        timeout=opts.timeout,
        stale_timeout=opts.stale_timeout,
    )

    if result.returncode == -1:
        _log(f"Agent TIMEOUT after {opts.timeout}s")
    elif result.returncode != 0:
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


def run_sprout_commit(opts: Opts) -> subprocess.CompletedProcess[str]:
    """Run ``sprout commit --skip-prompt`` if there are staged changes."""
    if not has_staged_changes(opts.repo):
        _log("No staged changes – skipping commit")
        return subprocess.CompletedProcess(
            ["sprout", "commit"], returncode=0, stdout="", stderr=""
        )

    cmd = [opts.sprout_bin, "commit", "--skip-prompt"]
    _log(f"Running: {' '.join(cmd)}")
    if opts.dry_run:
        _log("[DRY RUN] Would run sprout commit (skipped)")
        return subprocess.CompletedProcess(cmd, returncode=0, stdout="", stderr="")

    result = _run_process(cmd, cwd=opts.repo, timeout=min(opts.timeout, 300))

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
        self.skipped: list[str] = []

    def _checkpoint_path(self) -> pathlib.Path:
        return self.opts.repo / CHECKPOINT_FILE

    def process_one(self, item: TodoItem) -> bool:
        """Process a single TODO item. Returns True if completed, False if skipped."""
        _log(f"Processing TODO: {item.text!r}")

        # 1. Build templated workflow config
        tmp_workflow = build_templated_workflow(self.opts.workflow_config, item.text)
        try:
            # 2. Run sprout agent
            agent_result = run_sprout_agent(self.opts, tmp_workflow)

            # Handle timeout (returncode == -1 sentinel)
            if agent_result.returncode == -1:
                _log(f"Agent timed out for TODO: {item.text!r}")

                if self.opts.skip_on_timeout:
                    _log("Skipping timed-out TODO (--skip-on-timeout)")
                    self.skipped.append(item.text)
                    return False

                if self.opts.keep_on_timeout:
                    _log("Keeping staged changes from timed-out run (--keep-on-timeout)")
                    # Don't raise — fall through to commit/mark-complete
                else:
                    _log("Raising timeout error (default behavior)")
                    raise RuntimeError(
                        f"sprout agent timed out for TODO {item.text!r} "
                        f"(after {self.opts.timeout}s)"
                    )

            if agent_result.returncode != 0:
                raise RuntimeError(
                    f"sprout agent failed for TODO {item.text!r} "
                    f"(exit code {agent_result.returncode})"
                )

            # 3. Commit staged changes
            commit_result = run_sprout_commit(self.opts)
            if commit_result.returncode != 0:
                raise RuntimeError(
                    f"sprout commit failed for TODO {item.text!r} "
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
        return True

    def run(self) -> None:
        _log(f"Starting TODO workflow pipeline – repo={self.opts.repo}")
        _log(f"  TODO file:       {self.opts.todo_file}")
        _log(f"  Workflow config: {self.opts.workflow_config}")
        _log(f"  Sprout binary:   {self.opts.sprout_bin}")
        _log(f"  Max todos:       {self.opts.max_todos or 'unlimited'}")
        _log(f"  Single mode:     {self.opts.single}")
        _log(f"  Dry run:         {self.opts.dry_run}")
        _log(f"  Timeout:         {self.opts.timeout}s")
        _log(f"  Stale timeout:   {self.opts.stale_timeout}s")
        _log(f"  Skip on timeout: {self.opts.skip_on_timeout}")

        checkpoint_path = self._checkpoint_path()
        processed_texts = load_checkpoint(checkpoint_path)
        if processed_texts:
            _log(f"  Checkpoint:      {len(processed_texts)} previously processed items")

        try:
            while True:
                todos = parse_incomplete_todos(self.opts.todo_file)
                if not todos:
                    _log("No incomplete TODOs remain – done")
                    break

                # Skip already-processed items (from checkpoint)
                if processed_texts:
                    original_count = len(todos)
                    todos = [t for t in todos if t.text not in processed_texts]
                    if todos:
                        _log(
                            f"  Skipping {original_count - len(todos)} already-processed TODO(s) "
                            f"from checkpoint, {len(todos)} remaining"
                        )
                    elif original_count > 0:
                        _log(
                            f"  All {original_count} remaining TODO(s) already processed "
                            f"in checkpoint – marking checkpoint items as done in TODO.md"
                        )
                        # Re-parse and mark checkpoint items as done
                        all_todos = parse_incomplete_todos(self.opts.todo_file)
                        for t in all_todos:
                            if t.text in processed_texts:
                                try:
                                    mark_todo_complete(self.opts.todo_file, t.text)
                                except ValueError:
                                    pass
                        clear_checkpoint(checkpoint_path)
                        break

                if not todos:
                    _log("No new incomplete TODOs – done")
                    break

                item = todos[0]
                _log(
                    f"Found {len(todos)} incomplete TODO(s); next: {item.text!r}"
                )

                success = self.process_one(item)

                # Update checkpoint after successful processing
                if success:
                    processed_texts.append(item.text)
                    save_checkpoint(checkpoint_path, processed_texts, self.skipped)

                if self.opts.single:
                    _log("Single mode – stopping after one TODO")
                    break

                if self.opts.max_todos > 0 and self.processed >= self.opts.max_todos:
                    _log(
                        f"Reached max-todos limit ({self.opts.max_todos}) – stopping"
                    )
                    break

        except KeyboardInterrupt:
            _log("Interrupted by user – saving checkpoint and stopping")
            save_checkpoint(checkpoint_path, processed_texts, self.skipped)
            sys.exit(130)

        _log(
            f"Pipeline finished – processed {self.processed} TODO(s), "
            f"skipped {len(self.skipped)}"
        )
        if self.skipped:
            _log(f"Skipped TODOs: {self.skipped}")


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Iterate through TODO.md items using sprout workflow configs",
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
        "--sprout-bin",
        default="sprout",
        help="Path to sprout binary (default: sprout)",
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
    parser.add_argument(
        "--timeout",
        type=int,
        default=7200,
        help="Timeout in seconds for sprout agent subprocess (default: 7200)",
    )
    parser.add_argument(
        "--stale-timeout",
        type=int,
        default=600,
        help="Seconds of no output progress before warning (default: 600, 0 to disable)",
    )
    parser.add_argument(
        "--skip-on-timeout",
        action="store_true",
        help="Skip the TODO and continue to next on timeout instead of failing",
    )
    parser.add_argument(
        "--keep-on-timeout",
        action="store_true",
        help="Keep staged changes and mark TODO complete even after timeout",
    )
    parser.add_argument(
        "--clear-checkpoint",
        action="store_true",
        help="Clear the checkpoint file and start from the first TODO",
    )
    return parser


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()

    repo = pathlib.Path(args.repo).expanduser().resolve()
    todo_file = pathlib.Path(args.todo_file) if args.todo_file else repo / "TODO.md"
    workflow_config = pathlib.Path(args.workflow_config).expanduser().resolve()
    checkpoint_path = repo / CHECKPOINT_FILE

    # Handle --clear-checkpoint before anything else
    if args.clear_checkpoint:
        clear_checkpoint(checkpoint_path)
        _log("Done. Checkpoint cleared.")
        return

    opts = Opts(
        repo=repo,
        todo_file=todo_file,
        workflow_config=workflow_config,
        sprout_bin=args.sprout_bin,
        max_todos=args.max_todos,
        single=args.single,
        dry_run=args.dry_run,
        timeout=args.timeout,
        skip_on_timeout=args.skip_on_timeout,
        stale_timeout=args.stale_timeout,
        keep_on_timeout=args.keep_on_timeout,
    )

    pipeline = TodoPipeline(opts)
    pipeline.run()


if __name__ == "__main__":
    main()
