/**
 * Safe JSON helpers.
 *
 * WebUI boundary hardening: every place that decodes JSON we did not produce
 * ourselves — bridge payloads (WASM/native), localStorage values, server
 * responses, WebSocket frames — should go through `safeJsonParse` so a
 * malformed payload degrades into a fallback instead of throwing into a
 * render path or an event handler.
 *
 * `JSON.parse` is still correct for strings the app itself serialized in the
 * same tick (and for test fixtures); don't blanket-replace those.
 */

import { debugLog } from './log';

/** Default cap on message length surfaced for a parse failure. */
const PARSE_ERROR_LOG_LIMIT = 200;

/**
 * Parse `value` as JSON, returning `fallback` when it is not valid JSON.
 *
 * Also returns `fallback` for `undefined`/`null`/non-string inputs, so a
 * missing storage entry or an unset bridge field needs no separate guard.
 */
export function safeJsonParse<T>(value: unknown, fallback: T): T {
  if (typeof value !== 'string' || value.trim() === '') {
    return fallback;
  }
  try {
    return JSON.parse(value) as T;
  } catch (err) {
    debugLog(
      '[json] safeJsonParse failed:',
      err,
      `input=${String(value).slice(0, PARSE_ERROR_LOG_LIMIT)}${value.length > PARSE_ERROR_LOG_LIMIT ? '…' : ''}`,
    );
    return fallback;
  }
}

/**
 * Parse `value` as JSON, returning `null` when it is not valid JSON.
 * Convenient when `null` already means "absent" at the call site.
 */
export function safeJsonParseOrNull(value: unknown): unknown | null {
  return safeJsonParse<unknown | null>(value, null);
}

/**
 * Parse `value` as a JSON object (`{ ... }`), returning `fallback` when it is
 * not valid JSON *or* not a plain object. Use for bridge/server payloads that
 * are expected to be keyed records.
 */
export function safeJsonObject(value: unknown, fallback: Record<string, unknown> = {}): Record<string, unknown> {
  const parsed = safeJsonParse<unknown>(value, null);
  if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
    return parsed as Record<string, unknown>;
  }
  return fallback;
}
