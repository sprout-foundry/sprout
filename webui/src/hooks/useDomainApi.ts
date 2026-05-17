/**
 * Adapter-aware API hooks.
 *
 * These hooks use useSproutFetch() to get a fetch function that routes through
 * the adapter (cloud mode) or falls back to clientFetch (local mode). Each hook
 * returns an object with bound API methods for a specific domain.
 *
 * Usage in components:
 *   const files = useFilesApi();
 *   const data = await files.getFiles();
 *
 *   const git = useGitApi();
 *   await git.stageFile('/path/to/file');
 */

import { useMemo } from 'react';
import { useSproutFetch } from '../contexts/SproutAdapterContext';
import * as files from '../services/api/filesApi';
import * as git from '../services/api/gitApi';
import * as chat from '../services/api/chatApi';
import * as terminal from '../services/api/terminalApi';
import * as settings from '../services/api/settingsApi';
import * as credentials from '../services/api/credentialsApi';
import * as workspace from '../services/api/workspaceApi';
import * as ssh from '../services/api/sshApi';
import * as search from '../services/api/searchApi';
import * as editor from '../services/api/editorApi';
import * as onboarding from '../services/api/onboardingApi';
import * as session from '../services/api/sessionApi';
import * as misc from '../services/api/miscApi';

/**
 * Extract all but the first element from a tuple type.
 *
 * The generic constraints use `unknown[]` for type safety — they model function
 * parameter tuples where the first entry is the injected `fetchFn` and the
 * caller should never reference it.
 */
type Tail<T extends unknown[]> = T extends [unknown, ...infer R] ? R : never;

/**
 * Creates bound API methods from a domain module and a fetch function.
 * Binds the fetch function as the first argument to every exported function.
 *
 * The generic constraint uses `never[]` to accept any function signature.
 * Contravariance of function parameters means `(a: A, b: B) => R` extends
 * `(...args: never[]) => unknown`, so the constraint matches all functions
 * while preserving full type inference for the bound result.
 */
function bindModule<T extends Record<string, (...args: never[]) => unknown>>(
  mod: T,
  fetchFn: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>,
): { [K in keyof T]: (...args: Tail<Parameters<T[K]>>) => ReturnType<T[K]> } {
  const result: Record<string, (...args: unknown[]) => unknown> = {};
  for (const key of Object.keys(mod)) {
    if (typeof mod[key] === 'function') {
      result[key] = (...args: unknown[]) => (mod[key] as (...a: unknown[]) => unknown)(fetchFn, ...args);
    }
  }
  // Type-system boundary: the result shape is guaranteed by the generic constraint
  // but TypeScript cannot verify the mapped return type directly.
  return result as { [K in keyof T]: (...args: Tail<Parameters<T[K]>>) => ReturnType<T[K]> };
}

/**
 * Files API hook — adapter-aware file operations.
 */
export function useFilesApi() {
  const fetchFn = useSproutFetch();
  return useMemo(() => bindModule(files, fetchFn), [fetchFn]);
}

/**
 * Git API hook — adapter-aware git operations.
 */
export function useGitApi() {
  const fetchFn = useSproutFetch();
  return useMemo(() => bindModule(git, fetchFn), [fetchFn]);
}

/**
 * Chat API hook — adapter-aware chat/agent operations.
 */
export function useChatApi() {
  const fetchFn = useSproutFetch();
  return useMemo(() => bindModule(chat, fetchFn), [fetchFn]);
}

/**
 * Terminal API hook — adapter-aware terminal operations.
 */
export function useTerminalApi() {
  const fetchFn = useSproutFetch();
  return useMemo(() => bindModule(terminal, fetchFn), [fetchFn]);
}

/**
 * Settings API hook — adapter-aware settings operations.
 */
export function useSettingsApi() {
  const fetchFn = useSproutFetch();
  return useMemo(() => bindModule(settings, fetchFn), [fetchFn]);
}

/**
 * Credentials API hook — adapter-aware credential operations.
 */
export function useCredentialsApi() {
  const fetchFn = useSproutFetch();
  return useMemo(() => bindModule(credentials, fetchFn), [fetchFn]);
}

/**
 * Workspace API hook — adapter-aware workspace operations.
 */
export function useWorkspaceApi() {
  const fetchFn = useSproutFetch();
  return useMemo(() => bindModule(workspace, fetchFn), [fetchFn]);
}

/**
 * SSH API hook — adapter-aware SSH and instance operations.
 */
export function useSSHApi() {
  const fetchFn = useSproutFetch();
  return useMemo(() => bindModule(ssh, fetchFn), [fetchFn]);
}

/**
 * Search API hook — adapter-aware search operations.
 */
export function useSearchApi() {
  const fetchFn = useSproutFetch();
  return useMemo(() => bindModule(search, fetchFn), [fetchFn]);
}

/**
 * Editor API hook — adapter-aware editor/semantic operations.
 */
export function useEditorApi() {
  const fetchFn = useSproutFetch();
  return useMemo(() => bindModule(editor, fetchFn), [fetchFn]);
}

/**
 * Onboarding API hook — adapter-aware onboarding operations.
 */
export function useOnboardingApi() {
  const fetchFn = useSproutFetch();
  return useMemo(() => bindModule(onboarding, fetchFn), [fetchFn]);
}

/**
 * Session API hook — adapter-aware session operations.
 */
export function useSessionApi() {
  const fetchFn = useSproutFetch();
  return useMemo(() => bindModule(session, fetchFn), [fetchFn]);
}

/**
 * Misc API hook — adapter-aware stats, health, providers, etc.
 */
export function useMiscApi() {
  const fetchFn = useSproutFetch();
  return useMemo(() => bindModule(misc, fetchFn), [fetchFn]);
}
