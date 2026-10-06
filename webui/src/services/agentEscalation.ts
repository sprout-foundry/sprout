/**
 * agentEscalation.ts — lets the agent's shell commands run in a remote
 * workspace (the user's runner or the cloud) when the in-browser shell can't.
 *
 * The WASM executor (cmd/wasm/shell_executor.go) calls
 * globalThis.__sproutEscalate.run(command) when an agent command exits 127.
 * This module decides, per the user's policy, whether to run it in one of the
 * user's workspaces — on their own runner or in the cloud (runTxnCommand:
 * push browser files, run, pull results back) — and answers with the real
 * result, or explains why it didn't run, so the model can adapt instead of
 * seeing "command not found".
 *
 * Policy (per browser, localStorage):
 *   ask    — prompt the user for each command (default)
 *   always — run without asking
 *   never  — don't escalate agent commands
 */

import { HostUnavailableError, runTxnCommand, type TxnCommandOutcome } from './cloudTxnEscalate';
import { AUTO_HOST, hostDisplayName, rememberHost, type EscalationHost } from './escalationHost';

export type EscalationPolicy = 'ask' | 'always' | 'never';
export type ConsentDecision = 'once' | 'always' | 'deny';

/** A consent answer that also names the host the user picked. */
export interface ConsentAnswer {
  decision: ConsentDecision;
  host?: EscalationHost;
}

/** Why a prompt is shown again for the same command. */
export interface ConsentContext {
  /** Shown to the user, e.g. "MacBook Pro is offline or busy." */
  notice?: string;
  /** The runner that just turned the run down; the prompt shouldn't default to it. */
  unavailableRunnerId?: string;
}

/** What the WASM executor reads back (see escalateUnavailableCommand). */
export interface EscalationResult {
  ran: boolean;
  stdout?: string;
  stderr?: string;
  exitCode?: number;
  /** Why the command didn't run; shown to the model. */
  message?: string;
}

const POLICY_KEY = 'sprout.agentEscalationPolicy';

export function getEscalationPolicy(): EscalationPolicy {
  try {
    const v = window.localStorage.getItem(POLICY_KEY);
    return v === 'always' || v === 'never' ? v : 'ask';
  } catch {
    return 'ask';
  }
}

export function setEscalationPolicy(policy: EscalationPolicy): void {
  try {
    window.localStorage.setItem(POLICY_KEY, policy);
  } catch {
    // Storage unavailable (private mode): the policy lasts for this session only.
  }
}

export interface EscalationBridgeOptions {
  /** Repository the browser workspace was imported from; required to pick a workspace. */
  repoURL?: string;
  /**
   * Ask the user whether to run this command outside the browser. Answering
   * with a bare decision runs on the "auto" host (the platform picks).
   */
  requestConsent: (command: string, context?: ConsentContext) => Promise<ConsentDecision | ConsentAnswer>;
  /** Host for runs that don't prompt (policy "always"); defaults to "auto". */
  alwaysHost?: () => EscalationHost;
  /** Whether a runner host can take work right now; checked before unprompted runs. */
  isHostAvailable?: (host: EscalationHost) => Promise<boolean>;
  /** Progress for UI (opening/pushing/running/pulling). */
  onPhase?: (command: string, phase: string, host: EscalationHost) => void;
  /** Override for tests. */
  run?: (
    repoURL: string,
    command: string,
    onPhase: (phase: string) => void,
    host: EscalationHost,
  ) => Promise<TxnCommandOutcome>;
}

function normalizeAnswer(answer: ConsentDecision | ConsentAnswer): ConsentAnswer {
  return typeof answer === 'string' ? { decision: answer } : answer;
}

function whereFor(host: EscalationHost): string {
  return host.kind === 'runner' ? `on ${hostDisplayName(host)}` : 'in the cloud workspace';
}

function toResult(outcome: TxnCommandOutcome, host: EscalationHost): EscalationResult {
  const r = outcome.result;
  let stderr = r.stderr;
  if (r.timed_out) stderr += `\n[sprout] The command timed out ${whereFor(host)}.`;
  if (r.truncated) stderr += '\n[sprout] Output was truncated.';
  if (outcome.skippedFiles > 0) {
    stderr += `\n[sprout] ${outcome.skippedFiles} changed file(s) weren't copied back to the browser workspace.`;
  }
  return { ran: true, stdout: r.stdout, stderr, exitCode: r.timed_out ? 124 : r.exit_code };
}

/** Record an approval: the host for this repo, and "always" as the policy. */
function applyApproval(repoURL: string, answer: ConsentAnswer): EscalationHost {
  const host = answer.host ?? AUTO_HOST;
  rememberHost(repoURL, host);
  if (answer.decision === 'always') setEscalationPolicy('always');
  return host;
}

/**
 * Decide on and (maybe) run one command; never throws. When a chosen runner
 * turns the run down (offline or busy), the user is asked once more with the
 * reason so they can pick the cloud or another runner instead.
 */
export async function escalateCommand(command: string, opts: EscalationBridgeOptions): Promise<EscalationResult> {
  const repoURL = opts.repoURL;
  if (!repoURL) {
    return {
      ran: false,
      message:
        'Running this needs a cloud workspace, which is tied to a repository. Open the project from a GitHub repository to enable it.',
    };
  }
  const policy = getEscalationPolicy();
  if (policy === 'never') {
    return { ran: false, message: 'The user has turned off running agent commands in the cloud workspace.' };
  }

  let host: EscalationHost;
  let prompted = false;
  if (policy === 'ask') {
    const answer = normalizeAnswer(await opts.requestConsent(command));
    if (answer.decision === 'deny') {
      return { ran: false, message: 'The user declined to run this command in the cloud workspace.' };
    }
    host = applyApproval(repoURL, answer);
    prompted = true;
  } else {
    host = opts.alwaysHost?.() ?? AUTO_HOST;
  }

  const run = opts.run ?? runTxnCommand;
  for (let attempt = 0; ; attempt += 1) {
    try {
      if (!prompted && host.kind === 'runner' && opts.isHostAvailable && !(await opts.isHostAvailable(host))) {
        throw new HostUnavailableError(host);
      }
      const runHost = host;
      const outcome = await run(repoURL, command, (phase) => opts.onPhase?.(command, phase, runHost), runHost);
      return toResult(outcome, host);
    } catch (err) {
      if (err instanceof HostUnavailableError && attempt === 0) {
        const answer = normalizeAnswer(
          await opts.requestConsent(command, { notice: err.message, unavailableRunnerId: err.runnerId }),
        );
        if (answer.decision === 'deny') {
          return { ran: false, message: `${err.message} The user chose not to run the command elsewhere.` };
        }
        host = applyApproval(repoURL, answer);
        prompted = true;
        continue;
      }
      return {
        ran: false,
        message: `Running this ${whereFor(host)} failed: ${err instanceof Error ? err.message : String(err)}`,
      };
    }
  }
}

type EscalateGlobal = { __sproutEscalate?: { run: (command: string) => Promise<EscalationResult> } };

/** Install globalThis.__sproutEscalate; returns the uninstaller. */
export function installEscalationBridge(opts: EscalationBridgeOptions): () => void {
  const g = globalThis as EscalateGlobal;
  const bridge = { run: (command: string) => escalateCommand(command, opts) };
  g.__sproutEscalate = bridge;
  return () => {
    if (g.__sproutEscalate === bridge) delete g.__sproutEscalate;
  };
}
