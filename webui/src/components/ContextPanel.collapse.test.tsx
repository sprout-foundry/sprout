// @ts-nocheck

import { act, createElement, createRef } from 'react';
import { createRoot } from 'react-dom/client';

jest.mock('./TodoPanel', () => () => <div data-testid="todo-panel" />);
jest.mock('./RevisionListPanel', () => () => <div data-testid="revision-panel" />);
jest.mock('../services/api', () => {
  const mockApi = {
    getChangelog: jest.fn().mockResolvedValue({ revisions: [] }),
    getSessions: jest.fn().mockResolvedValue({ sessions: [], current_session_id: '' }),
    getRevisionDetails: jest.fn().mockResolvedValue({ revision: { files: [] } }),
    restoreSession: jest.fn().mockResolvedValue({ messages: [] }),
  };
  return {
    ApiService: {
      getInstance: jest.fn(() => mockApi),
    },
  };
});
jest.mock('../contexts/NotificationContext', () => {
  const noop = () => {};
  return Object.assign(function NotificationProviderMock({ children }) { return children; }, {
    useNotifications: () => ({ addNotification: noop }),
  });
});

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
  jest.clearAllMocks();
  window.localStorage.setItem('ledit.contextPanel.collapsed', '0');
  window.localStorage.setItem('ledit.contextPanel.tab.chat', 'subagents');
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
  const ContextPanel = require('./ContextPanel').default;
  await act(async () => {
    root.render(createElement(ContextPanel, { ...props, ref }));
  });
  await flushPromises();
}

describe('ContextPanel desktop collapse behavior', () => {
  it('collapses the desktop panel (showing rail) and reports the new state', async () => {
    const onCollapsedChange = jest.fn();
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