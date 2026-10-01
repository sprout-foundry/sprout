import { type RefObject, useEffect, useState } from 'react';

const OVERLAY_SELECTOR = '[role="dialog"], [role="alertdialog"], [role="menu"], .context-menu, .themed-dialog-overlay';

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
      // Menus and dialogs float above the page, outside every region;
      // using one (a tab's menu, a confirm) doesn't move you anywhere.
      const el = e.target instanceof Element ? e.target : e.target.parentElement;
      if (el?.closest(OVERLAY_SELECTOR)) return;
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
