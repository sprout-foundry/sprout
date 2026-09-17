// @ts-nocheck
// Save-path conflict gating and honest post-save buffer state:
//   - saveBuffer refuses to write over an unresolved external change
//     (the auto-save-clobbers-agent-edit race)
//   - an explicit force save (Cmd+S) writes and clears the conflict
//   - closeBuffer refuses to close a conflicted + modified buffer
//   - format-on-save syncs buffer.content so isModified is honest (no
//     rewrite-every-interval loop) and typing during the save roundtrip
//     keeps the buffer modified against the disk snapshot
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from 'vitest';
import { notificationBus } from '../services/notificationBus';
import { EditorManagerProvider, useEditorManager } from './EditorManagerContext';

const sproutFetchMock = vi.fn();

vi.mock('./SproutAdapterContext', () => ({
  SproutAdapterProvider: ({ children }) => children,
  useSproutAdapter: () => ({ clientFetch: vi.fn() }),
  useSproutFetch: () => sproutFetchMock,
}));

// Format-on-save toggled per-test through this handle.
const formatSettings = { enabled: false, formatted: 'FORMATTED' };

vi.mock('./EditorSettingsContext', async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    useEditorSettings: () => ({
      ...actual.useEditorSettings(),
      isAutoSaveEnabled: false,
      isFormatOnSaveEnabled: formatSettings.enabled,
    }),
  };
});

vi.mock('../services/formatter', async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    formatCodeWithConfigDiscovery: vi.fn(async (_content: string) => ({
      formatted: formatSettings.formatted,
      error: undefined,
    })),
    isFormattable: vi.fn(() => true),
  };
});

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
  formatSettings.enabled = false;
  formatSettings.formatted = 'FORMATTED';
  latestContext = undefined;
  localStorage.setItem('sprout-welcome-dismissed', 'true');
  localStorage.removeItem('sprout.editor.layoutState');
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  vi.restoreAllMocks();
});

function TestConsumer() {
  const ctx = useEditorManager();
  latestContext = ctx;
  return null;
}

const ctx = () => latestContext;

function renderProvider() {
  // Rendering a React root — not a Testing Library util call.
  // eslint-disable-next-line testing-library/no-unnecessary-act
  act(() => {
    root.render(createElement(EditorManagerProvider, null, createElement(TestConsumer)));
  });
}

async function actAndUpdate(fn: () => void) {
  act(() => {
    fn();
  });
  // Flush React batching + microtasks (not a Testing Library util call,
  // so the testing-library act lint rule does not apply here).
  // eslint-disable-next-line testing-library/no-unnecessary-act
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function okWriteResponse(path: string) {
  return new Response(
    JSON.stringify({
      success: true,
      message: 'File saved successfully',
      path,
      size: 42,
      mod_time: 5000,
    }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  );
}

async function openModifiedBuffer(path = '/w/conflict.ts', content = 'local edits') {
  renderProvider();
  let bufferId: string | null = null;
  await actAndUpdate(() => {
    bufferId = ctx().openWorkspaceBuffer({
      kind: 'file',
      path,
      title: path.split('/').pop(),
      content: 'original',
      ext: '.ts',
    });
  });
  // Simulate user typing: content diverges from originalContent.
  await actAndUpdate(() => {
    ctx().updateBufferContent(bufferId, content);
  });
  expect(ctx().buffers.get(bufferId).isModified).toBe(true);
  return bufferId;
}

describe('saveBuffer conflict gating', () => {
  it('skips the write while the external-change conflict is unresolved', async () => {
    const bufferId = await openModifiedBuffer();
    act(() => {
      ctx().setBufferExternallyModified(bufferId, 'disk version', 4242);
    });
    sproutFetchMock.mockImplementation(async (_url, _init) => okWriteResponse('/w/conflict.ts'));

    await actAndUpdate(() => {
      ctx().saveBuffer(bufferId);
    });

    expect(sproutFetchMock).not.toHaveBeenCalled();
    const buf = ctx().buffers.get(bufferId);
    expect(buf.content).toBe('local edits');
    expect(buf.isModified).toBe(true);
    expect(buf.externallyModified).toBe(true);
    expect(buf.diskContent).toBe('disk version');
  });

  it('a force save writes, clears the conflict, and lands clean', async () => {
    const bufferId = await openModifiedBuffer();
    act(() => {
      ctx().setBufferExternallyModified(bufferId, 'disk version', 4242);
    });
    sproutFetchMock.mockImplementation(async (_url, init) =>
      okWriteResponse('/w/conflict.ts', JSON.parse(init.body).content),
    );

    await actAndUpdate(() => {
      ctx().saveBuffer(bufferId, { force: true });
    });

    expect(sproutFetchMock).toHaveBeenCalledTimes(1);
    const buf = ctx().buffers.get(bufferId);
    expect(buf.externallyModified).toBe(false);
    expect(buf.diskContent).toBeNull();
    expect(buf.isModified).toBe(false);
    expect(buf.originalContent).toBe('local edits');
    expect(buf.file.modified).toBe(5000);
  });
});

describe('closeBuffer conflict gate', () => {
  it('refuses to close a conflicted modified buffer instead of auto-saving through it', async () => {
    const bufferId = await openModifiedBuffer();
    act(() => {
      ctx().setBufferExternallyModified(bufferId, 'disk version', 4242);
    });
    vi.spyOn(notificationBus, 'notify').mockImplementation(() => undefined);
    sproutFetchMock.mockImplementation(async (_url, _init) => okWriteResponse('/w/conflict.ts'));

    await actAndUpdate(() => {
      ctx().closeBuffer(bufferId);
    });

    expect(notificationBus.notify).toHaveBeenCalled();
    expect(ctx().buffers.has(bufferId)).toBe(true);
    expect(sproutFetchMock).not.toHaveBeenCalled();
  });

  it('closes normally once the conflict is resolved', async () => {
    const bufferId = await openModifiedBuffer();
    act(() => {
      ctx().setBufferExternallyModified(bufferId, 'disk version', 4242);
    });
    sproutFetchMock.mockImplementation(async (_url, _init) => okWriteResponse('/w/conflict.ts'));
    // Force save resolves the conflict.
    await actAndUpdate(() => {
      ctx().saveBuffer(bufferId, { force: true });
    });
    vi.spyOn(notificationBus, 'notify').mockImplementation(() => undefined);

    await actAndUpdate(() => {
      ctx().closeBuffer(bufferId);
    });

    expect(ctx().buffers.has(bufferId)).toBe(false);
  });
});

describe('format-on-save honest buffer state', () => {
  it('syncs buffer content with the formatted disk text instead of looping modified', async () => {
    formatSettings.enabled = true;
    formatSettings.formatted = 'FORMATTED';
    const bufferId = await openModifiedBuffer('/w/fmt.ts', 'unformatted edits');
    sproutFetchMock.mockImplementation(async (_url, _init) => okWriteResponse('/w/fmt.ts'));

    await actAndUpdate(() => {
      ctx().saveBuffer(bufferId, { force: true });
    });

    // The write carried the formatted content.
    const written = JSON.parse(sproutFetchMock.mock.calls[0][1].body).content;
    expect(written).toBe('FORMATTED');

    const buf = ctx().buffers.get(bufferId);
    expect(buf.content).toBe('FORMATTED');
    expect(buf.originalContent).toBe('FORMATTED');
    expect(buf.isModified).toBe(false);
  });

  it('keeps edits typed during the save roundtrip flagged modified', async () => {
    formatSettings.enabled = true;
    formatSettings.formatted = 'FORMATTED';
    const bufferId = await openModifiedBuffer('/w/fmt2.ts', 'unformatted edits');
    let seenBufferId: string | null = null;
    sproutFetchMock.mockImplementation(async (_url, _init) => {
      // User keeps typing while the HTTP roundtrip is in flight.
      seenBufferId = bufferId;
      act(() => {
        ctx().updateBufferContent(bufferId, 'unformatted edits + more');
      });
      return okWriteResponse('/w/fmt2.ts');
    });

    await actAndUpdate(() => {
      ctx().saveBuffer(bufferId, { force: true });
    });

    expect(seenBufferId).toBe(bufferId);
    const buf = ctx().buffers.get(bufferId);
    // The typed text survives; the disk snapshot is the saved content.
    expect(buf.content).toBe('unformatted edits + more');
    expect(buf.originalContent).toBe('FORMATTED');
    expect(buf.isModified).toBe(true);
  });
});
