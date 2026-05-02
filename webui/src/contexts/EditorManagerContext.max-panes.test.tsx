// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { EditorManagerProvider, useEditorManager, MAX_PANES, MIN_PANE_WIDTH_PERCENT } from './EditorManagerContext';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

jest.mock('../services/fileAccess', () => ({
  writeFileWithConsent: jest.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve({ message: 'File saved successfully' }),
  }),
}));

jest.mock('./SproutAdapterContext', () => ({
  ...jest.requireActual('./SproutAdapterContext'),
  useSproutFetch: () => jest.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve({ message: 'File saved successfully' }),
  }),
}));

jest.mock('../services/formatter', () => ({
  formatWithPrettier: jest.fn().mockResolvedValue(undefined),
  getPrettierParser: jest.fn().mockReturnValue(null),
}));

jest.mock('./NotificationContext', () => {
  const noop = () => {};
  return Object.assign(function NotificationProviderMock({ children }) { return children; }, {
    useNotifications: () => ({ addNotification: noop }),
  });
});

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;
let latestContext: any;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  jest.clearAllMocks();
  latestContext = undefined;
  localStorage.setItem('sprout-welcome-dismissed', 'true');
  localStorage.removeItem('sprout.editor.layoutState');
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

function TestConsumer() {
  const ctx = useEditorManager();
  latestContext = ctx;
  return null;
}

function renderProvider() {
  act(() => {
    root.render(createElement(EditorManagerProvider, null, createElement(TestConsumer)));
  });
}

const actAndUpdate = async (fn: () => void) => {
  act(() => {
    fn();
  });
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
};

const ctx = () => latestContext;

const makeFile = (path: string, name: string) => ({
  path,
  name,
  isDir: false,
  size: 0,
  modified: 0,
  ext: `.${name.split('.').pop() || 'txt'}`,
});

// ---------------------------------------------------------------------------
// Tests: MAX_PANES constant and pane limit
// ---------------------------------------------------------------------------

describe('MAX_PANES and pane limit', () => {
  it('MAX_PANES constant is exported and equals 6', () => {
    expect(MAX_PANES).toBe(6);
  });

  it('MIN_PANE_WIDTH_PERCENT constant is exported and equals 8', () => {
    expect(MIN_PANE_WIDTH_PERCENT).toBe(8);
  });

  it('can create up to 6 panes via splitPane', async () => {
    renderProvider();

    const initialPaneId = ctx().activePaneId;

    // Create 2nd pane
    let pane2Id: string;
    await actAndUpdate(() => {
      pane2Id = ctx().splitPane(initialPaneId, 'vertical');
    });
    expect(pane2Id).toBeTruthy();
    expect(ctx().panes.length).toBe(2);

    // Create 3rd pane
    let pane3Id: string;
    await actAndUpdate(() => {
      pane3Id = ctx().splitPane(initialPaneId, 'vertical');
    });
    expect(pane3Id).toBeTruthy();
    expect(ctx().panes.length).toBe(3);

    // Create 4th pane
    let pane4Id: string;
    await actAndUpdate(() => {
      pane4Id = ctx().splitPane(initialPaneId, 'horizontal');
    });
    expect(pane4Id).toBeTruthy();
    expect(ctx().panes.length).toBe(4);

    // Create 5th pane
    let pane5Id: string;
    await actAndUpdate(() => {
      pane5Id = ctx().splitPane(initialPaneId, 'horizontal');
    });
    expect(pane5Id).toBeTruthy();
    expect(ctx().panes.length).toBe(5);

    // Create 6th pane
    let pane6Id: string;
    await actAndUpdate(() => {
      pane6Id = ctx().splitPane(initialPaneId, 'vertical');
    });
    expect(pane6Id).toBeTruthy();
    expect(ctx().panes.length).toBe(6);

    // Cannot create 7th pane
    let pane7Id: string;
    await actAndUpdate(() => {
      pane7Id = ctx().splitPane(initialPaneId, 'vertical');
    });
    expect(pane7Id).toBeNull();
    expect(ctx().panes.length).toBe(6);
  });

  it('pane positions are assigned correctly up to senary', async () => {
    renderProvider();

    const expectedPositions = ['primary', 'secondary', 'tertiary', 'quaternary', 'quinary', 'senary'];

    // Split 5 times to get 6 panes
    for (let i = 0; i < 5; i++) {
      await actAndUpdate(() => {
        ctx().splitPane(ctx().activePaneId, 'vertical');
      });
    }

    expect(ctx().panes.length).toBe(6);

    // Verify each pane has the correct position
    ctx().panes.forEach((pane, index) => {
      expect(pane.position).toBe(expectedPositions[index]);
    });
  });
});

// ---------------------------------------------------------------------------
// Tests: Minimum pane width enforcement
// ---------------------------------------------------------------------------

describe('Minimum pane width enforcement', () => {
  it('updatePaneSize enforces MIN_PANE_WIDTH_PERCENT of 8%', async () => {
    renderProvider();

    // Split to create 2 panes
    const paneId1 = ctx().panes[0].id;
    let paneId2: string;
    await actAndUpdate(() => {
      paneId2 = ctx().splitPane(paneId1, 'vertical');
    });

    // Try to resize first pane to 5% (below minimum)
    await actAndUpdate(() => {
      ctx().updatePaneSize(paneId1, 5);
    });

    // Should be clamped to 8%
    expect(ctx().paneSizes[paneId1]).toBe(8);
    expect(ctx().paneSizes[paneId2]).toBe(92);
  });

  it('updatePaneSize enforces maximum size so other panes maintain minimum width', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    let paneId2: string;
    await actAndUpdate(() => {
      paneId2 = ctx().splitPane(paneId1, 'vertical');
    });

    // Try to resize first pane to 95% (leaving 5% for second pane)
    await actAndUpdate(() => {
      ctx().updatePaneSize(paneId1, 95);
    });

    // Should be clamped to 92% (100 - MIN_PANE_WIDTH_PERCENT)
    expect(ctx().paneSizes[paneId1]).toBe(92);
    expect(ctx().paneSizes[paneId2]).toBe(8);
  });

  it('updatePaneSize correctly calculates max size for 3 panes', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    await actAndUpdate(() => {
      ctx().splitPane(paneId1, 'vertical');
      ctx().splitPane(paneId1, 'horizontal');
    });

    expect(ctx().panes.length).toBe(3);

    // Try to resize first pane to 85% (leaving 15% for 2 panes)
    await actAndUpdate(() => {
      ctx().updatePaneSize(paneId1, 85);
    });

    // Should be clamped to 84% (100 - 8% * 2)
    expect(ctx().paneSizes[paneId1]).toBe(84);
  });

  it('updatePaneSize correctly calculates max size for 6 panes', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;

    // Create 6 panes
    for (let i = 0; i < 5; i++) {
      await actAndUpdate(() => {
        ctx().splitPane(ctx().activePaneId, 'vertical');
      });
    }

    expect(ctx().panes.length).toBe(6);

    // Try to resize first pane to 60% (leaving 40% for 5 panes)
    await actAndUpdate(() => {
      ctx().updatePaneSize(paneId1, 60);
    });

    // Should be clamped to 60% (100 - 8% * 5 = 60%)
    expect(ctx().paneSizes[paneId1]).toBe(60);
  });

  it('updatePaneSize allows sizes within valid range', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    let paneId2: string;
    await actAndUpdate(() => {
      paneId2 = ctx().splitPane(paneId1, 'vertical');
    });

    // Set to valid size
    await actAndUpdate(() => {
      ctx().updatePaneSize(paneId1, 50);
    });

    expect(ctx().paneSizes[paneId1]).toBe(50);
    expect(ctx().paneSizes[paneId2]).toBe(50);

    // Set to valid size near lower bound
    await actAndUpdate(() => {
      ctx().updatePaneSize(paneId1, 10);
    });

    expect(ctx().paneSizes[paneId1]).toBe(10);
    expect(ctx().paneSizes[paneId2]).toBe(90);

    // Set to valid size near upper bound
    await actAndUpdate(() => {
      ctx().updatePaneSize(paneId1, 85);
    });

    expect(ctx().paneSizes[paneId1]).toBe(85);
    expect(ctx().paneSizes[paneId2]).toBe(15);
  });
});

// ---------------------------------------------------------------------------
// Tests: getRightmostPane helper with 6 panes
// ---------------------------------------------------------------------------

describe('getRightmostPane helper with 6 panes', () => {
  it('finds the rightmost pane based on position order', async () => {
    renderProvider();

    // Create 6 panes
    for (let i = 0; i < 5; i++) {
      await actAndUpdate(() => {
        ctx().splitPane(ctx().activePaneId, 'vertical');
      });
    }

    const panes = ctx().panes;
    const rightmost = panes.find(p => p.position === 'senary');

    expect(rightmost).toBeDefined();
    expect(rightmost.position).toBe('senary');

    // Verify position order
    const positionOrder: Record<string, number> = {
      primary: 0,
      secondary: 1,
      tertiary: 2,
      quaternary: 3,
      quinary: 4,
      senary: 5,
    };

    panes.forEach(pane => {
      expect(positionOrder[pane.position]).toBeDefined();
    });
  });
});
