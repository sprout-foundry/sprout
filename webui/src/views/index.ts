/**
 * Documented views entry point for embedding.
 *
 * `@sprout/ui` is the canonical home for *primitives*: props-only components
 * that render in isolation and never touch app services. The views a host
 * application actually composes — the chat surface, the agent-changes panel,
 * the files views, the preview panel — mostly need the webui context stack
 * (adapter fetch, client session, websocket bridge), so their canonical code
 * lives in `webui/src/components` (see docs/CONSUMPTION_GUIDE.md, "Primitive
 * vs Composite Rubric"). This module re-exports the four views behind
 * one documented, typed surface so an embedding shell has a single import
 * point and does not have to reach into webui internals.
 *
 * The four views and their contract requirements:
 *
 * | View                | Source        | Requires to render |
 * | ------------------- | ------------- | ------------------ |
 * | `ChatView`          | webui         | The webui context stack (see below). |
 * | `AgentChangesPanel` | webui         | The webui context stack (see below). |
 * | `FileTree`          | `@sprout/ui`  | Nothing — fully standalone primitive. |
 * | `Editor`            | `@sprout/ui`  | Nothing — fully standalone primitive. |
 * | `PreviewPane`       | `@sprout/ui`  | Nothing — standalone presentational; the parent supplies `status`, `url`, and the `onRestart`/`onClose` callbacks. |
 * | `PreviewPanel`      | webui         | `SproutProvider` + backend + events (see below). |
 *
 * webui-context views (`ChatView`, `AgentChangesPanel`, `PreviewPanel`) are
 * application-level composites: they read webui contexts (adapter, buffers,
 * notifications, …) and call webui services. Embedding them outside the full
 * webui app is not just a matter of importing them here — the host must
 * provide the webui provider stack (see `webui/src/App.tsx` for the
 * authoritative wiring):
 *
 * - `SproutAdapterProvider` (webui) — `useSproutFetch` / `useSproutAdapter`;
 *   wraps `@sprout/ui`'s `SproutProvider` (adapter + fetch via context,
 *   exported from `@sprout/ui`).
 * - `EventsContextProvider` (`@sprout/ui`) — the websocket event bridge
 *   (`@sprout/ui`).
 * - `NotificationProvider` (`@sprout/ui`) — toasts used across the views.
 * - Buffer/editor/pane/hotkey contexts (webui) — `ChatView` and
 *   `AgentChangesPanel` consume these.
 *
 * `PreviewPanel` specifics (via `usePreviewStatus`): it needs the
 * `/api/preview/*` backend endpoints and keeps itself fresh two ways — it
 * polls `/api/preview/status` on an interval (fast while starting, slow
 * heartbeat otherwise, only while the panel is open) and it listens on
 * `window` for the `sprout:wsevent` CustomEvent (dispatched by webui's
 * websocket bridge) to re-mount its iframe when workspace files change.
 * An embedding shell without webui's websocket bridge must dispatch
 * `sprout:wsevent` window events with the same shape, or rely on polling
 * alone. `usePreviewStatus` is re-exported here so a host can drive the
 * presentational `PreviewPane` itself instead of using `PreviewPanel`.
 *
 * Layout configuration: rather than composing
 * the views one by one, a host can hand `ViewsLayout` a
 * `ViewsArrangement` — which `ViewKind`s go in which `ViewSlot`
 * (`left`/`center`/`right`/`overlay`) — and the layout renders the shell.
 * The arrangement is merged against `DEFAULT_VIEWS_ARRANGEMENT` (the
 * built-in webui composition) per slot: an omitted slot keeps the
 * default's kinds, a provided slot replaces them wholesale (an explicit
 * `[]` empties the slot). An unknown kind throws a `TypeError` naming the
 * kind and the valid ones — a typo fails fast instead of silently dropping
 * a view. `ExampleEmbedding` demonstrates the smallest arrangement:
 * composing chat + preview from the exports above.
 *
 * Import direction: everything here is exported FROM webui; nothing in
 * `@sprout/ui` imports back from webui.
 */

/**
 * ViewsLayout — layout configuration.
 *
 * A host does not have to place the views one by one: it hands
 * `ViewsLayout` an arrangement (`ViewsArrangement`) — which view kinds go
 * in which slot (`left`/`center`/`right`/`overlay`) — and the layout
 * renders them. Slots merge against `DEFAULT_VIEWS_ARRANGEMENT` per slot:
 * an omitted slot keeps the default's kinds; a provided slot (including an
 * explicit `[]`) replaces the default's. An unknown kind throws a
 * `TypeError` naming the kind — a typo must fail fast, not silently drop a
 * view. `ExampleEmbedding` is the spec's worked example: it composes chat +
 * preview from these very exports.
 */
export {
  ViewsLayout,
  resolveViewsArrangement,
  DEFAULT_VIEWS_ARRANGEMENT,
  VIEWS_BY_KIND,
  SLOT_ORDER,
} from './ViewsLayout';
export type { ViewSlot, ViewKind, ViewsArrangement, ViewComponent, ViewsLayoutProps } from './ViewsLayout';
export { ExampleEmbedding } from './ExampleEmbedding';
export type { ExampleEmbeddingProps } from './ExampleEmbedding';

// Chat view (composite — requires the webui context stack).
export { default as ChatView } from '../components/ChatView';
export type { ChatProps } from '../components/chat/types';

// Agent changes view (composite — requires the webui context stack).
export { default as AgentChangesPanel } from '../components/AgentChangesPanel';
export type { AgentChangesPanelProps } from '../components/AgentChangesPanel';

// Files views (primitives — standalone, from @sprout/ui).
export { Editor, FileTree } from '@sprout/ui';
export type { EditorProps, FileTreeProps } from '@sprout/ui';

// Preview view (primitive pane from @sprout/ui + composite panel from webui).
export { PreviewPane } from '@sprout/ui';
export type { PreviewPaneProps } from '@sprout/ui';
export { PreviewPanel } from '../components/PreviewPanel';
export type { PreviewPanelProps } from '../components/PreviewPanel';
export { usePreviewStatus } from '../hooks/usePreviewStatus';
export type { UsePreviewStatusReturn } from '../hooks/usePreviewStatus';

// Copy keys (webui config — the embedding's wording seam).
export {
  copy,
  formatCopy,
  installCopy,
  resetCopyForTests,
  DEFAULT_COPY,
  COPY_KEYS,
  COPY_INSTALLED_EVENT,
} from '../config/copy';
export type { CopyKey, CopyOverrides } from '../config/copy';
