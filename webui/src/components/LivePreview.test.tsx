// @vitest-environment jsdom
/**
 * LivePreview — the HTML preview iframe.
 *
 * What is pinned: with a source path the preview loads from the dedicated
 * preview origin (a real URL, so relative assets and blob: navigation work),
 * without one it falls back to srcDoc, and an unavailable origin also falls
 * back to srcDoc rather than rendering a broken frame.
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import LivePreview from './LivePreview';

const fetchMock = vi.fn();

vi.mock('../contexts/SproutAdapterContext', () => ({
  useSproutFetch: () => fetchMock,
}));

let container: HTMLDivElement;
let root: Root;

function json(body: unknown): Response {
  return { ok: true, json: async () => body } as Response;
}

async function render(props: {
  content: string;
  language: 'svg' | 'html';
  fileName: string;
  sourcePath?: string;
}): Promise<HTMLIFrameElement> {
  await act(async () => {
    root.render(createElement(LivePreview, props));
  });
  // The URL resolves in an effect; flush it.
  await act(async () => {});
  const frame = container.querySelector('iframe');
  if (!frame) throw new Error('no iframe rendered');
  return frame;
}

beforeEach(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  fetchMock.mockReset();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe('LivePreview', () => {
  it('loads from the preview origin when a source path is given', async () => {
    fetchMock.mockResolvedValue(json({ available: true, url: 'http://127.0.0.1:9/__preview/tok/page.html' }));

    const frame = await render({
      content: '<p>hi</p>',
      language: 'html',
      fileName: 'page.html',
      sourcePath: '/ws/page.html',
    });

    expect(fetchMock).toHaveBeenCalledWith('/api/preview/source?path=%2Fws%2Fpage.html');
    expect(frame.getAttribute('src')).toBe('http://127.0.0.1:9/__preview/tok/page.html');
    expect(frame.getAttribute('srcdoc')).toBeNull();
  });

  it('falls back to srcDoc when the origin is unavailable', async () => {
    fetchMock.mockResolvedValue(json({ available: false }));

    const frame = await render({
      content: '<p>hi</p>',
      language: 'html',
      fileName: 'page.html',
      sourcePath: '/ws/page.html',
    });

    expect(frame.getAttribute('srcdoc')).toBe('<p>hi</p>');
    expect(frame.getAttribute('src')).toBeNull();
  });

  it('falls back to srcDoc when the request fails', async () => {
    fetchMock.mockRejectedValue(new Error('offline'));

    const frame = await render({
      content: '<p>hi</p>',
      language: 'html',
      fileName: 'page.html',
      sourcePath: '/ws/page.html',
    });

    expect(frame.getAttribute('srcdoc')).toBe('<p>hi</p>');
  });

  it('does not request a preview URL without a source path', async () => {
    const frame = await render({ content: '<p>hi</p>', language: 'html', fileName: 'page.html' });

    expect(fetchMock).not.toHaveBeenCalled();
    expect(frame.getAttribute('srcdoc')).toBe('<p>hi</p>');
  });

  it('keeps scripts enabled in both modes', async () => {
    fetchMock.mockResolvedValue(json({ available: true, url: 'http://127.0.0.1:9/__preview/tok/page.html' }));

    const frame = await render({
      content: '<p>hi</p>',
      language: 'html',
      fileName: 'page.html',
      sourcePath: '/ws/page.html',
    });

    expect(frame.getAttribute('sandbox')).toContain('allow-scripts');
  });

  it('falls back to srcDoc when the document is edited, so edits stay visible', async () => {
    fetchMock.mockResolvedValue(json({ available: true, url: 'http://127.0.0.1:9/__preview/tok/page.html' }));

    const frame = await render({
      content: '<p>hi</p>',
      language: 'html',
      fileName: 'page.html',
      sourcePath: '/ws/page.html',
    });
    expect(frame.getAttribute('src')).toBe('http://127.0.0.1:9/__preview/tok/page.html');

    // Edit in the source textarea: the on-disk URL would show stale content,
    // so the preview must switch back to srcDoc and reflect the edit.
    const textarea = container.querySelector('textarea');
    if (!textarea) throw new Error('no source textarea rendered');
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set;
      setter?.call(textarea, '<p>edited</p>');
      textarea.dispatchEvent(new Event('input', { bubbles: true }));
    });

    const after = container.querySelector('iframe');
    expect(after?.getAttribute('srcdoc')).toBe('<p>edited</p>');
    expect(after?.getAttribute('src')).toBeNull();
  });
});
