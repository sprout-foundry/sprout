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
    python3 process/process.py --repo /path/to/seed
    python3 process/process.py --repo . --single --dry-run
    python3 process/process.py --repo . --timeout 10800
    python3 process/process.py --repo . --skip-on-timeout
"""

from __future__ import annotations

import argparse
import json
import os
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

CHECKPOINT_FILE = "process/.todo_pipeline_checkpoint.json"


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
        # Reset staleness on any non-empty output — the process is alive if
        # it's producing bytes.  Reset stale warning count only on recognised
        # progress indicators so a spinner doesn't mask a real stall.
        if data.strip():
            self.last_progress_time = time.time()
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


def _have_pty() -> bool:
    """True if this platform supports PTY operations (os.openpty + fcntl)."""
    if not hasattr(os, "openpty"):
        return False
    try:
        import fcntl  # noqa: F401
        return True
    except ImportError:
        return False


def _drain_pty(master_fd: int, monitor: ProgressMonitor) -> str:
    """Read all available data from a non-blocking PTY master fd.

    Returns the decoded text collected this call (may be empty).
    """
    chunks: list[str] = []
    while True:
        try:
            chunk = os.read(master_fd, 4096)
            if not chunk:
                break
            text = chunk.decode("utf-8", errors="replace")
            chunks.append(text)
            monitor.feed(text)
            for line in text.split("\n"):
                if line:
                    print(f"[out] {line}\n", end="", file=sys.stderr)
        except (BlockingIOError, OSError):
            break
    return "".join(chunks)


def _run_process_pty(
    cmd: list[str],
    cwd: pathlib.Path | str,
    timeout: int | None,
    stale_timeout: int,
) -> subprocess.CompletedProcess[str]:
    """PTY-backed run — gives line-buffered output from the child.

    stdout and stderr share one PTY stream, so we record the merged output
    as ``stdout`` and leave ``stderr`` empty to avoid double-counting when
    callers concatenate ``stdout + stderr``.
    """
    import fcntl  # Unix-only; guarded by _have_pty()

    monitor = ProgressMonitor(stale_timeout)
    stale_warning_interval = max(stale_timeout // 2, 60)

    master_fd, slave_fd = os.openpty()
    try:
        flags = fcntl.fcntl(master_fd, fcntl.F_GETFL)
        fcntl.fcntl(master_fd, fcntl.F_SETFL, flags | os.O_NONBLOCK)

        proc = subprocess.Popen(
            cmd, cwd=str(cwd), stdin=subprocess.DEVNULL,
            stdout=slave_fd, stderr=slave_fd,
        )
        os.close(slave_fd)
        slave_fd = -1

        start_time = time.time()
        last_stale_warning = 0.0
        output_text = ""

        try:
            while True:
                elapsed = time.time() - start_time

                output_text += _drain_pty(master_fd, monitor)

                if timeout and elapsed >= timeout:
                    _log(f"TIMEOUT after {elapsed:.0f}s – killing agent")
                    _log(f"  Progress: {monitor.status()}")
                    proc.kill()
                    try:
                        proc.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        pass
                    output_text += _drain_pty(master_fd, monitor)
                    return subprocess.CompletedProcess(
                        cmd, returncode=-1, stdout=output_text, stderr="",
                    )

                if monitor.is_stale():
                    if elapsed - last_stale_warning > stale_warning_interval:
                        monitor.stale_warnings += 1
                        _log(
                            f"STALE output (warning #{monitor.stale_warnings}): "
                            f"no progress for {stale_timeout}s. {monitor.status()}"
                        )
                        last_stale_warning = elapsed

                ret = proc.poll()
                if ret is not None:
                    output_text += _drain_pty(master_fd, monitor)
                    # PTY translates \n → \r\n (ONLCR); normalise back so
                    # downstream pattern matching and line splitting work.
                    output_text = output_text.replace("\r\n", "\n")
                    return subprocess.CompletedProcess(
                        cmd, returncode=ret, stdout=output_text, stderr="",
                    )

                time.sleep(2)

        except KeyboardInterrupt:
            _log("Interrupted – killing agent process")
            proc.kill()
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                pass
            raise
    finally:
        if master_fd >= 0:
            os.close(master_fd)
        if slave_fd >= 0:
            os.close(slave_fd)


def _run_process_pipes(
    cmd: list[str],
    cwd: pathlib.Path | str,
    timeout: int | None,
    stale_timeout: int,
) -> subprocess.CompletedProcess[str]:
    """Portable fallback using PIPE + threads (works on Windows).

    Trades line-buffered visibility for cross-platform compatibility.
    """
    monitor = ProgressMonitor(stale_timeout)
    stale_warning_interval = max(stale_timeout // 2, 60)

    proc = subprocess.Popen(
        cmd, cwd=str(cwd), stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
    )
    start_time = time.time()
    last_stale_warning = 0.0
    stdout_parts: list[str] = []
    stderr_parts: list[str] = []
    stdout_done = threading.Event()
    stderr_done = threading.Event()

    def _read(stream, parts: list[str], label: str):
        try:
            for line in stream:
                parts.append(line)
                monitor.feed(line)
                if line.strip():
                    print(f"[{label}] {line}", end="", file=sys.stderr)
        finally:
            stream.close()
            (stdout_done if label == "stdout" else stderr_done).set()

    t_out = threading.Thread(target=_read, args=(proc.stdout, stdout_parts, "stdout"), daemon=True)
    t_err = threading.Thread(target=_read, args=(proc.stderr, stderr_parts, "stderr"), daemon=True)
    t_out.start()
    t_err.start()

    try:
        while True:
            elapsed = time.time() - start_time
            if timeout and elapsed >= timeout:
                _log(f"TIMEOUT after {elapsed:.0f}s – killing agent")
                _log(f"  Progress: {monitor.status()}")
                proc.kill()
                try:
                    proc.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    pass
                stdout_done.wait(timeout=5)
                stderr_done.wait(timeout=5)
                return subprocess.CompletedProcess(
                    cmd, returncode=-1, stdout="".join(stdout_parts), stderr="".join(stderr_parts),
                )

            if monitor.is_stale():
                if elapsed - last_stale_warning > stale_warning_interval:
                    monitor.stale_warnings += 1
                    _log(
                        f"STALE output (warning #{monitor.stale_warnings}): "
                        f"no progress for {stale_timeout}s. {monitor.status()}"
                    )
                    last_stale_warning = elapsed

            ret = proc.poll()
            if ret is not None:
                stdout_done.wait(timeout=5)
                stderr_done.wait(timeout=5)
                return subprocess.CompletedProcess(
                    cmd, returncode=ret,
                    stdout="".join(stdout_parts), stderr="".join(stderr_parts),
                )

            time.sleep(2)

    except KeyboardInterrupt:
        _log("Interrupted – killing agent process")
        proc.kill()
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            pass
        raise


def _run_process(
    cmd: list[str],
    cwd: pathlib.Path | str,
    timeout: int | None = None,
    stale_timeout: int = 600,
) -> subprocess.CompletedProcess[str]:
    """Run a subprocess with progress monitoring.

    On Unix, uses a PTY so the child process line-buffers its output,
    giving real-time visibility without ``stdbuf``.  Falls back to PIPE +
    threads on Windows or when PTY allocation fails.
    """
    try:
        if _have_pty():
            return _run_process_pty(cmd, cwd, timeout, stale_timeout)
    except (OSError, ImportError):
        pass  # PTY not available — fall through to pipes

    return _run_process_pipes(cmd, cwd, timeout, stale_timeout)


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


# ---------------------------------------------------------------------------
# Post-agent validation
# ---------------------------------------------------------------------------

_PROVIDER_ERROR_PATTERNS = [
    r"provider.*error",
    r"provider.*failed",
    r"connection.*refused",
    r"connection.*reset",
    r"etimedout",
    r"etimedout",
    r"502.*bad.*gateway",
    r"503.*service.*unavailable",
    r"504.*gateway.*timeout",
    r"rate.*limit",
    r"too.*many.*requests",
    r"api.*key.*invalid",
    r"unauthorized",
    r"forbidden",
    r"no.*provider.*found",
    r"provider.*not.*configured",
    r"chat.*failed",
    r"chatstream.*failed",
    r"llm.*error",
    r"model.*not.*found",
]
_PROVIDER_ERROR_RE = re.compile("|".join(_PROVIDER_ERROR_PATTERNS), re.IGNORECASE)


def has_provider_errors(output: str) -> bool:
    """Check if agent output contains signs of provider failure."""
    return _PROVIDER_ERROR_RE.search(output) is not None


def validate_post_agent(
    opts: Opts,
    agent_result: subprocess.CompletedProcess[str],
    todo_text: str,
) -> bool:
    """Validate that the agent actually completed work successfully.

    Checks:
    1. No provider errors in output (even if exit code 0)
    2. There are staged changes (agent did actual work)
    3. Build passes (go build ./...)

    Returns True if all checks pass, False if any check fails.
    """
    # 1. Check for provider errors in output
    combined_output = agent_result.stdout + agent_result.stderr
    if has_provider_errors(combined_output):
        _log(f"PROVIDER ERROR detected in agent output for TODO: {todo_text!r}")
        _log("Skipping mark-complete due to provider failure")
        return False

    # 2. Check for staged changes (agent must have done actual work)
    if not has_staged_changes(opts.repo):
        _log(f"NO STAGED CHANGES after agent run for TODO: {todo_text!r}")
        _log("Skipping mark-complete – agent did not produce any changes")
        return False

    # 3. Verify build passes
    if not opts.dry_run:
        _log("Verifying build passes...")
        build_result = subprocess.run(
            ["go", "build", "./..."],
            cwd=str(opts.repo),
            capture_output=True,
            text=True,
            timeout=120,
        )
        if build_result.returncode != 0:
            _log(f"BUILD FAILED after agent run for TODO: {todo_text!r}")
            _log(f"  Build output: {build_result.stderr[:500]}")
            _log("Skipping mark-complete – build does not pass")
            return False
        _log("Build verification passed")
    else:
        _log("[DRY RUN] Would verify build passes (skipped)")

    return True


def run_sprout_commit(opts: Opts) -> subprocess.CompletedProcess[str]:
    """Run ``sprout commit --skip-prompt`` if there are staged changes."""
    if not has_staged_changes(opts.repo):
        _log("No staged changes – skipping commit")
        return subprocess.CompletedProcess(
            ["sprout", "commit"], returncode=0, stdout="", stderr=""
        )

    # sprout commit — _run_process handles PTY/pipe selection.
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

            # 2.5. Post-agent validation — prevent false completions from provider errors
            if not validate_post_agent(self.opts, agent_result, item.text):
                _log(f"VALIDATION FAILED for TODO: {item.text!r}")
                _log("Skipping mark-complete — agent did not produce valid results")
                self.skipped.append(item.text)
                return False

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
        default="process/process_workflow.json",
        help="Path to the workflow JSON (default: process/process_workflow.json)",
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
