/**
 * cloudTasks.ts — typed client for the Foundry platform task API (Mode A).
 *
 * When the browser IDE (Mode C) hits a hard browser limit — git push, VFS
 * quota, long-running command — the work can be handed off to the platform as
 * a cloud task instead of forcing the user into a full workspace (Mode B).
 * The platform surface is intentionally small:
 *
 *   POST /api/tasks       → 201 `{ task_id, status }` + `X-Remaining-Task-Credits`
 *   GET  /api/tasks/{id}  → 200 `{ task_id, status, ... }`
 *
 * All calls use RELATIVE paths so the CloudAdapter intercepts them in cloud
 * mode and proxies to the Foundry backend with session credentials (see
 * services/cloudAdapter.ts). In local mode they resolve against the local
 * server unchanged.
 *
 * This module is side-effect free and framework agnostic (no React imports)
 * so it can be unit tested and reused outside components.
 */

/** Response header carrying the user's remaining task credits after a submit. */
export const REMAINING_TASK_CREDITS_HEADER = 'X-Remaining-Task-Credits';

/**
 * Statuses a task never leaves once entered. Everything else ('pending',
 * 'running', ...) is still in flight and worth polling.
 */
export const CLOUD_TASK_TERMINAL_STATUSES = ['completed', 'failed', 'cancelled', 'timeout'] as const;

/** A platform task. `task_id` and `status` are the only guaranteed fields. */
export interface CloudTask {
  task_id: string;
  status: string;
  /** The platform may attach extra fields (logs_url, branch, model, ...). */
  [key: string]: unknown;
}

/** Body for POST /api/tasks. */
export interface SubmitCloudTaskInput {
  repo_url: string;
  prompt: string;
  model?: string;
  provider?: string;
  branch?: string;
}

/** Result of a successful submit: the created task plus the credit balance. */
export interface SubmitCloudTaskResult {
  task: CloudTask;
  remainingTaskCredits: number | null;
}

/** Options for pollCloudTask. */
export interface PollCloudTaskOptions {
  /** Delay between status checks. Default 2000ms. */
  intervalMs?: number;
  /** Give up after this long without a terminal status. Default 600000ms. */
  timeoutMs?: number;
  /** Called with every polled task, including non-terminal ones. */
  onTick?: (task: CloudTask) => void;
}

/**
 * Convert a non-2xx response into an Error. The platform reports failures as
 * `{ error: string }` (e.g. 402 credit exhaustion); gateways sometimes reply
 * with HTML or an empty body, so the status-aware fallback always applies.
 */
async function toRequestError(res: Response, action: string): Promise<Error> {
  const fallback = `${action} failed (HTTP ${res.status})`;
  try {
    const json = (await res.json()) as { error?: unknown } | null;
    const detail = json && typeof json.error === 'string' ? json.error.trim() : '';
    return new Error(detail || fallback);
  } catch {
    return new Error(fallback);
  }
}

/** Parse the remaining-credits header; null when absent or non-numeric. */
function parseRemainingCredits(res: Response): number | null {
  const raw = res.headers.get(REMAINING_TASK_CREDITS_HEADER);
  if (raw === null) return null;
  const value = Number(raw);
  return Number.isFinite(value) ? value : null;
}

/** Validate that a decoded task body actually carries a usable task_id. */
function requireTaskId(task: CloudTask, action: string): CloudTask {
  if (typeof task?.task_id !== 'string' || task.task_id === '') {
    throw new TypeError(`${action} response is missing task_id`);
  }
  return task;
}

/**
 * Submit a Mode A cloud task against a repository.
 *
 * Resolves with the created task and the user's remaining task credits
 * (parsed from the `X-Remaining-Task-Credits` response header, null when the
 * platform omits it). Rejects with the platform's `{ error }` message, or a
 * fallback that names the HTTP status (e.g. credit exhaustion → 402).
 */
export async function submitCloudTask(input: SubmitCloudTaskInput): Promise<SubmitCloudTaskResult> {
  const res = await fetch('/api/tasks', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify(input),
  });

  if (!res.ok) {
    throw await toRequestError(res, 'Cloud task submit');
  }

  const task = requireTaskId((await res.json()) as CloudTask, 'Cloud task submit');
  return { task, remainingTaskCredits: parseRemainingCredits(res) };
}

/**
 * Fetch a single cloud task by id. The id is URI-encoded so platform ids with
 * reserved characters survive the round trip.
 */
export async function getCloudTask(taskId: string): Promise<CloudTask> {
  if (typeof taskId !== 'string' || taskId === '') {
    throw new TypeError('taskId is required');
  }

  const res = await fetch(`/api/tasks/${encodeURIComponent(taskId)}`, {
    method: 'GET',
    credentials: 'include',
  });

  if (!res.ok) {
    throw await toRequestError(res, 'Cloud task status');
  }

  return requireTaskId((await res.json()) as CloudTask, 'Cloud task status');
}

/**
 * Whether a task status is terminal ('completed', 'failed', 'cancelled',
 * 'timeout') — i.e. polling it further cannot produce a different answer.
 */
export function isTerminalCloudTaskStatus(status: string): boolean {
  return (CLOUD_TASK_TERMINAL_STATUSES as readonly string[]).includes(status);
}

/**
 * Poll a cloud task until it reaches a terminal status.
 *
 * The first check happens after `intervalMs` (callers usually already hold the
 * status from the submit response, so an immediate duplicate fetch is wasted).
 * Resolves with the terminal task; `opts.onTick` fires for every poll so
 * callers can render live status. Rejects when `timeoutMs` elapses without a
 * terminal status (message starts with "Cloud task timed out") or when a
 * status request itself fails.
 *
 * setTimeout-based with a clearTimeout in the finally, so a settled poll never
 * leaves a dangling timer and small intervals are safe in unit tests. `opts`
 * may be omitted entirely; `onTick` is optional.
 */
export async function pollCloudTask(taskId: string, opts: PollCloudTaskOptions = {}): Promise<CloudTask> {
  const intervalMs = Math.max(0, opts.intervalMs ?? 2000);
  const timeoutMs = opts.timeoutMs ?? 600000;
  const deadline = Date.now() + timeoutMs;

  let pendingTimer: ReturnType<typeof setTimeout> | null = null;
  const wait = (ms: number): Promise<void> =>
    new Promise<void>((resolve) => {
      pendingTimer = setTimeout(() => {
        pendingTimer = null;
        resolve();
      }, ms);
    });

  try {
    for (;;) {
      await wait(intervalMs);
      const task = await getCloudTask(taskId);
      try {
        opts.onTick?.(task);
      } catch {
        // A throwing status listener must not abort the poll loop.
      }
      if (isTerminalCloudTaskStatus(task.status)) {
        return task;
      }
      if (Date.now() >= deadline) {
        throw new Error(`Cloud task timed out after ${timeoutMs}ms without reaching a terminal status`);
      }
    }
  } finally {
    if (pendingTimer !== null) clearTimeout(pendingTimer);
  }
}
