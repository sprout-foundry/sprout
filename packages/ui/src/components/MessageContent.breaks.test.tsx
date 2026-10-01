import { act, createElement } from 'react';
import { createRoot } from 'react-dom/client';
import { describe, expect, it } from 'vitest';
import MessageContent from './MessageContent';

describe('MessageContent line breaks (real markdown pipeline)', () => {
  it('keeps single line breaks as breaks', () => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    const container = document.createElement('div');
    const root = createRoot(container);
    act(() => {
      root.render(createElement(MessageContent, { content: '1\n2\n3' }));
    });
    expect(container.querySelectorAll('br')).toHaveLength(2);
    act(() => root.unmount());
  });
});
