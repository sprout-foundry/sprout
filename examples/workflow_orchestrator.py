#!/usr/bin/env python3
"""External orchestrator for ledit workflow concurrency.

This script coordinates many `ledit agent --workflow-config ...` runs with
provider-specific concurrency limits. It relies on workflow orchestration mode:

{
  "orchestration": {
    "enabled": true,
    "resume": true,
    "yield_on_provider_handoff": true,
    "state_file": ".ledit/workflow_state.json",
    "events_file": ".ledit/workflow_events.jsonl"
  }
}

Usage:
  python3 examples/workflow_orchestrator.py --manifest examples/workflow_orchestrator_manifest.json
"""

from __future__ import annotations

import argparse
import asyncio
import json
import pathlib
from dataclasses import dataclass
from typing import Any

DEFAULT_STATE_FILE = ".ledit/workflow_state.json"
DEFAULT_EVENTS_FILE = ".ledit/workflow_events.jsonl"


@dataclass
class Job:
    name: str
    cwd: pathlib.Path
    workflow_path: pathlib.Path
    extra_args: list[str]


def _read_json(path: pathlib.Path) -> dict[str, Any]:
    with path.open("r", encoding="utf-8") as f:
        return json.load(f)


def _read_state(path: pathlib.Path) -> dict[str, Any]:
    if not path.exists():
        return {
            "initial_completed": False,
            "next_step_index": 0,
            "has_error": False,
            "complete": False,
            "last_provider": "",
            "first_error": "",
        }
    with path.open("r", encoding="utf-8") as f:
        return json.load(f)


def _orchestration_paths(workflow_cfg: dict[str, Any], cwd: pathlib.Path) -> tuple[pathlib.Path, pathlib.Path]:
    orch = workflow_cfg.get("orchestration") or {}
    state_file = orch.get("state_file") or DEFAULT_STATE_FILE
    events_file = orch.get("events_file") or DEFAULT_EVENTS_FILE
    return cwd / state_file, cwd / events_file


def _predict_next_provider(workflow_cfg: dict[str, Any], state: dict[str, Any]) -> str | None:
    if state.get("complete"):
        return None

    initial_cfg = workflow_cfg.get("initial") or {}
    steps = workflow_cfg.get("steps") or []

    if not state.get("initial_completed"):
        return (initial_cfg.get("provider") or "default").strip() or "default"

    next_idx = int(state.get("next_step_index", 0))
    if next_idx >= len(steps):
        return None

    step = steps[next_idx] or {}
    provider = (step.get("provider") or state.get("last_provider") or initial_cfg.get("provider") or "default").strip()
    return provider or "default"


async def _run_ledit_once(job: Job) -> int:
    cmd = ["ledit", "agent", "--workflow-config", str(job.workflow_path), *job.extra_args]
    proc = await asyncio.create_subprocess_exec(
        *cmd,
        cwd=str(job.cwd),
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
    )
    stdout, stderr = await proc.communicate()

    if stdout:
        print(f"[{job.name}] stdout:\n{stdout.decode(errors='replace')}")
    if stderr:
        print(f"[{job.name}] stderr:\n{stderr.decode(errors='replace')}")

    return proc.returncode


async def _run_job(job: Job, limits: dict[str, asyncio.Semaphore]) -> None:
    workflow_cfg = _read_json(job.workflow_path)
    orch = workflow_cfg.get("orchestration") or {}
    if not orch.get("enabled", False):
        raise RuntimeError(f"Job {job.name}: workflow orchestration.enabled must be true")

    state_path, events_path = _orchestration_paths(workflow_cfg, job.cwd)

    while True:
        state_before = _read_state(state_path)
        if state_before.get("complete"):
            print(f"[{job.name}] complete")
            return

        provider = _predict_next_provider(workflow_cfg, state_before) or "default"
        if provider not in limits:
            provider = "default"

        print(
            f"[{job.name}] scheduling next segment on provider={provider} "
            f"(step={state_before.get('next_step_index', 0)}, initial_completed={state_before.get('initial_completed', False)})"
        )

        async with limits[provider]:
            rc = await _run_ledit_once(job)

        state_after = _read_state(state_path)
        if state_after.get("complete"):
            if rc != 0 and state_after.get("first_error"):
                raise RuntimeError(f"Job {job.name} finished with error: {state_after['first_error']}")
            print(f"[{job.name}] complete")
            return

        progressed = (
            state_after.get("initial_completed") != state_before.get("initial_completed")
            or int(state_after.get("next_step_index", 0)) != int(state_before.get("next_step_index", 0))
            or (state_after.get("last_provider") or "") != (state_before.get("last_provider") or "")
        )

        if rc != 0 and not progressed:
            debug_hint = f"events_file={events_path}"
            raise RuntimeError(f"Job {job.name} failed without progress (returncode={rc}, {debug_hint})")


async def orchestrate(manifest_path: pathlib.Path) -> None:
    manifest = _read_json(manifest_path)
    provider_limits = manifest.get("provider_limits") or {}
    default_limit = int(provider_limits.get("default", 1))

    limits: dict[str, asyncio.Semaphore] = {
        "default": asyncio.Semaphore(default_limit),
    }
    for provider, limit in provider_limits.items():
        limits[provider] = asyncio.Semaphore(int(limit))

    jobs: list[Job] = []
    for item in manifest.get("jobs", []):
        cwd = pathlib.Path(item.get("cwd", ".")).expanduser().resolve()
        workflow = pathlib.Path(item["workflow"])
        if not workflow.is_absolute():
            workflow = (cwd / workflow).resolve()

        jobs.append(
            Job(
                name=item.get("name") or workflow.stem,
                cwd=cwd,
                workflow_path=workflow,
                extra_args=list(item.get("extra_args") or []),
            )
        )

    if not jobs:
        raise RuntimeError("manifest has no jobs")

    await asyncio.gather(*(_run_job(job, limits) for job in jobs))


def main() -> None:
    parser = argparse.ArgumentParser(description="Orchestrate concurrent ledit workflows with provider limits")
    parser.add_argument("--manifest", required=True, help="Path to orchestration manifest JSON")
    args = parser.parse_args()

    manifest_path = pathlib.Path(args.manifest).expanduser().resolve()
    if not manifest_path.exists():
        raise SystemExit(f"Manifest not found: {manifest_path}")

    try:
        asyncio.run(orchestrate(manifest_path))
    except KeyboardInterrupt:
        print("Interrupted")
        raise SystemExit(130)


if __name__ == "__main__":
    main()
