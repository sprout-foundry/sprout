/**
 * Custom React Flow node renderer (SP-140-3 §3b).
 *
 * Two node renderings share one component: a node whose flow/wireframe pairing
 * resolved to a wireframe asset draws that SVG's imagery (object URL via
 * `useWireframeImage`), and every other node draws a labeled box. The box is
 * also the fallback when a wireframe fails to load, so a broken asset degrades
 * to a readable node instead of an empty rectangle.
 *
 * The component is presentational only: it renders `data` prepared by
 * `FlowsCanvas` (see `FlowCanvasNodeData` in `flowCanvasModel.ts`) and reports
 * clicks through the `onNodeClick` callback carried in `data`, so a click on the
 * image or the label behaves like a React Flow node click (including flows
 * without wireframes, where the styled box suppresses the built-in label).
 */

import { Handle, Position, type Node, type NodeProps } from '@xyflow/react';
import { memo } from 'react';
import type { FlowPoint } from '../../design/layout';
import { dragPreviewOf } from './flowDragPreview';
import { useWireframeImage } from './useWireframeImage';

/** Node data prepared by `FlowsCanvas` for the canvas node component. */
export interface FlowCanvasNodeData extends Record<string, unknown> {
  /** Node label from the flow source. */
  label: string;
  /** Node id from the flow source. */
  flowNodeId: string;
  /** Wireframe asset path for this node, or '' when the flow has none. */
  wireframePath: string;
  /** Wireframe SVG text (read by `FlowsCanvas`), or '' when unavailable. */
  wireframeText: string;
  /** Enclosing subgraph name, or '' when the node is not grouped. */
  group: string;
  /**
   * True while the user is dragging this node. React Flow writes drag paths
   * straight to the DOM and does not re-render nodes through React, so the
   * component owns its live position while a drag is in flight (see the drag
   * preview below).
   */
  dragging?: boolean;
  /** Fired with the clicked node id — the canvas's detail-pane hand-off. */
  onNodeClick: (id: string) => void;
}

export type FlowCanvasNode = Node<FlowCanvasNodeData, 'flowNode'>;

/** Position in the pane's coordinate space, used only for the drag preview. */
export type NodeDragPreviewProps = Record<string, FlowPoint>;

/** Handles on every side so edges read correctly in any orientation. */
const HANDLES = [
  { id: 'top', position: Position.Top },
  { id: 'right', position: Position.Right },
  { id: 'bottom', position: Position.Bottom },
  { id: 'left', position: Position.Left },
];

function FlowCanvasNodeView({ id, data, selected }: NodeProps<FlowCanvasNode>) {
  const { url, failed, onError } = useWireframeImage(data.wireframeText);
  const showImage = !!url && !failed;
  const className =
    `design-flow-node ${showImage ? 'has-image' : 'is-box'}` +
    `${selected ? ' selected' : ''}${data.dragging ? ' is-dragging' : ''}`;

  return (
    <div
      className={className}
      data-testid={`design-flow-node-${data.flowNodeId}`}
      data-node-id={data.flowNodeId}
      data-imagery={showImage ? 'wireframe' : 'box'}
      data-dragging={data.dragging ? 'true' : 'false'}
      onClick={() => data.onNodeClick(data.flowNodeId)}
      onPointerDownCapture={(event) => {
        // Record where the drag started so the drag preview can translate the
        // node under the cursor (React Flow does not re-render nodes mid-drag).
        if (!data.dragging) return;
        const parent = event.currentTarget.parentElement;
        if (!parent) return;
        const box = parent.getBoundingClientRect();
        const left = event.clientX - box.left;
        const top = event.clientY - box.top;
        event.currentTarget.dataset.dragOriginLeft = String(left);
        event.currentTarget.dataset.dragOriginTop = String(top);
        const preview = dragPreviewOf(id);
        if (preview) {
          preview.left = left;
          preview.top = top;
        }
      }}
    >
      {HANDLES.map((handle) => (
        <Handle
          key={handle.id}
          id={handle.id}
          type="source"
          position={handle.position}
          className="design-flow-handle"
        />
      ))}

      {showImage ? (
        <img
          className="design-flow-node-image"
          src={url}
          alt={data.label}
          data-testid={`design-flow-node-image-${data.flowNodeId}`}
          draggable={false}
          onDragStart={(event) => event.preventDefault()}
          onError={onError}
        />
      ) : null}

      <div className="design-flow-node-caption">
        <span className="design-flow-node-label" title={data.label}>
          {data.label}
        </span>
        {!showImage && data.wireframePath ? (
          <span className="design-flow-node-missing">wireframe unavailable</span>
        ) : null}
        {data.group ? <span className="design-flow-node-group">{data.group}</span> : null}
      </div>
    </div>
  );
}

export default memo(FlowCanvasNodeView);
