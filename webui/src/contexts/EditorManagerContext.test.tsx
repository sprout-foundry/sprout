// @ts-nocheck

import React from 'react';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { EditorManagerProvider, useEditorManager } from './EditorManagerContext';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

jest.mock('../services/fileAccess', () => ({
  writeFileWithConsent: jest.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve({ message: 'File saved successfully' }),
  }),
}));

// ---------------------------------------------------------------------------
// Helpers
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
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

/**
 * Test component that consumes the EditorManager context and stores
 * a reference to the latest context value for inspection in tests.
 */
function TestConsumer() {
  const ctx = useEditorManager();
  latestContext = ctx;
  return null;
}

/**
 * Mounts the EditorManagerProvider with a TestConsumer child.
 */
function renderProvider() {
  // eslint-disable-next-line testing-library/no-unnecessary-act
  act(() => {
    root.render(React.createElement(EditorManagerProvider, null, React.createElement(TestConsumer)));
  });
}

/**
 * Run a state-changing callback inside act() and flush microtasks
 * so that all React state updates are committed and latestContext
 * reflects the latest render.
 *
 * Uses multiple flush rounds to handle React 18's automatic batching.
 */
const actAndUpdate = async (fn: () => void) => {
  act(() => {
    fn();
  });
  // Triple-flush to handle React 18 automatic batching + microtasks
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
};

/**
 * Like actAndUpdate but adds a small real-world delay (2ms) after flushing.
 * Use this when buffer IDs are generated via Date.now() and multiple
 * buffers may be created in rapid succession (collision prevention).
 */
const actAndUpdateWithDelay = async (fn: () => void) => {
  act(() => {
    fn();
  });
  await act(async () => {
    await Promise.resolve();
  });
  await new Promise((r) => setTimeout(r, 2));
};

/** Shorthand to get the current context value from the latest render. */
const ctx = () => latestContext;

/** Collect buffers with a given paneId into an array. */
const buffersInPane = (paneId: string) => Array.from(ctx().buffers.values()).filter((b) => b.paneId === paneId);

/** Collect active (isActive === true) buffers in a given pane. */
const activeBuffersInPane = (paneId: string) =>
  Array.from(ctx().buffers.values()).filter((b) => b.paneId === paneId && b.isActive === true);

/** Helper to create a file object for openFile. */
const makeFile = (path: string, name: string) => ({
  path,
  name,
  isDir: false,
  size: 0,
  modified: 0,
  ext: `.${name.split('.').pop() || 'txt'}`,
});

// ---------------------------------------------------------------------------
// Tests: EditorManager tab / pane management
// ---------------------------------------------------------------------------

describe('EditorManager tab pane management', () => {
  // -----------------------------------------------------------------------
  // 1. openFile deactivates previous buffer but preserves paneId
  //    The design keeps paneId on old buffers but sets isActive=false.
  //    Only the most recently opened buffer should be active.
  // -----------------------------------------------------------------------
  it('openFile deactivates previous buffer but preserves paneId', async () => {
    renderProvider();

    const file1 = makeFile('/test/file1.txt', 'file1.txt');
    const file2 = makeFile('/test/file2.txt', 'file2.txt');

    const activePaneId = ctx().activePaneId;

    // Open file1 – it should become active in the pane
    let buf1Id: string;
    await actAndUpdateWithDelay(() => {
      buf1Id = ctx().openFile(file1);
    });
    expect(buf1Id).toBeTruthy();

    const buf1 = ctx().buffers.get(buf1Id);
    expect(buf1.paneId).toBe(activePaneId);
    expect(buf1.isActive).toBe(true);
    expect(ctx().activeBufferId).toBe(buf1Id);

    // Open file2 – file1 should be deactivated but keep its paneId
    let buf2Id: string;
    await actAndUpdateWithDelay(() => {
      buf2Id = ctx().openFile(file2);
    });
    expect(buf2Id).toBeTruthy();

    const buf2 = ctx().buffers.get(buf2Id);
    expect(buf2.isActive).toBe(true);
    expect(buf2.paneId).toBe(activePaneId);
    expect(ctx().activeBufferId).toBe(buf2Id);

    // file1 must be deactivated but RETAIN its paneId
    const buf1After = ctx().buffers.get(buf1Id!);
    expect(buf1After.paneId).toBe(activePaneId);
    expect(buf1After.isActive).toBe(false);

    // Exactly one active buffer in the pane
    expect(activeBuffersInPane(activePaneId).length).toBe(1);
    expect(activeBuffersInPane(activePaneId)[0].id).toBe(buf2Id);
  });

  // -----------------------------------------------------------------------
  // 2. openWorkspaceBuffer deactivates previous buffer but preserves paneId
  // -----------------------------------------------------------------------
  it('openWorkspaceBuffer deactivates previous buffer but preserves paneId', async () => {
    renderProvider();

    const activePaneId = ctx().activePaneId;

    // The initial chat buffer should be in pane-1
    const chatBuffer = ctx().buffers.get('buffer-chat');
    expect(chatBuffer.paneId).toBe(activePaneId);
    expect(chatBuffer.isActive).toBe(true);

    // Open a diff workspace buffer (non-chat kind → targets activePaneId)
    let diffId: string;
    await actAndUpdateWithDelay(() => {
      diffId = ctx().openWorkspaceBuffer({
        kind: 'diff',
        path: '__workspace/diff-deactivate-test',
        title: 'Diff Test',
        content: 'some diff',
      });
    });

    expect(diffId).toBeTruthy();
    const diffBuf = ctx().buffers.get(diffId!);
    expect(diffBuf.paneId).toBe(activePaneId);
    expect(diffBuf.isActive).toBe(true);

    // The chat buffer must be deactivated but keep its paneId
    const chatAfter = ctx().buffers.get('buffer-chat');
    expect(chatAfter.paneId).toBe(activePaneId);
    expect(chatAfter.isActive).toBe(false);

    // Both buffers should be associated with the pane
    expect(buffersInPane(activePaneId).length).toBe(2);
    // Only one should be active
    expect(activeBuffersInPane(activePaneId).length).toBe(1);
    expect(activeBuffersInPane(activePaneId)[0].id).toBe(diffId);
  });

  // -----------------------------------------------------------------------
  // 3. openWorkspaceBuffer with existing path reactivates instead of duping
  // -----------------------------------------------------------------------
  it('openWorkspaceBuffer with existing buffer activates it without creating duplicates', async () => {
    renderProvider();

    const path = '__workspace/dedup-test';

    let buf1Id: string;
    await actAndUpdateWithDelay(() => {
      buf1Id = ctx().openWorkspaceBuffer({
        kind: 'review',
        path,
        title: 'Review Test',
        content: 'initial content',
      });
    });
    expect(buf1Id).toBeTruthy();

    // Open again with the same path – must find the existing buffer
    let buf2Id: string;
    await actAndUpdateWithDelay(() => {
      buf2Id = ctx().openWorkspaceBuffer({
        kind: 'review',
        path,
        title: 'Review Test Updated',
        content: 'updated content',
      });
    });

    // Same buffer id – no duplicate created
    expect(buf2Id).toBe(buf1Id);

    // Verify only one buffer has this path
    const matching = Array.from(ctx().buffers.values()).filter((b) => b.file.path === path);
    expect(matching.length).toBe(1);

    // The buffer should be active
    expect(ctx().activeBufferId).toBe(buf1Id);
    const buf = ctx().buffers.get(buf1Id!);
    expect(buf.isActive).toBe(true);
  });

  // -----------------------------------------------------------------------
  // 4. switchToBuffer for cross-pane buffer updates pane.bufferId
  // -----------------------------------------------------------------------
  it('switchToBuffer for cross-pane buffer updates pane.bufferId', async () => {
    renderProvider();

    const file1 = makeFile('/test/cross-pane-a.txt', 'a.txt');
    const file2 = makeFile('/test/cross-pane-b.txt', 'b.txt');

    const initialPaneId = ctx().activePaneId; // 'pane-1'

    // Open file1 in pane-1
    let buf1Id: string;
    await actAndUpdateWithDelay(() => {
      buf1Id = ctx().openFile(file1);
    });
    expect(ctx().buffers.get(buf1Id!).paneId).toBe(initialPaneId);

    // Split pane to create a second pane — switches activePaneId
    let pane2Id: string;
    await actAndUpdateWithDelay(() => {
      pane2Id = ctx().splitPane(initialPaneId, 'vertical');
    });
    expect(pane2Id).toBeTruthy();
    expect(ctx().activePaneId).toBe(pane2Id);
    expect(ctx().panes.length).toBe(2);

    // Open file2 — it should go into the new active pane (pane2)
    let buf2Id: string;
    await actAndUpdateWithDelay(() => {
      buf2Id = ctx().openFile(file2);
    });
    expect(ctx().buffers.get(buf2Id!).paneId).toBe(pane2Id);
    expect(ctx().activeBufferId).toBe(buf2Id);

    // Now switch back to buf1 (which lives in pane-1 via switchToBuffer)
    // This triggers the cross-pane branch: detects buf1.paneId !== activePaneId
    await actAndUpdate(() => {
      ctx().switchToBuffer(buf1Id!);
    });

    // The active pane should now be pane-1
    expect(ctx().activePaneId).toBe(initialPaneId);

    // pane-1's bufferId should point to buf1
    const pane1 = ctx().panes.find((p) => p.id === initialPaneId);
    expect(pane1.bufferId).toBe(buf1Id);

    // buf1 should still be associated with pane-1
    expect(ctx().buffers.get(buf1Id!).paneId).toBe(initialPaneId);
    expect(ctx().activeBufferId).toBe(buf1Id);
  });

  // -----------------------------------------------------------------------
  // 5. Only one ACTIVE buffer per pane after multiple openFile calls
  //    (all previous buffers keep paneId but are set to isActive=false)
  // -----------------------------------------------------------------------
  it('only one active buffer per pane after multiple openFile calls', async () => {
    renderProvider();

    const files = [
      makeFile('/test/only-one-1.txt', '1.txt'),
      makeFile('/test/only-one-2.txt', '2.txt'),
      makeFile('/test/only-one-3.txt', '3.txt'),
    ];

    const activePaneId = ctx().activePaneId;

    // Open three files sequentially with delays to avoid Date.now() collisions
    const ids: string[] = [];
    for (const f of files) {
      let id: string;
      await actAndUpdateWithDelay(() => {
        id = ctx().openFile(f);
      });
      ids.push(id!);
    }

    // The last one should be the ACTIVE buffer
    expect(ctx().activeBufferId).toBe(ids[2]);

    // ALL four buffers (chat + 3 files) should be in the pane, preserving paneId
    const allInPane = buffersInPane(activePaneId);
    expect(allInPane.length).toBe(4);

    // Exactly one ACTIVE buffer should be in the pane
    const active = activeBuffersInPane(activePaneId);
    expect(active.length).toBe(1);
    expect(active[0].id).toBe(ids[2]);
    expect(active[0].isActive).toBe(true);

    // The previous two file buffers + chat buffer should be inactive
    // but keep their paneId
    ids.slice(0, 2).forEach((id) => {
      const buf = ctx().buffers.get(id!);
      expect(buf.isActive).toBe(false);
      expect(buf.paneId).toBe(activePaneId);
    });

    // The initial chat buffer should also be inactive with paneId preserved
    const chat = ctx().buffers.get('buffer-chat');
    expect(chat.isActive).toBe(false);
    expect(chat.paneId).toBe(activePaneId);
  });
});

// ---------------------------------------------------------------------------
// paneId preservation fix: comprehensive tests for the tab behavior
// ---------------------------------------------------------------------------

describe('paneId preservation fix', () => {
  // -----------------------------------------------------------------------
  // 1. activateBuffer: round-trip switching preserves paneId on all buffers
  // -----------------------------------------------------------------------
  it('preserves paneId on all buffers when switching between tabs in the same pane', async () => {
    renderProvider();

    const file1 = makeFile('/test/switch-1.txt', 'switch-1.txt');
    const file2 = makeFile('/test/switch-2.txt', 'switch-2.txt');

    const paneId = ctx().activePaneId;

    // Open two files into the active pane
    let buf1Id: string;
    await actAndUpdateWithDelay(() => {
      buf1Id = ctx().openFile(file1);
    });

    let buf2Id: string;
    await actAndUpdateWithDelay(() => {
      buf2Id = ctx().openFile(file2);
    });

    // After opening both, buf2 is active; buf1 is inactive
    expect(ctx().buffers.get(buf2Id!).isActive).toBe(true);
    expect(ctx().buffers.get(buf1Id!).isActive).toBe(false);

    // Both must still have the same paneId
    expect(ctx().buffers.get(buf1Id!).paneId).toBe(paneId);
    expect(ctx().buffers.get(buf2Id!).paneId).toBe(paneId);

    // Switch back to buf1 via switchToBuffer (which triggers deactivate logic)
    await actAndUpdateWithDelay(() => {
      ctx().switchToBuffer(buf1Id!);
    });

    expect(ctx().buffers.get(buf1Id!).isActive).toBe(true);
    expect(ctx().buffers.get(buf1Id!).paneId).toBe(paneId);

    // buf2 must be inactive BUT keep its paneId
    expect(ctx().buffers.get(buf2Id!).isActive).toBe(false);
    expect(ctx().buffers.get(buf2Id!).paneId).toBe(paneId);

    // Switch back to buf2
    await actAndUpdateWithDelay(() => {
      ctx().switchToBuffer(buf2Id!);
    });

    expect(ctx().buffers.get(buf2Id!).isActive).toBe(true);
    expect(ctx().buffers.get(buf2Id!).paneId).toBe(paneId);

    // buf1 must be inactive but retain paneId
    expect(ctx().buffers.get(buf1Id!).isActive).toBe(false);
    expect(ctx().buffers.get(buf1Id!).paneId).toBe(paneId);
  });

  // -----------------------------------------------------------------------
  // 2. openFile: three sequential opens all retain paneId
  // -----------------------------------------------------------------------
  it('openFile preserves paneId on all existing buffers in the same pane after three opens', async () => {
    renderProvider();

    const paneId = ctx().activePaneId;

    const file1 = makeFile('/test/preserve-1.ts', 'preserve-1.ts');
    const file2 = makeFile('/test/preserve-2.ts', 'preserve-2.ts');
    const file3 = makeFile('/test/preserve-3.ts', 'preserve-3.ts');

    let buf1Id: string;
    await actAndUpdateWithDelay(() => {
      buf1Id = ctx().openFile(file1);
    });

    let buf2Id: string;
    await actAndUpdateWithDelay(() => {
      buf2Id = ctx().openFile(file2);
    });

    let buf3Id: string;
    await actAndUpdateWithDelay(() => {
      buf3Id = ctx().openFile(file3);
    });

    // After opening buf3, all buffers should still have paneId
    expect(ctx().buffers.get(buf1Id!).paneId).toBe(paneId);
    expect(ctx().buffers.get(buf2Id!).paneId).toBe(paneId);
    expect(ctx().buffers.get(buf3Id!).paneId).toBe(paneId);

    // Also check the initial chat buffer
    expect(ctx().buffers.get('buffer-chat').paneId).toBe(paneId);

    // Only the most recent file should be active
    expect(ctx().buffers.get(buf1Id!).isActive).toBe(false);
    expect(ctx().buffers.get(buf2Id!).isActive).toBe(false);
    expect(ctx().buffers.get(buf3Id!).isActive).toBe(true);
  });

  // -----------------------------------------------------------------------
  // 3. openWorkspaceBuffer preserves paneId when opening workspace buffers
  // -----------------------------------------------------------------------
  it('openWorkspaceBuffer preserves paneId when switching to a new workspace buffer', async () => {
    renderProvider();

    const paneId = ctx().activePaneId;

    // Open a review workspace buffer (non-chat targets activePaneId)
    let reviewId: string;
    await actAndUpdateWithDelay(() => {
      reviewId = ctx().openWorkspaceBuffer({
        kind: 'review',
        path: '__workspace/review-preserve-test',
        title: 'Review',
        content: 'review content',
      });
    });

    // Chat buffer should be inactive but still have paneId
    const chatBuffer = ctx().buffers.get('buffer-chat');
    expect(chatBuffer.paneId).toBe(paneId);
    expect(chatBuffer.isActive).toBe(false);

    // Review buffer is active in the pane
    expect(ctx().buffers.get(reviewId!).paneId).toBe(paneId);
    expect(ctx().buffers.get(reviewId!).isActive).toBe(true);

    // Now open a regular file — both chat and review should keep their paneId
    const file = makeFile('/test/workspace-file.txt', 'workspace-file.txt');
    let fileId: string;
    await actAndUpdateWithDelay(() => {
      fileId = ctx().openFile(file);
    });

    expect(ctx().buffers.get('buffer-chat').paneId).toBe(paneId);
    expect(ctx().buffers.get(reviewId!).paneId).toBe(paneId);
    expect(ctx().buffers.get(fileId!).paneId).toBe(paneId);

    // Only the file is active
    expect(ctx().buffers.get('buffer-chat').isActive).toBe(false);
    expect(ctx().buffers.get(reviewId!).isActive).toBe(false);
    expect(ctx().buffers.get(fileId!).isActive).toBe(true);
  });

  // -----------------------------------------------------------------------
  // 4. switchToBuffer preserves paneId for all same-pane buffers
  // -----------------------------------------------------------------------
  it('switchToBuffer preserves paneId for all same-pane buffers', async () => {
    renderProvider();

    const paneId = ctx().activePaneId;

    // 2 files + initial chat buffer = 3 buffers in pane-1
    const file1 = makeFile('/test/same-pane-1.ts', 'same-pane-1.ts');
    const file2 = makeFile('/test/same-pane-2.ts', 'same-pane-2.ts');

    let buf1Id: string;
    await actAndUpdateWithDelay(() => {
      buf1Id = ctx().openFile(file1);
    });

    let buf2Id: string;
    await actAndUpdateWithDelay(() => {
      buf2Id = ctx().openFile(file2);
    });

    // All buffers should be in the same pane at this point
    const allBuffers = Array.from(ctx().buffers.values());
    allBuffers.forEach((b) => {
      expect(b.paneId).toBe(paneId);
    });

    // Switch to chat buffer
    await actAndUpdateWithDelay(() => {
      ctx().switchToBuffer('buffer-chat');
    });
    expect(ctx().buffers.get('buffer-chat').isActive).toBe(true);
    expect(ctx().buffers.get('buffer-chat').paneId).toBe(paneId);
    expect(ctx().buffers.get(buf1Id!).paneId).toBe(paneId);
    expect(ctx().buffers.get(buf2Id!).paneId).toBe(paneId);

    // Switch to buf1
    await actAndUpdateWithDelay(() => {
      ctx().switchToBuffer(buf1Id!);
    });
    expect(ctx().buffers.get(buf1Id!).isActive).toBe(true);
    expect(ctx().buffers.get(buf1Id!).paneId).toBe(paneId);
    expect(ctx().buffers.get('buffer-chat').paneId).toBe(paneId);
    expect(ctx().buffers.get(buf2Id!).paneId).toBe(paneId);

    // Switch to buf2
    await actAndUpdateWithDelay(() => {
      ctx().switchToBuffer(buf2Id!);
    });
    expect(ctx().buffers.get(buf2Id!).isActive).toBe(true);
    expect(ctx().buffers.get(buf2Id!).paneId).toBe(paneId);
    expect(ctx().buffers.get(buf1Id!).paneId).toBe(paneId);
    expect(ctx().buffers.get('buffer-chat').paneId).toBe(paneId);
  });

  // -----------------------------------------------------------------------
  // 5. closeBuffer does not orphan remaining tab paneIds
  // -----------------------------------------------------------------------
  it('closeBuffer keeps paneId on remaining buffers and activates the next correctly', async () => {
    renderProvider();

    const paneId = ctx().activePaneId;

    const file1 = makeFile('/test/close-1.ts', 'close-1.ts');
    const file2 = makeFile('/test/close-2.ts', 'close-2.ts');
    const file3 = makeFile('/test/close-3.ts', 'close-3.ts');

    let buf1Id: string;
    await actAndUpdateWithDelay(() => {
      buf1Id = ctx().openFile(file1);
    });
    let buf2Id: string;
    await actAndUpdateWithDelay(() => {
      buf2Id = ctx().openFile(file2);
    });
    let buf3Id: string;
    await actAndUpdateWithDelay(() => {
      buf3Id = ctx().openFile(file3);
    });

    // Close file2 (the middle buffer — active is buf3)
    await actAndUpdateWithDelay(() => {
      ctx().closeBuffer(buf2Id!);
    });

    // buf2 should be gone
    expect(ctx().buffers.get(buf2Id!)).toBeUndefined();

    // buf1 and buf3 should keep their paneId
    expect(ctx().buffers.get(buf1Id!).paneId).toBe(paneId);
    expect(ctx().buffers.get(buf3Id!).paneId).toBe(paneId);

    // buf3 should still be active since it was active before close
    expect(ctx().activeBufferId).toBe(buf3Id);
    expect(ctx().buffers.get(buf3Id!).isActive).toBe(true);

    // Now close the active buffer (buf3) — closeBuffer picks the next pane-mate
    // in Map iteration order (buffer-chat is first → becomes the new active buffer).
    // The key invariant: ALL remaining buffers keep their paneId.
    await actAndUpdateWithDelay(() => {
      ctx().closeBuffer(buf3Id!);
    });

    expect(ctx().buffers.get(buf3Id!)).toBeUndefined();

    // The new active buffer must be a valid pane-mate
    const newActiveId = ctx().activeBufferId;
    expect([buf1Id, 'buffer-chat']).toContain(newActiveId);
    expect(ctx().buffers.get(newActiveId!).paneId).toBe(paneId);
    expect(ctx().buffers.get(newActiveId!).isActive).toBe(true);

    // buf1 should still be in the pane (not orphaned)
    expect(ctx().buffers.get(buf1Id!)).toBeDefined();
    expect(ctx().buffers.get(buf1Id!).paneId).toBe(paneId);
  });

  // -----------------------------------------------------------------------
  // 6. Duplicate path handling in openFile activates existing buffer
  // -----------------------------------------------------------------------
  it('openFile with duplicate path activates the existing buffer without creating a new one', async () => {
    renderProvider();

    const file = makeFile('/test/dup.txt', 'dup.txt');

    let buf1Id: string;
    await actAndUpdateWithDelay(() => {
      buf1Id = ctx().openFile(file);
    });

    // Open the same file path again
    let buf2Id: string;
    await actAndUpdateWithDelay(() => {
      buf2Id = ctx().openFile(file);
    });

    // Should return the same buffer ID (no duplicate)
    expect(buf2Id).toBe(buf1Id);

    // Only one buffer with this path
    const matching = Array.from(ctx().buffers.values()).filter((b) => b.file.path === '/test/dup.txt');
    expect(matching.length).toBe(1);

    // The buffer should be active
    expect(ctx().activeBufferId).toBe(buf1Id);
    expect(ctx().buffers.get(buf1Id!).isActive).toBe(true);
  });

  // -----------------------------------------------------------------------
  // 7. The default chat buffer is never closable (isClosable: false)
  // -----------------------------------------------------------------------
  it('default chat buffer cannot be closed because isClosable is false', async () => {
    renderProvider();

    // Verify the initial chat buffer has isClosable = false
    const chatBuffer = ctx().buffers.get('buffer-chat');
    expect(chatBuffer.isClosable).toBe(false);

    // Attempt to close it — should be a no-op
    const bufferCountBefore = ctx().buffers.size;
    await actAndUpdateWithDelay(() => {
      ctx().closeBuffer('buffer-chat');
    });

    // The chat buffer should still be in the map, unchanged
    expect(ctx().buffers.get('buffer-chat')).toBeDefined();
    expect(ctx().buffers.size).toBe(bufferCountBefore);
  });

  // -----------------------------------------------------------------------
  // 8. openFile cross-pane reactivation switches pane and activates correctly
  // -----------------------------------------------------------------------
  it('openFile for cross-pane existing buffer switches pane and activates correctly', async () => {
    renderProvider();

    const file1 = makeFile('/test/cross-pane-1.txt', 'cross-pane-1.txt');
    const file2 = makeFile('/test/cross-pane-2.txt', 'cross-pane-2.txt');

    const pane1Id = ctx().activePaneId; // 'pane-1'

    // Open file1 in pane-1, then file2 (deactivates file1)
    let buf1Id: string;
    await actAndUpdateWithDelay(() => {
      buf1Id = ctx().openFile(file1);
    });
    await actAndUpdateWithDelay(() => {
      ctx().openFile(file2);
    });

    expect(ctx().buffers.get(buf1Id!).paneId).toBe(pane1Id);
    expect(ctx().buffers.get(buf1Id!).isActive).toBe(false);

    // Split pane to create pane-2 and open file3 there
    let pane2Id: string;
    await actAndUpdateWithDelay(() => {
      pane2Id = ctx().splitPane(pane1Id!, 'vertical');
    });
    expect(ctx().activePaneId).toBe(pane2Id);

    const file3 = makeFile('/test/cross-pane-3.txt', 'cross-pane-3.txt');
    await actAndUpdateWithDelay(() => {
      ctx().openFile(file3);
    });

    // Now openFile for file1 (which is in pane-1) from pane-2 context
    // This should switch to pane-1, activate file1, and set pane.bufferId
    let sameId: string;
    await actAndUpdateWithDelay(() => {
      sameId = ctx().openFile(file1);
    });

    // Should return the same buffer id
    expect(sameId).toBe(buf1Id);

    // activePaneId should now be pane-1
    expect(ctx().activePaneId).toBe(pane1Id);

    // pane-1's bufferId should point to buf1
    const pane1 = ctx().panes.find((p) => p.id === pane1Id);
    expect(pane1.bufferId).toBe(buf1Id);

    // buf1 should be active
    expect(ctx().buffers.get(buf1Id!).isActive).toBe(true);
    expect(ctx().buffers.get(buf1Id!).paneId).toBe(pane1Id);

    // file2 (also in pane-1) should be deactivated but keep its paneId
    const file2Buffer = Array.from(ctx().buffers.values()).find((b) => b.file.path === '/test/cross-pane-2.txt');
    expect(file2Buffer.isActive).toBe(false);
    expect(file2Buffer.paneId).toBe(pane1Id);

    // --- pane-2's state should be completely unaffected by the cross-pane switch ---
    const file3Buffer = Array.from(ctx().buffers.values()).find((b) => b.file.path === '/test/cross-pane-3.txt');
    expect(file3Buffer).toBeDefined();
    expect(file3Buffer.isActive).toBe(true);
    expect(file3Buffer.paneId).toBe(pane2Id);

    const pane2 = ctx().panes.find((p) => p.id === pane2Id);
    expect(pane2).toBeDefined();
    expect(pane2.bufferId).toBe(file3Buffer.id);
  });
});
