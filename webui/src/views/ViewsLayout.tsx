import { Editor, FileTree, PreviewPane } from '@sprout/ui';
import type { ElementType } from 'react';
import AgentChangesPanel from '../components/AgentChangesPanel';
import ChatView from '../components/ChatView';
import { PreviewPanel } from '../components/PreviewPanel';
import './ViewsLayout.css';

/**
 * Layout configuration + arrangement renderer.
 *
 * "The layout configuration accepts an embedding-supplied arrangement": a
 * host application describes WHICH views go in WHICH slot and this component
 * renders them. The views themselves are the ones exported from the views
 * entry (`./index`) — the host composes a shell out of the same view
 * components the built-in webui uses, without forking the web UI.
 *
 * The slot model:
 *
 *   - `left` / `center` / `right` — the three columns of a work surface.
 *   - `overlay` — a positioned layer above the columns (the preview panel
 *     lives here in the built-in arrangement).
 *
 * Each slot takes an ordered list of view kinds; the slot renders its views
 * in that order. Composite views (`chat`, `changes`, `previewPanel`) read
 * the webui context stack (adapter, buffers, notifications, …) and render
 * correctly only inside a host that provides it — see the views entry
 * header for the provider list and `webui/src/App.tsx` for the authoritative
 * wiring. The primitive views (`fileTree`, `editor`, `previewPane`) render
 * standalone.
 *
 * Slot CSS here is minimal scaffolding (see ViewsLayout.css): it makes the
 * slots exist and be visible, nothing more. Host CSS governs real
 * placement.
 */

/** A named region of the arranged shell. */
export type ViewSlot = 'left' | 'center' | 'right' | 'overlay';

/** Slots in render order. */
export const SLOT_ORDER: readonly ViewSlot[] = ['left', 'center', 'right', 'overlay'];

/**
 * A view an arrangement can place. Composite kinds (`chat`, `changes`,
 * `previewPanel`) require the webui context stack; primitives
 * (`fileTree`, `editor`, `previewPane`) render standalone.
 */
export type ViewKind = 'chat' | 'changes' | 'fileTree' | 'editor' | 'previewPane' | 'previewPanel';

/**
 * The arrangement: which view kinds render in which slot, in order. An
 * omitted slot keeps the default's content for that slot (see
 * `resolveViewsArrangement`); a present slot — including an explicit `[]` —
 * replaces the default wholesale.
 */
export interface ViewsArrangement {
  left?: ViewKind[];
  center?: ViewKind[];
  right?: ViewKind[];
  overlay?: ViewKind[];
}

/**
 * What an arrangement entry renders as. `ElementType` (not
 * `ComponentType`): the registry includes `FileTree`, a forwardRef
 * component, which a plain `ComponentType<object>` rejects.
 */
export type ViewComponent = ElementType;

/**
 * The built-in webui arrangement: files at left, the chat at center, the
 * agent-changes panel at right, the preview panel as an overlay. This is
 * what the web UI itself composes; an embedding supplies its own arrangement
 * on top of it.
 */
export const DEFAULT_VIEWS_ARRANGEMENT: ViewsArrangement = {
  left: ['fileTree'],
  center: ['chat'],
  right: ['changes'],
  overlay: ['previewPanel'],
};

/**
 * Kind → view component. The single place a view kind is bound to its
 * implementation; `resolveViewsArrangement` validates against it and
 * `ViewsLayout` renders from it.
 */
export const VIEWS_BY_KIND: Record<ViewKind, ViewComponent> = {
  chat: ChatView,
  changes: AgentChangesPanel,
  fileTree: FileTree,
  editor: Editor,
  previewPane: PreviewPane,
  previewPanel: PreviewPanel,
};

/**
 * Merge an embedding-supplied arrangement onto the default, per slot.
 *
 * - An omitted slot keeps the default's kinds for that slot.
 * - A provided slot REPLACES the default — an explicit `[]` empties the
 *   slot, which is how an embedding drops a built-in view it does not want.
 *
 * Unknown kinds throw a `TypeError` naming the offending kind and the valid
 * ones: a typo in an arrangement must fail fast, not silently drop a view.
 * Returns a fresh object; the default is never handed out mutated.
 */
export function resolveViewsArrangement(supplied?: ViewsArrangement): ViewsArrangement {
  const resolved: ViewsArrangement = {};
  for (const slot of SLOT_ORDER) {
    if (!supplied || supplied[slot] === undefined) {
      // Copy the default's slot array — never hand out (or share) the
      // default's own arrays, which a caller could mutate.
      resolved[slot] = [...(DEFAULT_VIEWS_ARRANGEMENT[slot] ?? [])];
      continue;
    }
    const provided = supplied[slot];
    for (const kind of provided) {
      if (!(kind in VIEWS_BY_KIND)) {
        throw new TypeError(
          `Unknown view kind "${kind}" in slot "${slot}". Valid kinds: ${Object.keys(VIEWS_BY_KIND).join(', ')}`,
        );
      }
    }
    resolved[slot] = [...provided];
  }
  return resolved;
}

/** Props for the layout: the arrangement, per-view props, and a class hook. */
export interface ViewsLayoutProps {
  /** The embedding-supplied arrangement; omitted renders the built-in one. */
  arrangement?: ViewsArrangement;
  /** Extra props handed to each view, keyed by its kind. */
  props?: Partial<Record<ViewKind, object>>;
  /** Additional class on the root, alongside `sprout-views-layout`. */
  className?: string;
}

/**
 * Render an arrangement: one slot container per slot in `SLOT_ORDER`, each
 * rendering its kinds in order with the matching entry from `props`.
 */
export function ViewsLayout({ arrangement, props, className }: ViewsLayoutProps): JSX.Element {
  const resolved = resolveViewsArrangement(arrangement);
  const rootClass = className ? `sprout-views-layout ${className}` : 'sprout-views-layout';
  return (
    <div className={rootClass} data-testid="views-layout">
      {SLOT_ORDER.map((slot) => (
        <div key={slot} data-slot={slot} className="sprout-views-layout__slot" data-testid={`views-slot-${slot}`}>
          {(resolved[slot] ?? []).map((kind) => {
            const View = VIEWS_BY_KIND[kind];
            return <View key={kind} {...(props?.[kind] ?? {})} />;
          })}
        </div>
      ))}
    </div>
  );
}

export default ViewsLayout;
