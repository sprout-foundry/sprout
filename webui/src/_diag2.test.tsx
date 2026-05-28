import { test, expect } from 'vitest';
import { Copy } from 'lucide-react';

test('what does lucide Copy render with', () => {
  // lucide forwardRef object: inspect its render output's child $$typeof
  const fn = (Copy as any).render || (Copy as any);
  const out = typeof fn === 'function' ? fn({ size: 14 }, null) : null;
  // out is an <svg> element created by lucide's React
  // eslint-disable-next-line no-console
  console.log('svg $$typeof', String(out && (out as any).$$typeof));
  console.log('react.element', String(Symbol.for('react.element')));
  console.log('react.transitional', String(Symbol.for('react.transitional.element')));
  // children of svg:
  const kids = out && (out as any).props && (out as any).props.children;
  const firstKid = Array.isArray(kids) ? kids.flat().find(Boolean) : kids;
  console.log('child $$typeof', String(firstKid && (firstKid as any).$$typeof));
  expect(true).toBe(true);
});
