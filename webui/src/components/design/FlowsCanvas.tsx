/**
 * Flows tab body — the flow canvas (SP-140-3 §3b).
 *
 * React Flow over the item-3.4 dagre layout: nodes carry wireframe SVG imagery
 * (object URLs minted from the file text `readAsset` returns) or a labeled box
 * when the flow has no wireframe, edges render with their `|label|` text, and
 * the viewport pans/zooms with `fitView`. Node selection and edge selection
 * surface in the detail pane through the shared tab props.
 *
 * The canvas owns *derived* state only (SP-140 invariant 2): positions come
 * from `design/layout.ts`, reusing the `design/flows/<name>.layout.json`
 * sidecar only when it covers the flow and its `derivedFrom` hash still
 * matches the `.mmd` (`resolveFlowLayout`) — a drifted hash regenerates the
 * layout on load. A drag reports the repositioned sidecar through
 * `onLayoutPersist`; the write itself belongs to `FlowsCanvasContainer`
 * (`designApi.writeLayout`, item 3.6), so this component stays a pure function
 * of its props and never writes a `.mmd` (SP-140-3 §3b round-trip rule).
 *
 * Data arrives through props (`flows`, `wireframes`, `assets`, `sidecar`, …) so
 * the component is a pure function of workspace state and stays unit-testable
 * without a network: `FlowsCanvasContainer` below is the thin adapter that
 * resolves assets, flow text, sidecar, and layout hint for the active flow.
 */

import {
  Background,
  BackgroundVariant,
  Controls,
  MarkerType,
  ReactFlow,
  useEdgesState,
  useNodesState,
  type Edge,
  type NodeTypes,
  type OnNodeDrag,
  type OnNodesChange,
  type Position,
  type ReactFlowInstance,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { parseFlowGraph } from '../../design/flowText';
import type { FlowGraph, FlowPoint } from '../../design/layout';
import { resolveFlowLayout } from '../../design/sidecar';
import type { DesignAssetEntry, DesignLayoutSidecar } from '../../services/api/types';
import type { DesignTabProps } from './DesignTabProps';
import {
  boxDimensionsForLabel,
  canvasOrientation,
  draggedPosition,
  handleIds,
  matchWireframeAssets,
  nodeDimensionsFor,
  parkingSpot,
} from './flowCanvasModel';
import { applyDragPreview, clearDragPreview, dragPreviewOf, resetNodeTransform } from './flowDragPreview';
import FlowCanvasNodeView, { handleIdFor, type FlowCanvasNode, type FlowHandleSide } from './FlowsCanvasNode';
import './DesignView.css';

/** One flow available to the canvas: its `.mmd` path and source text. */
export interface FlowCanvasFlow {
  path: string;
  text: string;
  /** Optional display name; defaults to the `.mmd` file's stem. */
  name?: string;
}

/** Where a stale layout or an unknown node lands: one column past the flow. */
const PARKING_FALLBACK: FlowPoint = { x: 0, y: 0 };

/** No nodes are mid-drag when nothing is being dragged. */
const NO_DRAGGING_NODES: ReadonlySet<string> = new Set<string>();

/**
 * Stable identities for the optional props' defaults. A `= []`/`= {}` literal
 * in the parameter list is a fresh object on every render, which would make the
 * derived layout memos (and the node-sync effect keyed on them) churn every
 * render — an infinite React Flow update loop. Shared frozen values keep the
 * "no props" case referentially stable.
 */
const EMPTY_FLOWS: FlowCanvasFlow[] = [];
export const EMPTY_WIREFRAMES: DesignAssetEntry[] = [];
export const EMPTY_ASSETS: (DesignAssetEntry | FlowCanvasFlow)[] = [];
const EMPTY_TEXT: Record<string, string> = {};

export interface FlowsCanvasProps extends DesignTabProps {
  /** Flow sources the canvas can draw, in rail order. */
  flows?: FlowCanvasFlow[];
  /** Flow selected in the rail / by default; defaults to the first flow. */
  activeFlowPath?: string | null;
  /** Wireframe assets, matched to node labels for node imagery. */
  wireframes?: DesignAssetEntry[];
  /** Raw SVG text keyed by wireframe asset path (object-URL source). */
  wireframeText?: Record<string, string>;
  /** Persisted positions for the active flow, when a sidecar was readable. */
  sidecar?: DesignLayoutSidecar | null;
  /** `design_render` orientation hint for the active flow, when known. */
  layoutHint?: string | null;
  /** Fired with the asset to inspect in the detail pane. */
  onSelectAsset?: (path: string) => void;
  /**
   * Fired once with the first rendered layout (fresh sidecar + positions) when
   * the layout had to be re-derived. Read-only notification: persistence runs
   * through `onLayoutPersist` on the container.
   */
  onLayoutChange?: (sidecar: DesignLayoutSidecar) => void;
  /**
   * Fired when the user finishes dragging a node, with the repositioned
   * sidecar (`design/flows/<name>.layout.json`, item 3.6).
   */
  onLayoutPersist?: (sidecar: DesignLayoutSidecar) => void;
  /**
   * Fired with a node/edge id when the pick should also open the flow source
   * in the editor (the §3b "click-through" wiring). Omitted in tests and in
   * hosts without an editor hand-off.
   */
  onOpenSource?: (flowNodeId: string) => void;
  /** Render the built-in chrome (status line, empty states). Off in tests. */
  showChrome?: boolean;
}

/**
 * Which flow the canvas is drawing: the requested path when it is known,
 * else the first flow, else nothing (the empty state).
 */
export function selectCanvasFlow(flows: FlowCanvasFlow[], requested?: string | null): FlowCanvasFlow | null {
  if (!flows || flows.length === 0) return null;
  if (requested) {
    const match = flows.find((flow) => flow.path === requested);
    if (match) return match;
  }
  return flows[0];
}

/** React Flow node/edge ids are independent namespaces; prefixes keep them clear. */
function nodeIdFor(flowNodeId: string): string {
  return `n:${flowNodeId}`;
}

/** Detail-pane label for a node: its label plus the flow it belongs to. */
export function nodeDetailLabel(graph: FlowGraph, id: string): string {
  const node = graph.nodes.find((candidate) => candidate.id === id);
  return node ? `${node.label} (${node.id})` : id;
}

/** Detail-pane label for an edge: operator, label, and both node labels. */
export function edgeDetailLabel(graph: FlowGraph, source: string, target: string, operator: string): string {
  const labelOf = (id: string) => graph.nodes.find((node) => node.id === id)?.label ?? id;
  return `${labelOf(source)} ${operator || '-->'} ${labelOf(target)}`;
}

const NODE_TYPES: NodeTypes = { flowNode: FlowCanvasNodeView };

export default function FlowsCanvas({
  flows = EMPTY_FLOWS,
  activeFlowPath = null,
  wireframes = EMPTY_WIREFRAMES,
  wireframeText = EMPTY_TEXT,
  sidecar = null,
  layoutHint = null,
  onSelectAsset,
  onLayoutChange,
  onLayoutPersist,
  onOpenSource,
  showChrome = true,
}: FlowsCanvasProps) {
  const activeFlow = useMemo(() => selectCanvasFlow(flows, activeFlowPath), [flows, activeFlowPath]);
  const flowText = activeFlow?.text ?? '';
  const graph = useMemo(() => parseFlowGraph(flowText), [flowText]);

  const dimensions = useMemo(
    () => nodeDimensionsFor(graph, wireframes, wireframeText),
    [graph, wireframes, wireframeText],
  );
  const layout = useMemo(
    () => resolveFlowLayout(flowText, graph, sidecar, layoutHint, dimensions),
    [flowText, graph, sidecar, layoutHint, dimensions],
  );
  const wireframeFor = useMemo(() => matchWireframeAssets(graph, wireframes), [graph, wireframes]);
  const flowName = useMemo(
    () => (activeFlow?.name ?? activeFlow?.path.split('/').pop() ?? '').replace(/\.mmd$/, ''),
    [activeFlow],
  );

  const [nodes, setNodes, onNodesChange] = useNodesState<FlowCanvasNode>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);

  /**
   * A node click's hand-off (§3b, item 3.5): fill the detail pane with the
   * flow that owns the node. It deliberately does NOT open the flow source —
   * that is the pane's explicit "Open in editor" affordance (and the edge
   * click's `#L` line hand-off). Opening on select unmounted the canvas, so a
   * click that should show node detail instead navigated the user out of the
   * design surface.
   */
  const selectNode = useCallback(
    (flowNodeId: string) => {
      if (!flowNodeId) return;
      onSelectAsset?.(activeFlow?.path ?? '');
    },
    [onSelectAsset, activeFlow],
  );

  const selectedNodes = useRef<Set<string>>(new Set());
  const draggedPositions = useRef<Record<string, FlowPoint>>({});
  const [draggingNodes, setDraggingNodes] = useState<ReadonlySet<string>>(NO_DRAGGING_NODES);

  const setNodeSelected = useCallback(
    (id: string, selected: boolean) => {
      if (selected) selectedNodes.current.add(id);
      else selectedNodes.current.delete(id);
      setNodes((current) => current.map((node) => (node.id === id ? { ...node, selected } : node)));
    },
    [setNodes],
  );

  // A pointer drag marks the node so its component may serve as its own drag
  // preview (React Flow moves the wrapper element without re-rendering nodes).
  // Selection changes are delegated: React Flow already tracks the drag target,
  // so the canvas must not also write selection on every position change.
  const handleNodesChange = useCallback<OnNodesChange<FlowCanvasNode>>(
    (changes) => {
      let dragging: Set<string> | null = null;
      let settled = false;
      for (const change of changes) {
        if (change.type !== 'position') continue;
        if (change.dragging === true) {
          (dragging ??= new Set(selectedNodes.current)).add(change.id);
          continue;
        }
        // `dragging: false` ends a drag; a settled position change with no
        // `dragging` flag (React Flow's final emit) ends it too.
        if (change.dragging === false || !change.dragging) {
          resetNodeTransform(change.id);
          clearDragPreview(change.id);
          settled = true;
        }
      }
      if (dragging) setDraggingNodes(dragging);
      else if (settled) setDraggingNodes(NO_DRAGGING_NODES);
      onNodesChange(changes);
    },
    [onNodesChange],
  );

  // Positions come from the sidecar/dagre; a hand-dragged node keeps its
  // position until the flow text changes, so a re-render never snaps it back
  // under the user's cursor. A node the sidecar does not cover (the flow gained
  // one since the layout was recorded) is parked clear of the stored box.
  const positions = useMemo(() => {
    const derived: Record<string, FlowPoint> = {};
    for (const node of graph.nodes) {
      derived[node.id] = layout.positions[node.id] ?? parkingSpot(Object.values(derived));
    }
    return derived;
  }, [graph, layout.positions]);

  useEffect(() => {
    if (layout.regenerated) onLayoutChange?.(layout.sidecar);
  }, [layout, onLayoutChange]);

  const edgeSides = useMemo(() => handleIds(canvasOrientation(graph, sidecar)), [graph, sidecar]);

  useEffect(() => {
    const next: FlowCanvasNode[] = graph.nodes.map((node) => {
      const id = nodeIdFor(node.id);
      const box = dimensions[node.id] ?? boxDimensionsForLabel(node.label);
      const handle = edgeSides;
      return {
        id,
        type: 'flowNode',
        position: draggedPositions.current[node.id] ?? positions[node.id] ?? PARKING_FALLBACK,
        width: box.width,
        height: box.height,
        selected: selectedNodes.current.has(id),
        sourcePosition: handle.source as Position,
        targetPosition: handle.target as Position,
        data: {
          label: node.label,
          flowNodeId: node.id,
          wireframePath: wireframeFor[node.id] ?? '',
          wireframeText: wireframeFor[node.id] ? (wireframeText[wireframeFor[node.id]] ?? '') : '',
          group: node.group,
          sourceSide: handle.source as FlowHandleSide,
          targetSide: handle.target as FlowHandleSide,
          dragging: draggingNodes.has(id),
          onNodeClick: (nodeId: string) => selectNode(nodeId),
        },
      };
    });
    setNodes(next);
  }, [graph, dimensions, positions, wireframeFor, wireframeText, selectNode, edgeSides, draggingNodes, setNodes]);

  useEffect(() => {
    setEdges((current) => {
      const selectedIds = new Set(current.filter((edge) => edge.selected).map((edge) => edge.id));
      return graph.edges.map((edge, index) => {
        const id = `${nodeIdFor(edge.source)}->${nodeIdFor(edge.target)}#${index}`;
        return {
          id,
          source: nodeIdFor(edge.source),
          target: nodeIdFor(edge.target),
          // React Flow pairs an edge with its endpoints by handle id; without
          // these it looks for the first `source`/`target` handle and drops the
          // edge when the target node has none (SP-140-3 §3b edge rendering).
          sourceHandle: handleIdFor('source', edgeSides.source as FlowHandleSide),
          targetHandle: handleIdFor('target', edgeSides.target as FlowHandleSide),
          label: edge.label || undefined,
          selected: selectedIds.has(id),
          markerEnd: edge.directed ? { type: MarkerType.ArrowClosed } : undefined,
          // `smoothstep` rather than the default bezier: with a horizontal
          // orientation both endpoints share a `y`, and the bezier then
          // collapses to a zero-height path the browser treats as invisible.
          // The orthogonal curve also matches how mermaid draws `LR`/`TB`.
          type: 'smoothstep',
        } satisfies Edge;
      });
    });
  }, [graph, edgeSides, setEdges]);

  const handleNodeClick = useCallback(
    (_event: React.MouseEvent, node: FlowCanvasNode) => node.data.onNodeClick(node.data.flowNodeId),
    [],
  );

  const handleEdgeClick = useCallback(
    (_event: React.MouseEvent, edge: Edge) => {
      const source = edge.source.replace(/^n:/, '');
      const target = edge.target.replace(/^n:/, '');
      // A multi-edge pair shares source/target; the edge index is the tiebreak.
      const index = Number(edge.id.split('#').pop());
      const operators = graph.edges.filter((candidate) => candidate.source === source && candidate.target === target);
      const operator = operators[Number.isFinite(index) ? index % Math.max(1, operators.length) : 0]?.operator ?? '';
      setEdges((current) => current.map((candidate) => ({ ...candidate, selected: candidate.id === edge.id })));
      onSelectAsset?.(`${activeFlow?.path ?? ''}#${edgeDetailLabel(graph, source, target, operator)}`);
      // The source hand-off carries a single node id so `handleOpenSource` can
      // find a line that literally contains it. The `a->b` pair form matched no
      // source line (edges render as `a --> b`), so the anchor silently fell
      // back to the top of the file.
      onOpenSource?.(source);
    },
    [graph, onSelectAsset, onOpenSource, activeFlow, setEdges],
  );

  // Mid-drag preview: while the set of dragging nodes is non-empty, follow the
  // pointer and translate each dragged node's element. React Flow pans with the
  // *pointer*, not the mouse — a pen or touch drag emits no pointermove at all,
  // and the node simply keeps React Flow's own (linearly interpolated) movement.
  useEffect(() => {
    if (draggingNodes.size === 0 || typeof window === 'undefined') return;
    const onPointerMove = (event: PointerEvent) => {
      for (const id of draggingNodes) applyDragPreview(id, event.clientX, event.clientY);
    };
    const finish = () => {
      for (const id of draggingNodes) {
        resetNodeTransform(id);
        clearDragPreview(id);
      }
      setDraggingNodes(NO_DRAGGING_NODES);
    };
    window.addEventListener('pointermove', onPointerMove);
    window.addEventListener('pointerup', finish);
    window.addEventListener('pointercancel', finish);
    return () => {
      window.removeEventListener('pointermove', onPointerMove);
      window.removeEventListener('pointerup', finish);
      window.removeEventListener('pointercancel', finish);
    };
  }, [draggingNodes]);

  const handleNodeDragStop = useCallback<OnNodeDrag<FlowCanvasNode>>(
    (_event, _node, dragged) => {
      for (const node of dragged) draggedPositions.current[node.data.flowNodeId] = node.position;
      setNodeSelected(dragged[0]?.id ?? '', true);
      if (!onLayoutPersist) return;
      const collapsed: Record<string, FlowPoint> = {};
      for (const node of graph.nodes) {
        const point = draggedPosition(node.id, draggedPositions.current, positions);
        if (point) collapsed[node.id] = point;
      }
      onLayoutPersist({
        nodes: collapsed,
        layoutHint: layout.sidecar.layoutHint,
        derivedFrom: layout.sidecar.derivedFrom,
      });
    },
    [graph, onLayoutPersist, layout.sidecar, positions, setNodeSelected],
  );

  const handleNodeDragStart = useCallback<OnNodeDrag<FlowCanvasNode>>((_event, node) => {
    dragPreviewOf(node.id);
    setDraggingNodes((current) => (current.has(node.id) ? current : new Set(current).add(node.id)));
  }, []);

  const handleInit = useCallback((instance: ReactFlowInstance<FlowCanvasNode, Edge>) => {
    instance.fitView({ maxZoom: 1, padding: 0.15 });
  }, []);

  if (!activeFlow || graph.nodes.length === 0) {
    return (
      <div className="design-tab-body" data-testid="design-flows-canvas" data-flow="">
        <p className="design-tab-placeholder">No flow source to render.</p>
        {activeFlow?.path ? <p className="design-tab-placeholder">{activeFlow.path}</p> : null}
      </div>
    );
  }

  return (
    <div className="design-flows" data-testid="design-flows-canvas" data-flow={flowName}>
      {showChrome ? (
        <p className="design-flows-status" data-testid="design-flows-status">
          {flowName} · {graph.nodes.length} {graph.nodes.length === 1 ? 'node' : 'nodes'} ·{' '}
          {graph.edges.length} {graph.edges.length === 1 ? 'edge' : 'edges'} · {canvasOrientation(graph, sidecar)}
        </p>
      ) : null}
      <div className="design-flows-viewport" data-testid="design-flow-graph">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={NODE_TYPES}
          onNodesChange={handleNodesChange}
          onEdgesChange={onEdgesChange}
          onNodeClick={handleNodeClick}
          onEdgeClick={handleEdgeClick}
          onNodeDragStart={handleNodeDragStart}
          onNodeDragStop={handleNodeDragStop}
          onInit={handleInit}
          fitView
          minZoom={0.1}
        >
          <Background variant={BackgroundVariant.Dots} gap={16} size={1} />
          <Controls showInteractive={false} />
        </ReactFlow>
      </div>
    </div>
  );
}
