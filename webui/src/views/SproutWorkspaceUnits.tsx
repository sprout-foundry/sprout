/**
 * The own-mode content of `SproutWorkspace`: what renders inside the chat and
 * editor units the composition mounts. Split from `SproutWorkspace` because
 * both paths read the units' state, which only exists below their providers.
 *
 * - `ArrangedViews` — the `layout` path: the arranged views with their
 *   assembled props.
 *
 * The space-shell path without host `shellProps` (`ComposedSpaceShell`)
 * lives in `SproutWorkspaceSpaceShell`.
 *
 * Internal to the composition; not re-exported from the views entry.
 */

import type { ChatProps } from '@sprout/ui';
import { useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import type { PreviewPanelProps } from '../components/PreviewPanel';
import { useTogglePreviewCommand } from '../hooks/useTogglePreviewCommand';
import { HostContext } from '../host';
import { useSproutWorkspaceViewProps } from './SproutWorkspaceChat';
import { resolveViewsArrangement, SLOT_ORDER, ViewsLayout } from './ViewsLayout';
import type { ViewComponent, ViewsArrangement, ViewKind } from './ViewsLayout';
import { useWorkspaceEditor } from './WorkspaceEditorContext';
import { WorkspaceEditorView } from './WorkspaceEditorView';
import { WorkspaceFileTreeView } from './WorkspaceFileTreeView';

/**
 * The components the composition renders in place of the primitives: the
 * assembled editor for `editor`, and a tree that reloads when the workspace's
 * contents arrive for `fileTree`.
 */
const ASSEMBLED_VIEW_COMPONENTS: Partial<Record<ViewKind, ViewComponent>> = {
  editor: WorkspaceEditorView,
  fileTree: WorkspaceFileTreeView,
};

/** With a host-supplied `viewProps.editor`, the `editor` kind keeps the bare primitive. */
const HOST_EDITOR_VIEW_COMPONENTS: Partial<Record<ViewKind, ViewComponent>> = { fileTree: WorkspaceFileTreeView };

/** The editor shown in the center slot while files are open in an arrangement without one. */
function OnDemandEditorView(): JSX.Element {
  return <WorkspaceEditorView className="sprout-workspace-editor--on-demand" />;
}

const ON_DEMAND_EDITOR_VIEW_COMPONENTS: Partial<Record<ViewKind, ViewComponent>> = {
  ...ASSEMBLED_VIEW_COMPONENTS,
  editor: OnDemandEditorView,
};

/**
 * The arranged views, rendered inside the chat and editor units so their
 * assembled props are available.
 *
 * The `editor` kind renders the assembled editor view unless the host
 * supplies `viewProps.editor`; then the host gets the bare `Editor` primitive
 * with its own props.
 *
 * An arrangement without an `editor` still opens files: selecting one in the
 * tree (or the changes panel, a chat file link, the palette) opens it in the
 * editor unit, and while any file is open the assembled editor is shown at
 * the start of the `center` slot, splitting it with what the host placed
 * there. Closing the last file removes it again. Without this the selection
 * would open a buffer nothing renders, so nothing would load or show.
 *
 * An arranged `previewPanel` is mounted only when the host declares
 * `capabilities.preview` (its backend serves `/api/preview/*`); otherwise it
 * is dropped from the arrangement, so it renders nothing and makes no
 * request. When mounted it starts closed and is opened by the `toggle_preview`
 * command (the command palette, or a host dispatching it); its close
 * affordance closes it. A host that passes `open` through
 * `viewProps.previewPanel` drives it instead (with its `onClose`), and the
 * command is not registered.
 */
export function ArrangedViews({
  arrangement,
  viewProps,
  chatOverrides,
}: {
  arrangement: ViewsArrangement;
  viewProps?: Partial<Record<ViewKind, object>>;
  chatOverrides?: Partial<ChatProps>;
}): JSX.Element {
  const resolved = usePreviewGatedArrangement(arrangement);
  const hostControlled = (viewProps?.previewPanel as Partial<PreviewPanelProps> | undefined)?.open !== undefined;
  const previewArranged = arrangementHas(resolved, 'previewPanel');
  const [previewOpen, setPreviewOpen] = useState(false);
  const closePreview = useCallback(() => setPreviewOpen(false), []);
  useTogglePreviewCommand(setPreviewOpen, previewArranged && !hostControlled);
  const previewPanel = useMemo<PreviewPanelProps>(
    () => ({ open: previewOpen, onClose: closePreview }),
    [previewOpen, closePreview],
  );
  const props = useSproutWorkspaceViewProps(viewProps, { chatOverrides, previewPanel });
  useOpenInEditorRequests();
  const { openFiles } = useWorkspaceEditor();
  const onDemandEditor = !viewProps?.editor && !arrangementHas(resolved, 'editor') && openFiles.length > 0;
  const shown = useMemo(
    () => (onDemandEditor ? { ...resolved, center: ['editor' as const, ...(resolved.center ?? [])] } : resolved),
    [resolved, onDemandEditor],
  );
  let components = ASSEMBLED_VIEW_COMPONENTS;
  if (viewProps?.editor) components = HOST_EDITOR_VIEW_COMPONENTS;
  else if (onDemandEditor) components = ON_DEMAND_EDITOR_VIEW_COMPONENTS;
  return <ViewsLayout arrangement={shown} props={props} components={components} className="sprout-workspace__layout" />;
}

/** Whether any slot of a resolved arrangement places `kind`. */
function arrangementHas(arrangement: ViewsArrangement, kind: ViewKind): boolean {
  return SLOT_ORDER.some((slot) => arrangement[slot]?.includes(kind) ?? false);
}

/**
 * Resolve an arrangement against the default and drop `previewPanel` when the
 * host's backend does not serve the preview lifecycle, so the panel is never
 * mounted there (and never polls `/api/preview/status`). Reads the host
 * through the context directly: an ambient caller may render the layout
 * without a `HostProvider`, which counts as no capability.
 */
export function usePreviewGatedArrangement(arrangement?: ViewsArrangement): ViewsArrangement {
  const previewCapable = useContext(HostContext)?.capabilities.preview === true;
  return useMemo(() => {
    const resolved = resolveViewsArrangement(arrangement);
    if (previewCapable) return resolved;
    const gated: ViewsArrangement = {};
    for (const slot of SLOT_ORDER) {
      gated[slot] = (resolved[slot] ?? []).filter((kind) => kind !== 'previewPanel');
    }
    return gated;
  }, [arrangement, previewCapable]);
}

/**
 * Open the files the rest of the workspace asks for — file links in the chat
 * and tool output dispatch `sprout:open-in-editor` — in the editor unit. The
 * standalone app handles the same event at its root (where it also switches
 * the view), so only the composed workspace listens here.
 */
export function useOpenInEditorRequests(onOpen?: () => void): void {
  const { openFile } = useWorkspaceEditor();
  const onOpenRef = useRef(onOpen);
  onOpenRef.current = onOpen;
  useEffect(() => {
    const handle = (event: Event) => {
      const detail = (event as CustomEvent<{ path?: string; lineNumber?: number }>).detail;
      if (!detail?.path) return;
      openFile(detail.path, { line: detail.lineNumber });
      onOpenRef.current?.();
    };
    window.addEventListener('sprout:open-in-editor', handle);
    return () => window.removeEventListener('sprout:open-in-editor', handle);
  }, [openFile]);
}
