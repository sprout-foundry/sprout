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
import type { ChatProps } from '@sprout/ui';
import { useMemo } from 'react';
import type { PreviewPanelProps } from '../components/PreviewPanel';
import { clientFetch } from '../services/clientSession';
import { LocalEventsProvider } from '../services/localEventsProvider';
import type { AppState } from '../types/app';
import type { ViewKind } from './ViewsLayout';
import { useWorkspaceFileTree } from './useWorkspaceFileTree';
import { createEmptyChatState, useWorkspaceChatProps } from './WorkspaceChatContext';
import type { WorkspaceChatViewProps } from './WorkspaceChatContext';
import { useWorkspaceEditorProps } from './WorkspaceEditorContext';

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
 * The chat unit's view props with the host's `chatOverrides` merged over the
 * assembled chat props. The overrides are the host's own callbacks and
 * values (the model picker, session restore, fork, …) that the unit does not
 * own; anything they set wins over the unit's value for that prop.
 */
export function useSproutWorkspaceChatViewProps(chatOverrides?: Partial<ChatProps>): WorkspaceChatViewProps {
  const { chatProps, reviewProps, diffState } = useWorkspaceChatProps();
  const merged = useMemo(
    () => (chatOverrides ? { ...chatProps, ...chatOverrides } : chatProps),
    [chatProps, chatOverrides],
  );
  return useMemo(() => ({ chatProps: merged, reviewProps, diffState }), [merged, reviewProps, diffState]);
}

/** What the composition assembles beyond the units' own props. */
export interface SproutWorkspaceViewAssembly {
  /** Merged over the assembled chat props (see `useSproutWorkspaceChatViewProps`). */
  chatOverrides?: Partial<ChatProps>;
  /** The preview panel's open state and close handler, owned by the composition. */
  previewPanel?: PreviewPanelProps;
}

/**
 * Assemble the layout's per-kind props from the chat and editor units and the
 * host's overrides. Runs inside the `WorkspaceChatProvider` and
 * `WorkspaceEditorProvider` the units need: both are mounted around the
 * layout, and this reads their state below them. A kind the host supplies
 * replaces the assembled props for that kind, so a host composing its own
 * surface is not fighting the unit's — except `previewPanel`, whose host
 * props are merged over the assembled `open`/`onClose`, so a host can pass
 * just `open` to drive it.
 *
 * - `chat` — the chat unit's view props, with `chatOverrides` merged over.
 * - `fileTree` — the workspace file API (`useWorkspaceFileTree`), with
 *   selection wired to the editor unit: picking a file opens it, and the
 *   active file is the selected one.
 * - `changes` — `onFileClick`, so a changed file opens in the editor. The
 *   panel still fetches its own session changes (when the host declares
 *   `capabilities.agentChanges`).
 * - `previewPanel` — the composition's open state and close handler.
 * - `git` — `onOpenFile`, so a changed file opens in the editor unit.
 * - `search` — `onFileClick`, so a result opens in the editor at its line.
 *
 * The `editor` kind takes no props here: the composition renders the
 * assembled editor view for it (see `SproutWorkspace`).
 */
export function useSproutWorkspaceViewProps(
  overrides?: Partial<Record<ViewKind, object>>,
  { chatOverrides, previewPanel }: SproutWorkspaceViewAssembly = {},
): Partial<Record<ViewKind, object>> {
  const { chatProps } = useSproutWorkspaceChatViewProps(chatOverrides);
  const editor = useWorkspaceEditorProps();
  const fileTree = useWorkspaceFileTree({
    onFileSelect: editor.fileTree.onFileSelect,
    selectedFile: editor.fileTree.selectedFile,
  });

  return useMemo<Partial<Record<ViewKind, object>>>(() => {
    const assembled: Partial<Record<ViewKind, object>> = {
      chat: chatProps,
      fileTree,
      changes: { onFileClick: editor.onFileClick },
      git: { onOpenFile: editor.onFileClick },
      search: { onFileClick: editor.onFileClick },
    };
    if (previewPanel) assembled.previewPanel = previewPanel;
    if (overrides) {
      for (const kind of Object.keys(overrides) as ViewKind[]) {
        assembled[kind] =
          kind === 'previewPanel' ? { ...assembled.previewPanel, ...overrides.previewPanel } : overrides[kind];
      }
    }
    return assembled;
  }, [chatProps, fileTree, editor.onFileClick, previewPanel, overrides]);
}
