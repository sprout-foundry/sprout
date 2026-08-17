import type { SlashCommand } from './slashCommands';

/**
 * Slash-command ARGUMENT completion for the WebUI command bar.
 *
 * The terminal already completes slash-command arguments (e.g. the values
 * for `/risk-profile`, `/model`, `/persona`) via the rich completer in
 * package cmd. This module brings the same capability to the browser by
 * talking to `POST /api/command/complete` (pkg/webui/api_command_complete.go),
 * whose wire shape mirrors the terminal's completer:
 *
 *   Request:  {"command": "/risk-profile per"}
 *   Response: {"command": "risk-profile", "completions": [{"text": "permissive", "description": ""}]}
 *
 * The component that consumes this stays fully usable without a server
 * (Storybook, tests, standalone): everything here degrades to an empty
 * result on any error, and CommandInput only calls the API once the user
 * is past the command name (a space separates the name from the argument).
 */

/** One completion entry returned by the server. */
export interface CommandCompletion {
  text: string;
  description: string;
}

/** Successful 200 body of POST /api/command/complete. */
export interface CommandCompletionResponse {
  command: string;
  completions: CommandCompletion[];
}

/**
 * Minimal interface for API operations needed by slash-command argument
 * completion. The host application provides this via the CommandInput
 * component, mirroring the CommandHistoryApi pattern.
 */
export interface CommandCompletionApi {
  completeCommand(command: string): Promise<CommandCompletionResponse>;
}

/**
 * Trailing-debounce delay for argument-completion requests. Kept in this
 * module (rather than inline in CommandInput) so tests and consumers share
 * one constant.
 */
export const ARGUMENT_COMPLETION_DEBOUNCE_MS = 150;

/**
 * Fetch-based CommandCompletionApi. POSTs the raw command text to
 * `${baseUrl}/api/command/complete` and parses the JSON response.
 *
 * Never throws: a non-OK HTTP status, a network rejection, or malformed
 * JSON all degrade to an empty result so the UI simply shows no argument
 * completions (matching the no-server/no-API behavior).
 *
 * The optional `signal` lets callers abort an in-flight request; the
 * CommandInput component itself relies on a request-generation counter for
 * stale-response dropping, so this is purely an escape hatch.
 */
export function createHttpCommandCompletionApi(
  fetchFn: typeof fetch = fetch,
  baseUrl = '',
): CommandCompletionApi {
  return {
    async completeCommand(command: string, signal?: AbortSignal): Promise<CommandCompletionResponse> {
      try {
        const response = await fetchFn(`${baseUrl}/api/command/complete`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ command }),
          signal,
        });
        if (!response.ok) {
          return { command: '', completions: [] };
        }
        const data = (await response.json()) as Partial<CommandCompletionResponse>;
        return {
          command: typeof data.command === 'string' ? data.command : '',
          completions: Array.isArray(data.completions) ? data.completions : [],
        };
      } catch {
        return { command: '', completions: [] };
      }
    },
  };
}

/**
 * Map a server completion response to dropdown rows. Argument-phase rows
 * (raw candidate text, empty description) become SlashCommand entries with
 * `isArgument: true` so SlashCommandAutocomplete renders them WITHOUT the
 * leading "/" (argument values are not command names). Empty/malformed
 * entries are filtered out.
 */
export function argumentCandidatesFromResponse(resp: CommandCompletionResponse | null | undefined): SlashCommand[] {
  if (!resp || !Array.isArray(resp.completions)) {
    return [];
  }
  return resp.completions
    .filter((c) => c && typeof c.text === 'string' && c.text.trim().length > 0)
    .map((c) => ({
      name: c.text,
      description: c.description ?? '',
      isArgument: true,
    }));
}

/**
 * Replace the LAST whitespace-delimited word of `text` with `candidate`,
 * appending a trailing space so the user can immediately type the next
 * argument. Whitespace semantics match the server's strings.Fields (spaces
 * and tabs).
 *
 * Examples:
 *   replaceLastWord('/risk-profile per', 'permissive')
 *     → { value: '/risk-profile permissive ', cursor: 25 }
 *   replaceLastWord('/risk-profile ', 'permissive')   // empty last word
 *     → { value: '/risk-profile permissive ', cursor: 25 }
 *
 * `cursor` is the position just after the inserted candidate + space, which
 * is where the input caret should land.
 */
export function replaceLastWord(text: string, candidate: string): { value: string; cursor: number } {
  // Locate the LAST run of whitespace separators that is followed only by
  // the word being replaced (possibly empty when text ends with a space).
  // e.g. "/risk-profile per"  → separator at index 12 → prefix "/risk-profile "
  //      "/risk-profile "     → trailing space, empty last word → prefix "/risk-profile "
  //      "per"                → no separator → prefix "" (whole text is the word)
  const lastSeparatorIndex = text.search(/[\t ]+(?=[^\t ]*$)/);
  const prefix = lastSeparatorIndex < 0 ? '' : text.slice(0, lastSeparatorIndex + 1);
  const value = prefix + candidate + ' ';
  return { value, cursor: value.length };
}

/** Minimal trailing debouncer used to coalesce per-keystroke requests. */
export interface Debouncer {
  debounce(fn: () => void): void;
  cancel(): void;
}

/**
 * Trailing debouncer: repeated `debounce(fn)` calls within `ms` reset the
 * timer, so `fn` runs at most once after the last call. `cancel()` drops a
 * pending invocation. Created per-instance by CommandInput; the underlying
 * timer is an ordinary setTimeout, so it plays well with vi.useFakeTimers.
 */
export function createDebouncer(ms: number): Debouncer {
  let timer: ReturnType<typeof setTimeout> | null = null;
  return {
    debounce(fn: () => void) {
      if (timer !== null) {
        clearTimeout(timer);
      }
      timer = setTimeout(() => {
        timer = null;
        fn();
      }, ms);
    },
    cancel() {
      if (timer !== null) {
        clearTimeout(timer);
        timer = null;
      }
    },
  };
}
