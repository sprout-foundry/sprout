/**
 * The chat a composed workspace mounts.
 *
 * A host that mounts a workspace through `SproutWorkspace` should get a
 * working chat without wiring one: this module owns the events provider the
 * chat unit and the provider stack share, and assembles the chat view props
 * from the chat unit so the layout renders a live chat. It is the internal
 * half of `SproutWorkspace` — the public surface is the workspace component
 * and the chat unit it is built on — so it is not re-exported from the views
 * entry.
 *
 * The events provider is owned here rather than defaulted inside
 * `SproutProviders`: the unit subscribes to the transport, so the wrapper and
 * the unit must share one instance. `SproutWorkspace` receives it from this
 * hook and passes the same instance to both.
 */

import type { EventsProvider } from '@sprout/events';
import { useMemo } from 'react';
import { clientFetch } from '../services/clientSession';
import { LocalEventsProvider } from '../services/localEventsProvider';
import type { AppState } from '../types/app';
import type { ViewKind } from './ViewsLayout';
import { createEmptyChatState, useWorkspaceChatProps } from './WorkspaceChatContext';

/** The transport + fetch the chat unit and the provider stack share. */
export interface SproutWorkspaceChatTransport {
  eventsProvider: EventsProvider;
  fetchFn: typeof fetch;
  /** The chat state the unit starts from (empty unless the host supplies one). */
  initialState: AppState;
}

export interface SproutWorkspaceChatOptions {
  /** The chat state to start from; defaults to an empty chat. */
  initialState?: AppState;
  /** The host's fetch; defaults to the app's adapter-aware `clientFetch`. */
  fetchFn?: typeof fetch;
}

/**
 * Own the transport a composed workspace's chat runs on. The events provider
 * is created once and never replaced, so the provider stack and the chat unit
 * share one identity across re-renders.
 */
export function useSproutWorkspaceChatTransport({
  initialState,
  fetchFn,
}: SproutWorkspaceChatOptions = {}): SproutWorkspaceChatTransport {
  const eventsProvider = useMemo(() => new LocalEventsProvider(), []);
  const initial = useMemo(() => initialState ?? createEmptyChatState(), [initialState]);
  return { eventsProvider, fetchFn: fetchFn ?? clientFetch, initialState: initial };
}

/**
 * Assemble the layout's per-kind props from the chat unit and the host's
 * overrides. Runs inside the `WorkspaceChatProvider` the unit needs: the
 * provider is mounted around the layout, and this reads the chat state below
 * it. A kind the host supplies replaces the assembled props for that kind, so
 * a host composing its own chat surface is not fighting the unit's.
 *
 * Only the `chat` kind is assembled here. The changes view self-fetches its
 * session changes and takes only optional callbacks the app owns
 * (`onAskAgent`, `onFileClick`, …) — the composition has no such callbacks to
 * hand it, so it is left to render with none rather than handed props it does
 * not read.
 */
export function useSproutWorkspaceViewProps(
  overrides?: Partial<Record<ViewKind, object>>,
): Partial<Record<ViewKind, object>> {
  const { chatProps } = useWorkspaceChatProps();

  return useMemo<Partial<Record<ViewKind, object>>>(() => {
    const assembled: Partial<Record<ViewKind, object>> = { chat: chatProps };
    if (overrides) {
      for (const kind of Object.keys(overrides) as ViewKind[]) {
        assembled[kind] = overrides[kind];
      }
    }
    return assembled;
  }, [chatProps, overrides]);
}
