// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot } from 'react-dom/client';

vi.mock('./ContextPanel', () => ({
  default: (props) =>
    createElement('aside', {
      'data-testid': 'context-panel',
      'data-idle': props.isIdle ? 'true' : 'false',
      'data-mobile': props.isMobileLayout ? 'true' : 'false',
      'data-tablet': props.isTabletLayout ? 'true' : 'false',
    }),
}));
vi.mock('../services/api', () => ({
  ApiService: { getInstance: () => ({ getSessions: vi.fn(), restoreSession: vi.fn() }) },
}));

import ContextSidebar from './ContextSidebar';

const BASE_PROPS = {
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
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

async function renderSidebar(props: Record<string, unknown>) {
  await act(async () => {
    root.render(createElement(ContextSidebar, props));
  });
}

function panel(): HTMLElement | null {
  return container.querySelector('[data-testid="context-panel"]');
}

describe('ContextSidebar desktop stability', () => {
  it('desktop: panel stays mounted when focus moves chat → file', async () => {
    await renderSidebar({ ...BASE_PROPS, isMobile: false, isTablet: false, showContextSidebar: true, currentView: 'chat' });
    expect(panel()).not.toBeNull();
    expect(panel()?.dataset.idle).toBe('false');

    // Open a plain file buffer → chat no longer active
    await renderSidebar({ ...BASE_PROPS, isMobile: false, isTablet: false, showContextSidebar: false, currentView: 'chat' });
    expect(panel()).not.toBeNull();
    expect(panel()?.dataset.idle).toBe('true');
  });

  it('desktop: costs view also keeps the mounted panel (idle)', async () => {
    await renderSidebar({ ...BASE_PROPS, isMobile: false, isTablet: false, showContextSidebar: true, currentView: 'costs' });
    expect(panel()).not.toBeNull();
    expect(panel()?.dataset.idle).toBe('true');
  });

  it('desktop: same wrapper element across the switch (no remount)', async () => {
    await renderSidebar({ ...BASE_PROPS, isMobile: false, isTablet: false, showContextSidebar: true, currentView: 'chat' });
    const before = panel();
    await renderSidebar({ ...BASE_PROPS, isMobile: false, isTablet: false, showContextSidebar: false, currentView: 'chat' });
    const after = panel();
    // React reconciliation keeps the same DOM node because ContextSidebar
    // renders the same <aside> in both states.
    expect(after).toBe(before);
  });

  it('mobile: panel unmounts when no chat is focused (overlay behavior preserved)', async () => {
    await renderSidebar({ ...BASE_PROPS, isMobile: true, isTablet: false, showContextSidebar: true, currentView: 'chat' });
    expect(panel()).not.toBeNull();
    expect(panel()?.dataset.mobile).toBe('true');

    await renderSidebar({ ...BASE_PROPS, isMobile: true, isTablet: false, showContextSidebar: false, currentView: 'chat' });
    expect(panel()).toBeNull();
  });

  it('tablet: panel unmounts when no chat is focused (overlay behavior preserved)', async () => {
    await renderSidebar({ ...BASE_PROPS, isMobile: false, isTablet: true, showContextSidebar: true, currentView: 'chat' });
    expect(panel()).not.toBeNull();
    expect(panel()?.dataset.tablet).toBe('true');

    await renderSidebar({ ...BASE_PROPS, isMobile: false, isTablet: true, showContextSidebar: false, currentView: 'chat' });
    expect(panel()).toBeNull();
  });
});
