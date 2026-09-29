import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { highlightExcerpt } from './highlightExcerpt';

describe('highlightExcerpt', () => {
  it('marks searched terms and keeps other brackets as text', () => {
    const { container } = render(<span>{highlightExcerpt('reply with [first]-convo [executing tool]', 'FIRST')}</span>);
    expect(container.textContent).toBe('reply with first-convo [executing tool]');
    expect(Array.from(container.querySelectorAll('mark')).map((m) => m.textContent)).toEqual(['first']);
  });

  it('leaves the excerpt alone without a query', () => {
    const { container } = render(<span>{highlightExcerpt('a [b] c', '')}</span>);
    expect(container.textContent).toBe('a [b] c');
  });
});
