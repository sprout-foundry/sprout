import { ViewsLayout, resolveViewsArrangement } from './index';
import type { ChatProps, PreviewPanelProps, ViewsArrangement } from './index';

/**
 * Props for the example: the host-owned chat state, optional preview
 * overrides, and the optional arrangement. Mirrors what an embedding shell
 * would hold in its own state.
 */
export interface ExampleEmbeddingProps {
  /** The chat view's props — the host owns the conversation state. */
  chat: ChatProps;
  /** The preview panel's props; defaults to open with a no-op close. */
  preview?: PreviewPanelProps;
  /**
   * Slots replacing the example's base slots (chat at center, preview as
   * the overlay). Kinds are validated by `resolveViewsArrangement`.
   */
  arrangement?: ViewsArrangement;
}

/**
 * Example embedding.
 *
 * The spec's acceptance criterion: "An example embedding composes chat +
 * preview from exported views." This is that example — a host-application
 * fragment showing the smallest useful composition: the host supplies the
 * chat and preview state through props (where that state comes from is the
 * host's business) and describes WHERE the views go through an arrangement.
 *
 * It imports the views from the views entry point (`./index`) — the same
 * documented surface an embedding shell outside this repository consumes.
 * The circular barrel import is deliberate and safe: nothing here is read
 * at module-init time, only at render time, and `import/no-cycle` is off
 * for this codebase.
 *
 * The base arrangement explicitly empties the left and right slots — the
 * example only supplies chat + preview, so it drops the built-in file tree
 * and changes panel rather than rendering views it has no props for. The
 * caller's `arrangement` then replaces slots wholesale on top of that base
 * (per-slot replace semantics), and `resolveViewsArrangement` validates the
 * result, so the final placement is the embedding's choice.
 */

/** The example's own arrangement: chat at center, preview as the overlay. */
const BASE_ARRANGEMENT: ViewsArrangement = {
  left: [],
  center: ['chat'],
  right: [],
  overlay: ['previewPanel'],
};

const DEFAULT_PREVIEW_PROPS: PreviewPanelProps = { open: true, onClose: () => undefined };

/**
 * A chat + preview shell — the composed example the criterion
 * describes.
 */
export function ExampleEmbedding({ chat, preview, arrangement }: ExampleEmbeddingProps): JSX.Element {
  const resolved = resolveViewsArrangement({ ...BASE_ARRANGEMENT, ...(arrangement ?? {}) });
  return (
    <div data-testid="views-example-embedding">
      <ViewsLayout arrangement={resolved} props={{ chat, previewPanel: preview ?? DEFAULT_PREVIEW_PROPS }} />
    </div>
  );
}

export default ExampleEmbedding;
