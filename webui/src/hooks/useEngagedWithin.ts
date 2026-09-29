import { type RefObject, useEffect, useState } from 'react';

/**
 * Whether the user's last click or keyboard focus landed inside ref's
 * element. Clicks count as well as focus: selecting chat text or clicking an
 * empty pane moves no keyboard focus, but it is still working there.
 */
export function useEngagedWithin(ref: RefObject<HTMLElement | null>): boolean {
  const [engaged, setEngaged] = useState(true);
  useEffect(() => {
    const update = (e: Event) => {
      const root = ref.current;
      if (!root || !(e.target instanceof Node)) return;
      setEngaged(root.contains(e.target));
    };
    document.addEventListener('pointerdown', update, true);
    document.addEventListener('focusin', update, true);
    return () => {
      document.removeEventListener('pointerdown', update, true);
      document.removeEventListener('focusin', update, true);
    };
  }, [ref]);
  return engaged;
}
