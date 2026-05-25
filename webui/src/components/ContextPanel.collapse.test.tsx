// @ts-nocheck

import { act, createElement, createRef } from 'react';
import { createRoot } from 'react-dom/client';

vi.mock('./TodoPanel', () => ({ default: (props) => props.children }));
vi.mock('./RevisionListPanel', () => ({ default: (props) => props.children }));
vi.mock('../services/api', () => ({
  // No longer used by ContextPanel — kept for any transitive imports
}));
vi.mock('../contexts/NotificationContext', () => ({
  NotificationProvider: ({ children }) => children,
  useNotifications: () => ({ addNotification: () => {} }),
}));

import ContextPanel from './ContextPanel';

const MINIMAL_CHAT_PROPS = {
  context: 'chat',
  toolExecutions: [],
  fileEdits: [],
  logs: [],
  subagentActivities: [],
  delegateActivities: [],
  currentTodos: [],
  messages: [],
  isProcessing: false,
  lastError: null,
  queryProgress: null,
  onLoadRevisionHistory: vi.fn().mockResolvedValue({ revisions: [] }),
  onLoadSessions: vi.fn().mockResolvedValue({ sessions: [], current_session_id: '' }),
  onRestoreSession: vi.fn().mockResolvedValue({ messages: [] }),
  onLoadRevisionDetails: vi.fn().mockResolvedValue({ revision: { files: [] } }),
};

function makeChatProps(overrides: Record<string, unknown> = {}) {
  return { ...MINIMAL_CHAT_PROPS, ...overrides };
}

let container: HTMLDivElement;
let root: any;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
  window.localStorage.setItem('sprout.contextPanel.collapsed', '0');
  window.localStorage.setItem('sprout.contextPanel.tab.chat', 'subagents');
  Object.defineProperty(window, 'innerWidth', { writable: true, configurable: true, value: 1280 });
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

async function flushPromises() {
  await act(async () => {
    await Promise.resolve();
  });
}

async function renderPanel(props: Record<string, unknown>, ref?: React.RefObject<unknown>) {
  await act(async () => {
    root.render(createElement(ContextPanel, { ...props, ref }));
  });
  await flushPromises();
}

describe('ContextPanel desktop collapse behavior', () => {
  it('collapses the desktop panel (showing rail) and reports the new state', async () => {
    const onCollapsedChange = vi.fn();
    const panelRef = createRef<any>();

    await renderPanel(makeChatProps({ onCollapsedChange }), panelRef);

    expect(container.querySelector('.context-panel')).not.toBeNull();
    expect(container.querySelector('.context-panel')?.classList.contains('collapsed')).toBe(false);
    expect(onCollapsedChange).toHaveBeenLastCalledWith(false);

    act(() => {
      panelRef.current.closePanel();
    });
    await flushPromises();

    // Collapsed panel should still be mounted with the rail visible
    expect(container.querySelector('.context-panel')).not.toBeNull();
    expect(container.querySelector('.context-panel')?.classList.contains('collapsed')).toBe(true);
    expect(container.querySelector('.context-panel-resizer')).toBeNull();
    expect(onCollapsedChange).toHaveBeenLastCalledWith(true);
  });
});
