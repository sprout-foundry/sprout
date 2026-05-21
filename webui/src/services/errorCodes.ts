/**
 * Server error code registry — SP-034-5f.
 *
 * Centralizes the wire-format codes returned by the Go backend so the
 * frontend can branch on a single source of truth instead of repeated
 * `errorCode === 'foo'` string comparisons scattered across handlers.
 *
 * When the server adds a new code:
 * 1. Add it to ServerErrorCode below
 * 2. If the code has a default UX (toast, modal, reload, etc.), add a
 *    handler in the dispatcher consumers
 *
 * Pin the strings to what the Go side actually emits — `pkg/webui`
 * grep for `"code":` to see the source of truth.
 */

/**
 * Known structured error codes the Go backend may attach to error
 * events. Unknown codes are not type errors — they pass through as
 * plain strings — but anything appearing in this union has a defined
 * meaning and (typically) a documented frontend reaction.
 */
export type ServerErrorCode =
  | 'config_conflict' // SP-034-4: config file changed on disk under us
  | 'no_provider' // no LLM provider configured — trigger onboarding
  | 'model_not_available' // configured model is gone — show model selector
  | 'invalid_request' // generic validation failure on inbound message
  | 'unauthorized' // service-mode auth failure
  | (string & {}); // accept any code; this trick keeps autocomplete on the known ones

/**
 * Shape we expect on the `data` field of any error-type WS event or
 * structured fetch response. All fields optional — older server builds
 * or generic failures may omit some.
 */
export interface ServerErrorData {
  code?: string;
  message?: string;
  details?: unknown;
  // Per-code extras (e.g. config_conflict.current_summary, no_provider.providers)
  // ride alongside as additional keys; consumers can destructure them.
  [extra: string]: unknown;
}

/**
 * Pulls the error code out of an unknown payload. Returns "" when the
 * payload doesn't carry one or has a non-string code. Safe to call on
 * anything — never throws.
 */
export function getServerErrorCode(payload: unknown): string {
  if (!payload || typeof payload !== 'object') return '';
  const code = (payload as Record<string, unknown>).code;
  return typeof code === 'string' ? code : '';
}

/**
 * Type guard for the known codes. Mostly useful in test code; runtime
 * comparison against the string literal works the same way.
 */
const KNOWN_CODES: ReadonlySet<string> = new Set<ServerErrorCode>([
  'config_conflict',
  'no_provider',
  'model_not_available',
  'invalid_request',
  'unauthorized',
]);

export function isKnownServerErrorCode(code: string): boolean {
  return KNOWN_CODES.has(code);
}

/**
 * Lightweight code-keyed dispatcher. Consumers register handlers for
 * the codes they care about; the dispatcher returns true when a
 * registered handler ran, false otherwise — letting the caller fall
 * through to its generic error path.
 *
 * Use case: `useEventHandler.ts`'s case 'error' block dispatches by
 * code and falls back to the generic "FAIL Error: ..." rendering when
 * no handler matched.
 */
export type ServerErrorHandler = (data: ServerErrorData) => void;

export function dispatchServerError(
  data: ServerErrorData,
  handlers: Partial<Record<ServerErrorCode, ServerErrorHandler>>,
): boolean {
  const code = getServerErrorCode(data);
  if (!code) return false;
  const handler = (handlers as Record<string, ServerErrorHandler | undefined>)[code];
  if (typeof handler === 'function') {
    handler(data);
    return true;
  }
  return false;
}
