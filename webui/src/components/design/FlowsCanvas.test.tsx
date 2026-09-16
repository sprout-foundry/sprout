/**
 * SP-140-3 item 3.5 — Flows canvas.
 *
 * Two layers are covered:
 *
 * 1. `FlowsCanvas` composed against a mocked React Flow. Mocking the library
 *    keeps the assertions honest — node/edge props, imagery vs labeled boxes,
 *    and the selection callbacks are read off the props React Flow receives —
 *    while avoiding React Flow's own ResizeObserver/handle-registry
 *    requirements in jsdom.
 * 2. `FlowsCanvasContainer` against a mocked `designApi`, asserting the flow
 *    text, wireframe text, and sidecar resolve through `readAsset`.
 *
 * The React Flow double is scoped with `vi.hoisted` state so the same mock can
 * be driven from both layers.
 *
 * Assertions use the suite's plain-expect convention (`expect(...)` /
 * `expect.poll`) rather than `@testing-library/jest-dom` matchers.
 */

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createElement, type ComponentType } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { readAsset, writeLayout } from '../../services/api/designApi';
import FlowsCanvas, { edgeDetailLabel, nodeDetailLabel, selectCanvasFlow } from './FlowsCanvas';
import { FlowsCanvasContainer } from './FlowsCanvasContainer';

interface MockNode {
  id: string;
  data: {
    flowNodeId: string;
    label: string;
    wireframePath: string;
    wireframeText: string;
    group: string;
    dragging?: boolean;
    onNodeClick: (id: string) => void;
  };
  position: { x: number; y: number };
  width?: number;
  height?: number;
  selected?: boolean;
  type: string;
}

interface MockEdge {
  id: string;
  source: string;
  target: string;
  label?: string;
  selected?: boolean;
  markerEnd?: unknown;
}

interface MockReactFlowProps {
  nodes: MockNode[];
  edges: MockEdge[];
  nodeTypes?: Record<string, ComponentType<Record<string, unknown>>>;
  fitView?: boolean;
  minZoom?: number;
  onNodeClick?: (event: unknown, node: MockNode) => void;
  onEdgeClick?: (event: unknown, edge: MockEdge) => void;
  onNodeDragStart?: (event: unknown, node: MockNode, nodes: MockNode[]) => void;
  onNodeDragStop?: (event: unknown, node: MockNode, nodes: MockNode[]) => void;
  onNodesChange?: (changes: unknown[]) => void;
}

const rf = vi.hoisted(() => ({
  nodes: [] as unknown[],
  edges: [] as unknown[],
  props: {} as Record<string, unknown>,
}));

vi.mock('@xyflow/react', async () => {
  const actual = await vi.importActual<Record<string, unknown>>('@xyflow/react');
  const MockReactFlow = (props: MockReactFlowProps) => {
    rf.props = props as unknown as Record<string, unknown>;
    rf.nodes = props.nodes ?? [];
    rf.edges = props.edges ?? [];
    return createElement(
      'div',
      { 'data-testid': 'react-flow-mock' },
      rf.nodes.map((node) => {
        const NodeComponent = props.nodeTypes?.[node.type];
        return createElement(
          'div',
          { key: node.id, 'data-testid': `rf-node-${node.id}` },
          NodeComponent ? createElement(NodeComponent, { ...node, selected: !!node.selected }) : null,
        );
      }),
      rf.edges.map((edge) =>
        createElement(
          'button',
          {
            key: edge.id,
            type: 'button',
            'data-testid': `rf-edge-${edge.id}`,
            onClick: (event: MouseEvent) => props.onEdgeClick?.(event, edge),
          },
          typeof edge.label === 'string' ? edge.label : '',
        ),
      ),
      rf.nodes.map((node) =>
        createElement('button', {
          key: `click-${node.id}`,
          type: 'button',
          'data-testid': `rf-node-click-${node.id}`,
          onClick: (event: MouseEvent) => props.onNodeClick?.(event, node),
        }),
      ),
      rf.nodes.map((node) =>
        createElement('button', {
          key: `drag-${node.id}`,
          type: 'button',
          'data-testid': `rf-node-drag-${node.id}`,
          onClick: (event: MouseEvent) => props.onNodeDragStart?.(event, node, [node]),
        }),
      ),
    );
  };
  return {
    ...actual,
    default: MockReactFlow,
    ReactFlow: MockReactFlow,
    // `Handle` reads React Flow's store, which only exists inside a real
    // provider. The node component's handles are pure decoration for these
    // assertions, so they render as inert elements here.
    Handle: (props: { id?: string }) =>
      createElement('span', { 'data-testid': 'rf-handle', 'data-handle-id': props.id ?? '' }),
  };
});

vi.mock('../../services/api/designApi', async () => {
  const actual = await vi.importActual<Record<string, unknown>>('../../services/api/designApi');
  return { ...actual, readAsset: vi.fn(), writeLayout: vi.fn() };
});

vi.mock('../../contexts/SproutAdapterContext', async () => {
  const actual = await vi.importActual<Record<string, unknown>>('../../contexts/SproutAdapterContext');
  return { ...actual, useSproutFetch: () => vi.fn() };
});

const mockedReadAsset = vi.mocked(readAsset);

// `reports` declares its label on its own line: the shared mermaid subset
// parser reads bracket labels only from the statement's leading segment, and a
// `|edge label|` segment is an operator tail (item 3.2/3.4 behaviour).
const FLOW_TEXT = [
  'flowchart LR',
  '  login[Login] --> dash[Dash]',
  '  reports[Reports]',
  '  dash -->|opens| reports',
].join('\n');
const WIREFRAME_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50"><rect width="100" height="50"/></svg>';

const WIREFRAMES = [
  { path: 'wireframes/login.svg', name: 'login.svg', kind: 'wireframe' as const, size: 0, modified: 0 },
  { path: 'wireframes/dash.svg', name: 'dash.svg', kind: 'wireframe' as const, size: 0, modified: 0 },
];

const SVG_TEXT = { 'wireframes/login.svg': WIREFRAME_SVG, 'wireframes/dash.svg': WIREFRAME_SVG };

/** FNV-1a of FLOW_TEXT — the `derivedFrom` a fresh sidecar records. */
const FLOW_HASH = 'ec5a377b';

const baseProps = {
  flows: [{ path: 'flows/app.mmd', name: 'app', text: FLOW_TEXT }],
  wireframes: WIREFRAMES,
  wireframeText: SVG_TEXT,
  showChrome: false,
};

let createObjectURL: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.clearAllMocks();
  rf.nodes = [];
  rf.edges = [];
  rf.props = {};
  createObjectURL = vi.fn(() => 'blob:mock-wireframe');
  URL.createObjectURL = createObjectURL;
  URL.revokeObjectURL = vi.fn();
});

/** The rendered node component for one flow node id. */
function nodeProps(flowNodeId: string): MockNode {
  return (rf.nodes as MockNode[]).find((node) => node.id === `n:${flowNodeId}`) as MockNode;
}

function flowProps(): MockReactFlowProps {
  return rf.props as unknown as MockReactFlowProps;
}

describe('FlowsCanvas rendering', () => {
  it('renders a React Flow graph from parsed nodes and edges with dagre positions', () => {
    render(<FlowsCanvas {...baseProps} />);

    expect(screen.getByTestId('react-flow-mock')).toBeTruthy();
    expect((rf.nodes as MockNode[]).map((node) => node.data.flowNodeId)).toEqual(['login', 'dash', 'reports']);
    expect(rf.edges).toHaveLength(2);
    // dagre laid the graph out: LR puts `login` left of `dash`, rows aligned.
    const login = nodeProps('login');
    const dash = nodeProps('dash');
    expect(login.position.x).toBeLessThan(dash.position.x);
    expect(login.position.y).toBeCloseTo(dash.position.y, 0);
    // The reported geometry is the box the node component renders.
    expect(login.width).toBe(208);
    expect(login.height).toBeGreaterThan(0);
  });

  it('asks React Flow to fit the viewport', () => {
    render(<FlowsCanvas {...baseProps} />);
    expect(flowProps().fitView).toBe(true);
    expect(flowProps().minZoom).toBeLessThan(1);
  });

  it('renders wireframe SVG imagery in matched nodes and labeled boxes elsewhere', () => {
    render(<FlowsCanvas {...baseProps} />);

    expect(screen.getByTestId('design-flow-node-image-login')).toBeTruthy();
    expect(screen.getByTestId('design-flow-node-image-dash')).toBeTruthy();
    // `reports` has no matching design/wireframes asset.
    expect(screen.queryByTestId('design-flow-node-image-reports')).toBeNull();
    expect(screen.getByTestId('design-flow-node-reports').getAttribute('data-imagery')).toBe('box');
    expect(screen.getByTestId('design-flow-node-login').getAttribute('data-imagery')).toBe('wireframe');
    expect(createObjectURL).toHaveBeenCalledTimes(2);
    expect(createObjectURL.mock.calls[0][0]).toBeInstanceOf(Blob);
  });

  it('renders labeled boxes for every node when the flow has no wireframes', () => {
    render(<FlowsCanvas {...baseProps} wireframes={[]} wireframeText={{}} />);

    expect(screen.queryByTestId('design-flow-node-image-login')).toBeNull();
    for (const id of ['login', 'dash', 'reports']) {
      expect(screen.getByTestId(`design-flow-node-${id}`).getAttribute('data-imagery')).toBe('box');
    }
    expect(screen.queryAllByTestId(/^design-flow-node-image-/)).toHaveLength(0);
    expect(createObjectURL).not.toHaveBeenCalled();
  });

  it('keeps the wireframe label visible alongside the imagery', () => {
    render(<FlowsCanvas {...baseProps} />);
    expect(screen.getByText('Login')).toBeTruthy();
    expect(screen.getByText('Reports')).toBeTruthy();
  });

  it('renders edge labels', () => {
    render(<FlowsCanvas {...baseProps} />);
    expect((rf.edges as MockEdge[]).map((edge) => edge.label)).toEqual([undefined, 'opens']);
    expect(screen.getByTestId('rf-edge-n:dash->n:reports#1').textContent).toContain('opens');
  });

  it('marks directed edges with an arrow marker', () => {
    render(<FlowsCanvas {...baseProps} />);
    expect((rf.edges as MockEdge[]).every((edge) => !!edge.markerEnd)).toBe(true);
  });

  it('routes every edge between the handles `dagre` laid the nodes out along', () => {
    render(<FlowsCanvas {...baseProps} />);
    const login = nodeProps('login');
    const dash = nodeProps('dash');
    // LR: ranks are horizontal, so edges leave right and enter left.
    expect(login.sourcePosition ?? (login as unknown as { sourcePosition?: string }).sourcePosition).toBe('right');
    expect((dash as unknown as { targetPosition?: string }).targetPosition).toBe('left');
  });
});

describe('FlowsCanvas selection', () => {
  it('fires the detail-pane callback on node select', () => {
    const onSelectAsset = vi.fn();
    render(<FlowsCanvas {...baseProps} onSelectAsset={onSelectAsset} />);

    fireEvent.click(screen.getByTestId('rf-node-click-n:login'));
    expect(onSelectAsset).toHaveBeenCalledWith('flows/app.mmd');
  });

  it('fires the click-through callback with the selected node id', () => {
    const onOpenSource = vi.fn();
    render(<FlowsCanvas {...baseProps} onOpenSource={onOpenSource} />);

    fireEvent.click(screen.getByTestId('rf-node-click-n:dash'));
    expect(onOpenSource).toHaveBeenCalledWith('dash');
  });

  it('fires the detail-pane callback on edge select with the edge description', () => {
    const onSelectAsset = vi.fn();
    render(<FlowsCanvas {...baseProps} onSelectAsset={onSelectAsset} />);

    fireEvent.click(screen.getByTestId('rf-edge-n:dash->n:reports#1'));
    expect(onSelectAsset).toHaveBeenCalledWith('flows/app.mmd#Dash --> Reports');
  });

  it('fires the click-through callback with the edge endpoint ids', () => {
    const onOpenSource = vi.fn();
    render(<FlowsCanvas {...baseProps} onOpenSource={onOpenSource} />);

    fireEvent.click(screen.getByTestId('rf-edge-n:dash->n:reports#1'));
    expect(onOpenSource).toHaveBeenCalledWith('dash->reports');
  });

  it('fires on image click for wireframe nodes, like a node click', () => {
    const onSelectAsset = vi.fn();
    render(<FlowsCanvas {...baseProps} onSelectAsset={onSelectAsset} />);

    fireEvent.click(screen.getByTestId('design-flow-node-image-login'));
    expect(onSelectAsset).toHaveBeenCalledWith('flows/app.mmd');
  });

  it('marks the selected edge so the detail pane and canvas agree', () => {
    const onSelectAsset = vi.fn();
    render(<FlowsCanvas {...baseProps} onSelectAsset={onSelectAsset} />);

    fireEvent.click(screen.getByTestId('rf-edge-n:dash->n:reports#1'));
    const selected = (rf.edges as MockEdge[]).find((edge) => edge.id === 'n:dash->n:reports#1');
    expect(selected?.selected).toBe(true);
    expect(onSelectAsset.mock.calls[0][0]).toContain('#');
  });
});

describe('FlowsCanvas empty and layout seams', () => {
  it('renders the empty state without a flow', () => {
    render(<FlowsCanvas {...baseProps} flows={[]} />);
    expect(screen.getByTestId('design-flows-canvas').getAttribute('data-flow')).toBe('');
    expect(screen.getByText('No flow source to render.')).toBeTruthy();
  });

  it('reuses sidecar positions when they are fresh', () => {
    render(
      <FlowsCanvas
        {...baseProps}
        sidecar={{
          nodes: { login: { x: 5, y: 7 }, dash: { x: 6, y: 8 }, reports: { x: 9, y: 9 } },
          layoutHint: '',
          derivedFrom: FLOW_HASH,
        }}
      />,
    );
    expect(nodeProps('login').position).toEqual({ x: 5, y: 7 });
    expect(nodeProps('dash').position).toEqual({ x: 6, y: 8 });
  });

  it('falls back to derived positions for nodes a partial sidecar does not cover', () => {
    render(
      <FlowsCanvas
        {...baseProps}
        sidecar={{ nodes: { login: { x: 5, y: 7 } }, layoutHint: '', derivedFrom: FLOW_HASH }}
      />,
    );
    expect(nodeProps('login').position).not.toEqual({ x: 5, y: 7 });
    expect(nodeProps('dash').position.x).toBeGreaterThan(nodeProps('login').position.x);
  });

  it('reports a regenerated layout so item 3.6 can persist a stale sidecar', () => {
    const onLayoutChange = vi.fn();
    render(<FlowsCanvas {...baseProps} sidecar={null} onLayoutChange={onLayoutChange} />);
    expect(onLayoutChange).toHaveBeenCalledTimes(1);
    const [sidecar] = onLayoutChange.mock.calls[0];
    expect(sidecar.nodes).toMatchObject({ login: expect.any(Object), dash: expect.any(Object) });
    expect(sidecar.derivedFrom).toBeTruthy();
    // The canvas holds derived state only — it never writes the sidecar itself.
    expect(writeLayout).not.toHaveBeenCalled();
  });

  it('reports dragged positions through the persistence seam', () => {
    const onLayoutPersist = vi.fn();
    render(<FlowsCanvas {...baseProps} onLayoutPersist={onLayoutPersist} />);

    const dragged = { ...nodeProps('login'), position: { x: 400, y: 300 } };
    act(() => flowProps().onNodeDragStop?.(new MouseEvent('mouseup'), dragged, [dragged]));

    expect(onLayoutPersist).toHaveBeenCalledWith(
      expect.objectContaining({ nodes: expect.objectContaining({ login: { x: 400, y: 300 } }) }),
    );
  });

  it('keeps a dragged position when React Flow reports a settled position change', () => {
    const onLayoutPersist = vi.fn();
    render(<FlowsCanvas {...baseProps} onLayoutPersist={onLayoutPersist} />);

    const dragged = { ...nodeProps('login'), position: { x: 120, y: 90 } };
    act(() => flowProps().onNodeDragStop?.(new MouseEvent('mouseup'), dragged, [dragged]));
    // A later (settled) position change must not snap the node back to its
    // derived layout position.
    act(() => flowProps().onNodesChange?.([{ id: 'n:login', type: 'position', position: { x: 120, y: 90 } }]));

    expect(nodeProps('login').position).toEqual({ x: 120, y: 90 });
  });

  it('falls back to a labeled box when a wireframe fails to load', () => {
    render(<FlowsCanvas {...baseProps} />);
    fireEvent.error(screen.getByTestId('design-flow-node-image-login'));
    expect(screen.queryByTestId('design-flow-node-image-login')).toBeNull();
    expect(screen.getByTestId('design-flow-node-login').getAttribute('data-imagery')).toBe('box');
  });
});

describe('FlowsCanvas drag preview', () => {
  it('marks the node as dragging and arms its mid-drag preview on drag start', () => {
    render(<FlowsCanvas {...baseProps} />);
    const node = nodeProps('login');

    expect(node.data.dragging).toBe(false);
    fireEvent.click(screen.getByTestId('rf-node-drag-n:login'));
    expect(nodeProps('login').data.dragging).toBe(true);
    expect(screen.getByTestId('design-flow-node-login').getAttribute('data-dragging')).toBe('true');
  });

  it('clears the dragging mark when React Flow reports the drag settled', () => {
    render(<FlowsCanvas {...baseProps} />);
    const node = nodeProps('login');

    fireEvent.click(screen.getByTestId('rf-node-drag-n:login'));
    expect(nodeProps('login').data.dragging).toBe(true);

    act(() => flowProps().onNodesChange?.([{ id: 'n:login', type: 'position', position: node.position }]));
    expect(nodeProps('login').data.dragging).toBe(false);
    expect(screen.getByTestId('design-flow-node-login').getAttribute('data-dragging')).toBe('false');
  });
});

describe('FlowsCanvas helpers', () => {
  const graph = {
    direction: 'LR',
    groups: [],
    nodes: [{ id: 'login', label: 'Login', group: '' }],
    edges: [],
  };

  it('selects the requested flow, else the first', () => {
    const flows = [
      { path: 'a.mmd', text: '' },
      { path: 'b.mmd', text: '' },
    ];
    expect(selectCanvasFlow(flows, 'b.mmd')?.path).toBe('b.mmd');
    expect(selectCanvasFlow(flows, 'missing.mmd')?.path).toBe('a.mmd');
    expect(selectCanvasFlow([], 'a.mmd')).toBeNull();
  });

  it('describes nodes and edges for the detail pane', () => {
    expect(nodeDetailLabel(graph, 'login')).toBe('Login (login)');
    expect(edgeDetailLabel(graph, 'login', 'dash', '-->')).toBe('Login --> dash');
  });
});

describe('FlowsCanvasContainer wiring', () => {
  const flows = [{ path: 'flows/app.mmd', name: 'app.mmd', kind: 'flow' as const, size: 0, modified: 0 }];

  it('resolves flow text, wireframe text, and sidecar through readAsset', async () => {
    mockedReadAsset.mockImplementation(async (_fetchFn, path) => {
      if (path === 'flows/app.mmd') return FLOW_TEXT;
      if (path === 'wireframes/login.svg') return WIREFRAME_SVG;
      if (path === 'flows/app.layout.json') {
        return JSON.stringify({
          nodes: { login: { x: 5, y: 7 }, dash: { x: 6, y: 8 }, reports: { x: 9, y: 9 } },
          layoutHint: 'left-right',
          derivedFrom: FLOW_HASH,
        });
      }
      return '';
    });

    render(
      <FlowsCanvasContainer
        flows={flows}
        wireframes={[WIREFRAMES[0]]}
        layouts={[{ path: 'flows/app.layout.json', name: 'app.layout.json', kind: 'layout', size: 0, modified: 0 }]}
      />,
    );

    await waitFor(() => expect((rf.nodes as MockNode[]).length).toBeGreaterThan(0));
    // The stored position wins over the derived one.
    expect(nodeProps('login').position).toEqual({ x: 5, y: 7 });
    expect(screen.getByTestId('design-flow-node-image-login')).toBeTruthy();
    expect(mockedReadAsset.mock.calls.map((call) => call[1])).toEqual(
      expect.arrayContaining(['flows/app.mmd', 'wireframes/login.svg', 'flows/app.layout.json']),
    );
  });

  it('persists a dragged layout through onPersistLayout with the flow name', async () => {
    mockedReadAsset.mockImplementation(async (_fetchFn, path) => (path === 'flows/app.mmd' ? FLOW_TEXT : ''));

    const onPersistLayout = vi.fn();
    render(<FlowsCanvasContainer flows={flows} onPersistLayout={onPersistLayout} />);

    await waitFor(() => expect((rf.nodes as MockNode[]).length).toBeGreaterThan(0));
    const dragged = { ...nodeProps('login'), position: { x: 11, y: 12 } };
    act(() => flowProps().onNodeDragStop?.(new MouseEvent('mouseup'), dragged, [dragged]));

    expect(onPersistLayout).toHaveBeenCalledWith('app', expect.objectContaining({ nodes: expect.any(Object) }));
  });

  it('opens the flow source at the picked statement line through onOpenFile', async () => {
    mockedReadAsset.mockImplementation(async (_fetchFn, path) => (path === 'flows/app.mmd' ? FLOW_TEXT : ''));

    const onOpenFile = vi.fn();
    render(<FlowsCanvasContainer flows={flows} onOpenFile={onOpenFile} />);

    await waitFor(() => expect((rf.nodes as MockNode[]).length).toBeGreaterThan(0));
    // `login` is declared on the flow's second line.
    fireEvent.click(screen.getByTestId('rf-node-click-n:login'));
    expect(onOpenFile).toHaveBeenCalledWith('flows/app.mmd#L2');
  });

  it('accepts pre-resolved flow text without re-reading it', async () => {
    mockedReadAsset.mockImplementation(async () => '');

    render(
      <FlowsCanvasContainer flows={flows} resolvedFlows={[{ path: 'flows/app.mmd', name: 'app', text: FLOW_TEXT }]} />,
    );

    await waitFor(() => expect((rf.nodes as MockNode[]).length).toBeGreaterThan(0));
    expect(nodeProps('login').data.label).toBe('Login');
  });
});
