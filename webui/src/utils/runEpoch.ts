/**
 * Per-chat run-start counters.
 *
 * A status snapshot (stats, query status, a chat switch response) is fetched
 * asynchronously; by the time it lands a new run may already have started and
 * the snapshot's "idle" is stale. Callers capture {@link runEpoch} when they
 * request a snapshot and compare it when they apply it: a changed epoch means
 * a run started in between, so an idle snapshot must not end it.
 */

const epochs = new Map<string, number>();

const keyOf = (chatId: string | null | undefined): string => chatId ?? '';

/** Record that a run started (or was sent) for the chat. */
export function noteRunStarted(chatId: string | null | undefined): void {
  const key = keyOf(chatId);
  epochs.set(key, (epochs.get(key) ?? 0) + 1);
}

/** The chat's current run-start counter. */
export function runEpoch(chatId: string | null | undefined): number {
  return epochs.get(keyOf(chatId)) ?? 0;
}

/** Clears every counter. Tests only. */
export function resetRunEpochs(): void {
  epochs.clear();
}
