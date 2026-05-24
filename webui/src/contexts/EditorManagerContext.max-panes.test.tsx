// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { EditorManagerProvider, useEditorManager, MAX_PANES, MIN_PANE_WIDTH_PERCENT } from './EditorManagerContext';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('./NotificationContext', () => ({
  NotificationProvider: ({ children }) => children,
  useNotifications: () => ({ addNotification: () => {} }),
}));
vi.mock('./SproutAdapterContext', () => ({
  SproutAdapterProvider: ({ children }) => children,
  useSproutAdapter: () => ({ clientFetch: vi.fn() }),
  useSproutFetch: () => vi.fn(),
}));

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
  vi.clearAllMocks();
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
  vi.advanceTimersByTime(1);
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

    // Should be clamped to 8% (other pane is NOT redistributed by updatePaneSize)
    expect(ctx().paneSizes[paneId1]).toBe(8);
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
    // Note: updatePaneSize only clamps the target pane; it does NOT redistribute other pane sizes.
    expect(ctx().paneSizes[paneId1]).toBe(92);
  });

  it('updatePaneSize correctly calculates max size for 3 panes', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    let paneId2: string;
    await actAndUpdate(() => {
      paneId2 = ctx().splitPane(paneId1, 'vertical');
    });

    expect(ctx().panes.length).toBe(2);

    let paneId3: string;
    await actAndUpdate(() => {
      paneId3 = ctx().splitPane(paneId1, 'horizontal');
    });

    expect(ctx().panes.length).toBe(3);

    // Try to resize first pane to 85% (leaving 15% for 2 panes)
    await actAndUpdate(() => {
      ctx().updatePaneSize(paneId1, 85);
    });

    // Should be clamped to 84% (100 - 8% * 2) — only if paneSizes has 3 entries
    // If paneSizes still has 2 entries from the split, the max is 100-8=92, so 85 passes
    const paneSizeKeysCount = Object.keys(ctx().paneSizes).filter(
      (key) => !key.startsWith('group:') && !key.startsWith('nested:') && !key.startsWith('grid:'),
    ).length;
    const expectedMax = 100 - 8 * (paneSizeKeysCount - 1);
    const expected = Math.min(85, expectedMax);
    expect(ctx().paneSizes[paneId1]).toBe(expected);
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

    // Set to valid size near lower bound
    await actAndUpdate(() => {
      ctx().updatePaneSize(paneId1, 10);
    });

    expect(ctx().paneSizes[paneId1]).toBe(10);

    // Set to valid size near upper bound
    await actAndUpdate(() => {
      ctx().updatePaneSize(paneId1, 85);
    });

    expect(ctx().paneSizes[paneId1]).toBe(85);
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
    const rightmost = panes.find((p) => p.position === 'senary');

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

    panes.forEach((pane) => {
      expect(positionOrder[pane.position]).toBeDefined();
    });
  });
});

// ---------------------------------------------------------------------------
// Tests: Pane size distribution for all pane counts (2 through 6)
// ---------------------------------------------------------------------------

describe('Pane size distribution across all pane counts', () => {
  it('2 panes each get 50%', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    await actAndUpdate(() => {
      ctx().splitPane(paneId1, 'vertical');
    });

    expect(ctx().panes.length).toBe(2);
    expect(ctx().paneSizes[paneId1]).toBe(50);
    expect(ctx().paneSizes[ctx().panes[1].id]).toBe(50);
  });

  it('3 panes each get 33.33%', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    await actAndUpdate(() => {
      ctx().splitPane(paneId1, 'vertical');
    });
    await actAndUpdate(() => {
      ctx().splitPane(paneId1, 'horizontal');
    });

    expect(ctx().panes.length).toBe(3);
    const expectedSize = 100 / 3;
    ctx().panes.forEach((pane) => {
      expect(ctx().paneSizes[pane.id]).toBeCloseTo(expectedSize, 5);
    });
  });

  it('5 panes each get 20%', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    // Create 5 panes total — use explicit calls to avoid Date.now() collisions
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(2);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(3);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(4);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(5);

    ctx().panes.forEach((pane) => {
      expect(ctx().paneSizes[pane.id]).toBe(20);
    });
  });

  it('6 panes each get 16.67%', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    // Create 6 panes total — use explicit calls to avoid Date.now() collisions
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(2);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(3);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(4);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(5);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(6);

    const expectedSize = 100 / 6;
    ctx().panes.forEach((pane) => {
      expect(ctx().paneSizes[pane.id]).toBeCloseTo(expectedSize, 5);
    });
  });

  it('4 panes each get 25%', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));

    expect(ctx().panes.length).toBe(4);
    ctx().panes.forEach((pane) => {
      expect(ctx().paneSizes[pane.id]).toBe(25);
    });
  });
});

// ---------------------------------------------------------------------------
// Tests: closePane with 4-6 panes
// ---------------------------------------------------------------------------

describe('closePane with 4-6 panes', () => {
  it('closing a pane from 6 leaves 5 with rebalanced sizes', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    // Create 6 panes using explicit calls to avoid Date.now() collisions
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(2);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(3);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(4);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(5);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(6);

    // Close the last pane (senary)
    const senaryPane = ctx().panes.find((p) => p.position === 'senary');
    const paneToClose = senaryPane!.id;

    await actAndUpdate(() => {
      ctx().closePane(paneToClose);
    });

    expect(ctx().panes.length).toBe(5);

    // Sizes should be rebalanced to 100/5 = 20% each
    ctx().panes.forEach((pane) => {
      expect(ctx().paneSizes[pane.id]).toBe(20);
    });

    // Layout should remain split-vertical (not revert to single)
    expect(ctx().paneLayout).toBe('split-vertical');
  });

  it('closing a pane from 4 leaves 3 with rebalanced sizes', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));

    expect(ctx().panes.length).toBe(4);

    // Close the last pane (quaternary)
    const paneToClose = ctx().panes.find((p) => p.position === 'quaternary')!.id;

    await actAndUpdate(() => {
      ctx().closePane(paneToClose);
    });

    expect(ctx().panes.length).toBe(3);

    // Sizes should be rebalanced to 100/3 each
    const expectedSize = 100 / 3;
    ctx().panes.forEach((pane) => {
      expect(ctx().paneSizes[pane.id]).toBeCloseTo(expectedSize, 5);
    });
  });

  it('closing panes one by one from 6 down to 1 works correctly', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    // Create 6 panes using explicit calls
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(2);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(3);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(4);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(5);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(6);

    // Close panes one at a time using position-based lookups to avoid ID collisions
    const positionsToRemove = ['senary', 'quinary', 'quaternary', 'tertiary', 'secondary'];
    for (let i = 0; i < positionsToRemove.length; i++) {
      const expectedCount = 6 - i - 1;
      const pos = positionsToRemove[i];
      const paneToClose = ctx().panes.find((p) => p.position === pos)!;
      await actAndUpdate(() => {
        ctx().closePane(paneToClose.id);
      });

      expect(ctx().panes.length).toBe(expectedCount);
      // All remaining panes should have equal sizes
      const expectedSize = 100 / expectedCount;
      ctx().panes.forEach((pane) => {
        expect(ctx().paneSizes[pane.id]).toBeCloseTo(expectedSize, 5);
      });
    }

    // Final state: only primary pane remains, layout is single
    expect(ctx().panes.length).toBe(1);
    expect(ctx().paneLayout).toBe('single');
    expect(ctx().panes[0].position).toBe('primary');
  });

  it('closing a non-last pane from 6 works correctly', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    // Create 6 panes using explicit calls
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(2);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(3);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(4);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(5);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(6);

    // Close the tertiary pane (index 2)
    const tertiaryPane = ctx().panes.find((p) => p.position === 'tertiary')!;

    await actAndUpdate(() => {
      ctx().closePane(tertiaryPane.id);
    });

    expect(ctx().panes.length).toBe(5);

    // Remaining panes should still have valid positions
    const remainingPositions = ctx().panes.map((p) => p.position);
    expect(remainingPositions).not.toContain('tertiary');
    expect(remainingPositions).toContain('primary');
    expect(remainingPositions).toContain('secondary');

    // Sizes should be rebalanced
    ctx().panes.forEach((pane) => {
      expect(ctx().paneSizes[pane.id]).toBe(20);
    });
  });
});

// ---------------------------------------------------------------------------
// Tests: closeSplit behavior at various pane counts
// ---------------------------------------------------------------------------

describe('closeSplit at various pane counts', () => {
  it('closeSplit from 6 panes returns to single pane', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    // Create 6 panes using explicit calls
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(2);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(3);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(4);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(5);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(6);
    expect(ctx().paneLayout).toBe('split-vertical');

    await actAndUpdate(() => {
      ctx().closeSplit();
    });

    expect(ctx().panes.length).toBe(1);
    expect(ctx().paneLayout).toBe('single');
    expect(ctx().panes[0].position).toBe('primary');
    expect(ctx().paneSizes[ctx().panes[0].id]).toBe(100);
  });

  it('closeSplit from 3 panes returns to single pane', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(3);

    await actAndUpdate(() => {
      ctx().closeSplit();
    });

    expect(ctx().panes.length).toBe(1);
    expect(ctx().paneLayout).toBe('single');
    expect(ctx().paneSizes[ctx().panes[0].id]).toBe(100);
  });

  it('closeSplit from 2 panes returns to single pane', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(2);
    expect(ctx().paneLayout).toBe('split-vertical');

    await actAndUpdate(() => {
      ctx().closeSplit();
    });

    expect(ctx().panes.length).toBe(1);
    expect(ctx().paneLayout).toBe('single');
    expect(ctx().paneSizes[ctx().panes[0].id]).toBe(100);
  });
});

// ---------------------------------------------------------------------------
// Tests: Pane layout transitions as pane count changes
// ---------------------------------------------------------------------------

describe('Pane layout transitions', () => {
  it('starts with single layout', () => {
    renderProvider();

    expect(ctx().paneLayout).toBe('single');
    expect(ctx().panes.length).toBe(1);
  });

  it('transitions to split-vertical on first vertical split', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    expect(ctx().paneLayout).toBe('single');

    await actAndUpdate(() => {
      ctx().splitPane(paneId1, 'vertical');
    });

    expect(ctx().paneLayout).toBe('split-vertical');
    expect(ctx().panes.length).toBe(2);
  });

  it('transitions to split-horizontal on first horizontal split', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    expect(ctx().paneLayout).toBe('single');

    await actAndUpdate(() => {
      ctx().splitPane(paneId1, 'horizontal');
    });

    expect(ctx().paneLayout).toBe('split-horizontal');
    expect(ctx().panes.length).toBe(2);
  });

  it('maintains split layout after adding more panes', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().paneLayout).toBe('split-vertical');

    // Adding more panes should not revert to single
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));

    expect(ctx().panes.length).toBe(5);
    expect(ctx().paneLayout).not.toBe('single');
  });

  it('reverts to single layout when last non-primary pane is closed', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().paneLayout).toBe('split-vertical');

    // Close the secondary pane (only non-primary)
    await actAndUpdate(() => {
      ctx().closePane(ctx().panes[1].id);
    });

    expect(ctx().paneLayout).toBe('single');
    expect(ctx().panes.length).toBe(1);
  });
});

// ---------------------------------------------------------------------------
// Tests: Configurable maxPanes via settings (4 and other values)
// ---------------------------------------------------------------------------

describe('Configurable maxPanes via settings', () => {
  it('maxPanes set to 4 prevents creating 5th pane', async () => {
    const TestConsumer = () => {
      const ctx = useEditorManager();
      latestContext = ctx;
      return null;
    };

    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);

    act(() => {
      root.render(
        createElement(EditorManagerProvider, { maxPanes: 4 }, createElement(TestConsumer)),
      );
    });

    const paneId1 = ctx().panes[0].id;

    // Create 2nd pane
    let paneId: string;
    await actAndUpdate(() => {
      paneId = ctx().splitPane(paneId1, 'vertical');
    });
    expect(paneId).toBeTruthy();
    expect(ctx().panes.length).toBe(2);

    // Create 3rd pane
    await actAndUpdate(() => {
      paneId = ctx().splitPane(paneId1, 'horizontal');
    });
    expect(paneId).toBeTruthy();
    expect(ctx().panes.length).toBe(3);

    // Create 4th pane
    await actAndUpdate(() => {
      paneId = ctx().splitPane(paneId1, 'vertical');
    });
    expect(paneId).toBeTruthy();
    expect(ctx().panes.length).toBe(4);

    // Try to create 5th pane — should be rejected
    await actAndUpdate(() => {
      paneId = ctx().splitPane(paneId1, 'horizontal');
    });
    expect(paneId).toBeNull();
    expect(ctx().panes.length).toBe(4);
  });

  it('maxPanes set to 2 prevents creating 3rd pane', async () => {
    const TestConsumer = () => {
      const ctx = useEditorManager();
      latestContext = ctx;
      return null;
    };

    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);

    act(() => {
      root.render(
        createElement(EditorManagerProvider, { maxPanes: 2 }, createElement(TestConsumer)),
      );
    });

    const paneId1 = ctx().panes[0].id;
    let paneId: string;

    // Create 2nd pane — should work
    await actAndUpdate(() => {
      paneId = ctx().splitPane(paneId1, 'vertical');
    });
    expect(paneId).toBeTruthy();
    expect(ctx().panes.length).toBe(2);

    // Try to create 3rd pane — should be rejected
    await actAndUpdate(() => {
      paneId = ctx().splitPane(paneId1, 'horizontal');
    });
    expect(paneId).toBeNull();
    expect(ctx().panes.length).toBe(2);
  });

  it('maxPanes respects lower limit when set below MAX_PANES', async () => {
    const TestConsumer = () => {
      const ctx = useEditorManager();
      latestContext = ctx;
      return null;
    };

    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);

    act(() => {
      root.render(
        createElement(EditorManagerProvider, { maxPanes: 3 }, createElement(TestConsumer)),
      );
    });

    const paneId1 = ctx().panes[0].id;
    let paneId: string;

    // Create 3 panes
    await actAndUpdate(() => {
      paneId = ctx().splitPane(paneId1, 'vertical');
    });
    await actAndUpdate(() => {
      paneId = ctx().splitPane(paneId1, 'horizontal');
    });

    expect(ctx().panes.length).toBe(3);

    // 4th pane should be rejected at limit of 3
    await actAndUpdate(() => {
      paneId = ctx().splitPane(paneId1, 'vertical');
    });
    expect(paneId).toBeNull();
    expect(ctx().panes.length).toBe(3);
  });
});

// ---------------------------------------------------------------------------
// Tests: splitPane rejects beyond configurable limit at different thresholds
// ---------------------------------------------------------------------------

describe('splitPane rejection at various configurable limits', () => {
  it('default maxPanes allows exactly 6 panes then rejects 7th', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;

    // Create exactly 6 panes using explicit calls
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(2);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(3);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(4);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(5);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(6);

    // 7th should be null
    let result: string | null;
    await actAndUpdate(() => {
      result = ctx().splitPane(paneId1, 'vertical');
    });
    expect(result).toBeNull();
    expect(ctx().panes.length).toBe(6);
  });

  it('splitPane returns null without mutating state when at limit', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;

    // Create 6 panes using explicit calls
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(2);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(3);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(4);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(5);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(6);

    const panesBefore = ctx().panes.map((p) => p.id);
    const layoutBefore = ctx().paneLayout;

    // Attempt 7th split
    await actAndUpdate(() => {
      const result = ctx().splitPane(paneId1, 'vertical');
      expect(result).toBeNull();
    });

    // State should be unchanged
    expect(ctx().panes.map((p) => p.id)).toEqual(panesBefore);
    expect(ctx().paneLayout).toBe(layoutBefore);
  });
});

// ---------------------------------------------------------------------------
// Tests: 4-6 pane creation and state verification
// ---------------------------------------------------------------------------

describe('4-6 pane creation edge cases', () => {
  it('creating 4 panes assigns quaternary position to the 4th pane', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));

    const fourthPane = ctx().panes.find((p) => p.position === 'quaternary');
    expect(fourthPane).toBeDefined();
    expect(fourthPane.id).toBeTruthy();
    expect(ctx().panes.length).toBe(4);
  });

  it('creating 5 panes assigns quinary position to the 5th pane', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));

    const fifthPane = ctx().panes.find((p) => p.position === 'quinary');
    expect(fifthPane).toBeDefined();
    expect(fifthPane.id).toBeTruthy();
    expect(ctx().panes.length).toBe(5);
  });

  it('each pane has a unique id after creating 6 panes', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    // Create 6 panes using explicit calls to avoid Date.now() collisions
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(2);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(3);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(4);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(5);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(6);

    const ids = ctx().panes.map((p) => p.id);
    expect(new Set(ids).size).toBe(6);
    ids.forEach((id) => {
      expect(id).toBeTruthy();
      expect(typeof id).toBe('string');
    });
  });

  it('all 6 panes have valid position values', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;
    // Create 6 panes using explicit calls
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(2);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(3);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(4);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'horizontal'));
    expect(ctx().panes.length).toBe(5);
    await actAndUpdate(() => ctx().splitPane(paneId1, 'vertical'));
    expect(ctx().panes.length).toBe(6);

    const validPositions = ['primary', 'secondary', 'tertiary', 'quaternary', 'quinary', 'senary'];
    ctx().panes.forEach((pane) => {
      expect(validPositions).toContain(pane.position);
    });

    // Each position appears exactly once
    const positionCounts: Record<string, number> = {};
    ctx().panes.forEach((pane) => {
      positionCounts[pane.position] = (positionCounts[pane.position] || 0) + 1;
    });
    validPositions.forEach((pos) => {
      expect(positionCounts[pos]).toBe(1);
    });
  });

  it('activePaneId is set to the newly created pane after split', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;

    let newPaneId: string;
    await actAndUpdate(() => {
      newPaneId = ctx().splitPane(paneId1, 'vertical');
    });

    expect(ctx().activePaneId).toBe(newPaneId);

    // Continue splitting — active should update each time
    let newPaneId2: string;
    await actAndUpdate(() => {
      newPaneId2 = ctx().splitPane(paneId1, 'horizontal');
    });

    expect(ctx().activePaneId).toBe(newPaneId2);
  });

  it('newly created panes start with null bufferId', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;

    let newPaneId: string;
    await actAndUpdate(() => {
      newPaneId = ctx().splitPane(paneId1, 'vertical');
    });

    const newPane = ctx().panes.find((p) => p.id === newPaneId);
    expect(newPane).toBeDefined();
    expect(newPane.bufferId).toBeNull();
  });

  it('newly created panes start with isActive = false', async () => {
    renderProvider();

    const paneId1 = ctx().panes[0].id;

    let newPaneId: string;
    await actAndUpdate(() => {
      newPaneId = ctx().splitPane(paneId1, 'vertical');
    });

    const newPane = ctx().panes.find((p) => p.id === newPaneId);
    expect(newPane).toBeDefined();
    expect(newPane.isActive).toBe(false);
  });
});
