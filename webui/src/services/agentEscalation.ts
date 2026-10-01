/**
 * agentEscalation.ts — lets the agent's shell commands run in the cloud
 * workspace when the in-browser shell can't run them.
 *
 * The WASM executor (cmd/wasm/shell_executor.go) calls
 * globalThis.__sproutEscalate.run(command) when an agent command exits 127.
 * This module decides, per the user's policy, whether to run it in the
 * user's cloud workspace container (runTxnCommand: push browser files, run,
 * pull results back) and answers with the real result — or explains why it
 * didn't run, so the model can adapt instead of seeing "command not found".
 *
 * Policy (per browser, localStorage):
 *   ask    — prompt the user for each command (default)
 *   always — run without asking
 *   never  — don't escalate agent commands
 */

import { runTxnCommand } from './cloudTxnEscalate';

export type EscalationPolicy = 'ask' | 'always' | 'never';
export type ConsentDecision = 'once' | 'always' | 'deny';

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
  /** Ask the user whether to run this command in the cloud workspace. */
  requestConsent: (command: string) => Promise<ConsentDecision>;
  /** Progress for UI (opening/pushing/running/pulling). */
  onPhase?: (command: string, phase: string) => void;
  /** Override for tests. */
  run?: typeof runTxnCommand;
}

/** Decide on and (maybe) run one command; never throws. */
export async function escalateCommand(command: string, opts: EscalationBridgeOptions): Promise<EscalationResult> {
  if (!opts.repoURL) {
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
  if (policy === 'ask') {
    const decision = await opts.requestConsent(command);
    if (decision === 'deny') {
      return { ran: false, message: 'The user declined to run this command in the cloud workspace.' };
    }
    if (decision === 'always') setEscalationPolicy('always');
  }

  try {
    const outcome = await (opts.run ?? runTxnCommand)(opts.repoURL, command, (phase) => opts.onPhase?.(command, phase));
    const r = outcome.result;
    let stderr = r.stderr;
    if (r.timed_out) stderr += '\n[sprout] The command timed out in the cloud workspace.';
    if (r.truncated) stderr += '\n[sprout] Output was truncated.';
    if (outcome.skippedFiles > 0) {
      stderr += `\n[sprout] ${outcome.skippedFiles} changed file(s) weren't copied back to the browser workspace.`;
    }
    return { ran: true, stdout: r.stdout, stderr, exitCode: r.timed_out ? 124 : r.exit_code };
  } catch (err) {
    return {
      ran: false,
      message: `Running this in the cloud workspace failed: ${err instanceof Error ? err.message : String(err)}`,
    };
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
