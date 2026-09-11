// @ts-nocheck

import { act, createElement, createRef } from 'react';
import { createRoot } from 'react-dom/client';

vi.mock('./TodoPanel', () => ({ default: (props) => props.children }));
vi.mock('./AgentChangesPanel', () => ({ default: () => null }));
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
  currentTodos: [],
  messages: [],
  isProcessing: false,
  lastError: null,
  queryProgress: null,
  onLoadSessions: vi.fn().mockResolvedValue({ sessions: [], current_session_id: '' }),
  onRestoreSession: vi.fn().mockResolvedValue({ messages: [] }),
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
  it('collapses the desktop panel (showing rail) via closePanel', async () => {
    const panelRef = createRef<any>();

    await renderPanel(makeChatProps(), panelRef);

    expect(container.querySelector('.context-panel')).not.toBeNull();
    expect(container.querySelector('.context-panel')?.classList.contains('collapsed')).toBe(false);

    act(() => {
      panelRef.current.closePanel();
    });
    await flushPromises();

    // Collapsed panel should still be mounted with the rail visible
    expect(container.querySelector('.context-panel')).not.toBeNull();
    expect(container.querySelector('.context-panel')?.classList.contains('collapsed')).toBe(true);
    expect(container.querySelector('.context-panel-resizer')).toBeNull();
  });

  it('togglePanel flips collapsed state both ways', async () => {
    const panelRef = createRef<any>();

    await renderPanel(makeChatProps(), panelRef);

    expect(container.querySelector('.context-panel')?.classList.contains('collapsed')).toBe(false);

    act(() => {
      panelRef.current.togglePanel();
    });
    await flushPromises();
    expect(container.querySelector('.context-panel')?.classList.contains('collapsed')).toBe(true);

    act(() => {
      panelRef.current.togglePanel();
    });
    await flushPromises();
    expect(container.querySelector('.context-panel')?.classList.contains('collapsed')).toBe(false);
  });

  it('idle mode: panel stays mounted, rail buttons disabled, collapse button enabled', async () => {
    const panelRef = createRef<any>();

    await renderPanel(makeChatProps({ isIdle: true }), panelRef);

    const panel = container.querySelector('.context-panel');
    expect(panel).not.toBeNull();
    expect(panel?.classList.contains('context-panel-idle')).toBe(true);

    // Tab buttons disabled, collapse button still usable
    const tabButtons = Array.from(container.querySelectorAll('[data-testid="context-panel-tab"]'));
    expect(tabButtons.length).toBeGreaterThan(0);
    for (const btn of tabButtons) {
      expect(btn.disabled).toBe(true);
    }
    const collapseBtn = container.querySelector('[data-testid="context-panel-collapse"]');
    expect(collapseBtn?.disabled).toBe(false);

    // Idle body note replaces tab content
    expect(container.textContent).toContain('Chat context');

    // togglePanel still works while idle (hide the panel in file view)
    act(() => {
      panelRef.current.togglePanel();
    });
    await flushPromises();
    expect(container.querySelector('.context-panel')?.classList.contains('collapsed')).toBe(true);
  });
});
