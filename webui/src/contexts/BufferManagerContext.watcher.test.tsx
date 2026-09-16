// @ts-nocheck
// Integration test: with the external file watcher rewired into
// BufferManagerProvider, an external write to an open CLEAN buffer is
// detected on the poll and the buffer auto-reloads from disk.
import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from 'vitest';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { EditorManagerProvider, useEditorManager } from './EditorManagerContext';
import { checkFilesModified } from '../services/apiFileCheck';
import { readFileWithConsent } from '../services/fileAccess';

vi.mock('../services/apiFileCheck', () => ({
  checkFilesModified: vi.fn(),
}));

vi.mock('../services/fileAccess', () => ({
  readFileWithConsent: vi.fn(),
}));

// Autosave off — keeps the test deterministic (no background writes flipping
// isModified mid-test).
vi.mock('./EditorSettingsContext', async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    useEditorSettings: () => ({
      ...actual.useEditorSettings(),
      isAutoSaveEnabled: false,
      isFormatOnSaveEnabled: false,
    }),
  };
});

vi.mock('./SproutAdapterContext', () => ({
  SproutAdapterProvider: ({ children }) => children,
  useSproutAdapter: () => ({ clientFetch: vi.fn() }),
  useSproutFetch: () => vi.fn(),
}));

const mockCheckFilesModified = checkFilesModified as ReturnType<typeof vi.fn>;
const mockReadFileWithConsent = readFileWithConsent as ReturnType<typeof vi.fn>;

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

/** Shorthand to get the current context value from the latest render. */
const ctx = () => latestContext;

function renderProvider() {
  act(() => {
    root.render(createElement(EditorManagerProvider, null, createElement(TestConsumer)));
  });
}

describe('external file watcher integration (rewired into BufferManagerProvider)', () => {
  it('auto-reloads a clean open buffer when the file changes on disk', async () => {
    // The mock endpoint always reports the change (mtime 2000 vs the buffer's
    // learned 0). Poll 1 is the watcher's first sighting of the path — it
    // learns the mtime silently; poll 2 fires the notification.
    mockCheckFilesModified.mockResolvedValue({
      modified: [{ path: '/w/a.ts', mod_time: 2000, size: 3 }],
    });
    mockReadFileWithConsent.mockResolvedValue({
      ok: true,
      text: async () => 'abc',
    });

    renderProvider();

    // Open a clean file buffer with known content.
    let bufferId: string | null = null;
    await actAndUpdate(async () => {
      bufferId = ctx().openWorkspaceBuffer({
        kind: 'file',
        path: '/w/a.ts',
        title: 'a.ts',
        content: 'old',
        ext: '.ts',
      });
    });
    expect(bufferId).toBeTruthy();
    expect(ctx().buffers.get(bufferId).content).toBe('old');

    // Start-up poll runs on the count 0→1 transition; the notification fires
    // on the second poll (~3s), then the reload waits out the 500ms
    // coalescing debounce before reading disk.
    await vi.waitFor(
      () => {
        const buffer = ctx().buffers.get(bufferId);
        expect(buffer?.content).toBe('abc');
        expect(buffer?.contentLoaded).toBe(true);
        expect(buffer?.isModified).toBe(false);
      },
      { timeout: 8000, interval: 50 },
    );

    // The disk read went through the consent-aware reader.
    expect(mockReadFileWithConsent).toHaveBeenCalledWith('/w/a.ts');
  });

  it('does not touch buffers whose files have not changed', async () => {
    mockCheckFilesModified.mockResolvedValue({ modified: [] });

    renderProvider();

    let bufferId: string | null = null;
    await actAndUpdate(async () => {
      bufferId = ctx().openWorkspaceBuffer({
        kind: 'file',
        path: '/w/unchanged.ts',
        title: 'unchanged.ts',
        content: 'stable',
        ext: '.ts',
      });
    });

    await new Promise((r) => setTimeout(r, 700));

    expect(ctx().buffers.get(bufferId).content).toBe('stable');
    expect(mockReadFileWithConsent).not.toHaveBeenCalled();
  });
});

/** Flush React batching + microtasks after a state change. */
async function actAndUpdate(fn: () => void) {
  act(() => {
    fn();
  });
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}
