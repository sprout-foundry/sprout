import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react';

/**
 * Whether an element is currently narrower than `maxWidth` pixels. Follows
 * the ref to whichever element it points at after each render, since the
 * element can be swapped (e.g. the editor behind a preview toggle).
 */
export function useIsNarrow(ref: RefObject<HTMLElement>, maxWidth: number): boolean {
  const [narrow, setNarrow] = useState(false);
  const observed = useRef<{ el: HTMLElement; observer: ResizeObserver | null } | null>(null);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || observed.current?.el === el) return;
    observed.current?.observer?.disconnect();
    const update = () => {
      const width = el.getBoundingClientRect().width;
      // Unlaid-out (hidden) elements measure 0; keep the last answer.
      if (width > 0) setNarrow(width < maxWidth);
    };
    update();
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(update);
    observer?.observe(el);
    observed.current = { el, observer };
  });

  useEffect(
    () => () => {
      observed.current?.observer?.disconnect();
      observed.current = null;
    },
    [],
  );

  return narrow;
}
