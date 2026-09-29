import { act, createElement, useRef } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { requestComposerFocus, useComposerFocusRequest } from './useComposerFocusRequest';

let container: HTMLDivElement;
let root: Root;

function Composer({ chatId }: { chatId?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  useComposerFocusRequest(ref, chatId);
  return createElement('div', { ref }, createElement('textarea', { id: `input-${chatId ?? 'default'}` }));
}

beforeEach(() => {
  (globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true;
  vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
    cb(0);
    return 1;
  });
  vi.spyOn(Element.prototype, 'getClientRects').mockReturnValue([{}] as unknown as DOMRectList);
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

function mountTwoChats() {
  act(() =>
    root.render(
      createElement('div', null, createElement(Composer, { chatId: 'a' }), createElement(Composer, { chatId: 'b' })),
    ),
  );
}

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.restoreAllMocks();
});

describe('useComposerFocusRequest', () => {
  it('focuses only the requested chat composer', () => {
    mountTwoChats();
    act(() => requestComposerFocus('b'));
    expect(document.activeElement?.id).toBe('input-b');
    act(() => requestComposerFocus('a'));
    expect(document.activeElement?.id).toBe('input-a');
  });

  it('leaves focus alone when the composer is hidden', () => {
    mountTwoChats();
    vi.mocked(Element.prototype.getClientRects).mockReturnValue([] as unknown as DOMRectList);
    act(() => requestComposerFocus('a'));
    expect(document.activeElement).toBe(document.body);
  });
});
