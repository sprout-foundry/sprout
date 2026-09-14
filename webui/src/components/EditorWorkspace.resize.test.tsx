// @ts-nocheck

import { act, createElement, Fragment, useEffect, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { EditorManagerProvider, useEditorManager } from '../contexts/EditorManagerContext';
import EditorWorkspace from './EditorWorkspace';

vi.mock('../contexts/NotificationContext', () => ({
  NotificationProvider: ({ children }) => children,
  useNotifications: () => ({ addNotification: () => {} }),
}));
vi.mock('../contexts/SproutAdapterContext', () => ({
  SproutAdapterProvider: ({ children }) => children,
  useSproutAdapter: () => ({ clientFetch: vi.fn() }),
  useSproutFetch: () => vi.fn(),
}));

// WorkspacePane → EditorPane pulls the full CodeMirror editor — too heavy for
// this layout/drag test and irrelevant to it.
vi.mock('./WorkspacePane', () => ({
  default: () => createElement('div', { className: 'mock-workspace-pane' }),
}));

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  root = null;
});

const chatProps = { messages: [] };
const reviewProps = {
  review: null,
  reviewError: null,
  reviewFixResult: null,
  reviewFixLogs: [],
  reviewFixSessionID: null,
  isReviewLoading: false,
  isReviewFixing: false,
  onFixFromReview: () => {},
};
const diffState = {
  activeDiffPath: null,
  activeDiff: null,
  diffMode: 'combined',
  isDiffLoading: false,
  diffError: null,
  onDiffModeChange: () => {},
};

function Harness() {
  const { splitPane, paneSizes, panes, updatePaneSize } = useEditorManager();
  const [splitDone, setSplitDone] = useState(false);
  useEffect(() => {
    if (!splitDone && panes.length === 1) {
      splitPane(panes[0].id, 'vertical');
      setSplitDone(true);
    }
  }, [splitDone, splitPane, panes]);
  return createElement(
    Fragment,
    null,
    createElement('div', { 'data-testid': 'sizes' }, JSON.stringify(paneSizes)),
    createElement('div', { 'data-testid': 'update' }, String(typeof updatePaneSize)),
    createElement(EditorWorkspace, {
      currentView: 'chat',
      chatProps,
      reviewProps,
      diffState,
      handleOutlineNavigateToSymbol: () => {},
    }),
  );
}

async function settle(n = 4) {
  await act(async () => {
    for (let i = 0; i < n; i++) await new Promise((r) => setTimeout(r, 10));
  });
}

describe('EditorWorkspace split resize', () => {
  it('dragging the flex-layout resize handle updates pane sizes', async () => {
    act(() => {
      root.render(createElement(EditorManagerProvider, null, createElement(Harness)));
    });
    await settle();

    const handle = container.querySelector('.panes-container > .resize-handle');
    expect(handle).not.toBeNull();

    const sizesBefore = JSON.parse(container.querySelector('[data-testid="sizes"]').textContent || '{}');
    expect(Object.keys(sizesBefore).length).toBe(2);

    const panesContainer = container.querySelector('.panes-container');
    panesContainer.getBoundingClientRect = () => ({
      width: 1000,
      height: 800,
      top: 0,
      left: 0,
      right: 1000,
      bottom: 800,
      x: 0,
      y: 0,
      toJSON: () => {},
    });

    act(() => {
      handle.dispatchEvent(
        new PointerEvent('pointerdown', { bubbles: true, cancelable: true, clientX: 500, clientY: 100, pointerId: 1 }),
      );
    });
    await act(async () => {
      window.dispatchEvent(
        new PointerEvent('pointermove', { bubbles: true, clientX: 600, clientY: 100, pointerId: 1 }),
      );
      await new Promise((r) => setTimeout(r, 5));
    });
    await act(async () => {
      window.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, pointerId: 1 }));
      for (let i = 0; i < 2; i++) await new Promise((r) => setTimeout(r, 10));
    });

    const sizesAfter = JSON.parse(container.querySelector('[data-testid="sizes"]').textContent || '{}');
    const firstId = Object.keys(sizesAfter)[0];
    expect(sizesAfter[firstId]).toBeCloseTo(60, 0);
  });
});
