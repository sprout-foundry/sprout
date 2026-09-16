/**
 * Flows tab body — placeholder for SP-140-3 item 3.5.
 *
 * The real canvas (React Flow over dagre-derived positions, wireframe SVG
 * imagery in nodes, edge labels, drag-to-reposition sidecar writes) lands in
 * item 3.5. This stub exists so the DesignView shell's tab structure is
 * complete and type-checked; item 3.5 replaces this file's body.
 */

import type { DesignTabProps } from './DesignTabProps';

export default function FlowsCanvas(_props: DesignTabProps = {}) {
  return (
    <div className="design-tab-body" data-testid="design-flows-canvas">
      <p className="design-tab-placeholder">Flow canvas — not yet implemented (SP-140-3 item 3.5).</p>
    </div>
  );
}
