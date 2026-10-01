import { useEffect, type RefObject } from 'react';

const FOCUS_COMPOSER_EVENT = 'sprout:focus-composer';

// The composer only autofocuses on mount; a chat opened from the sidebar is
// often already mounted, and the sidebar click keeps focus otherwise.
export function requestComposerFocus(chatId: string): void {
  window.dispatchEvent(new CustomEvent(FOCUS_COMPOSER_EVENT, { detail: { chatId } }));
}

export function useComposerFocusRequest(containerRef: RefObject<HTMLElement | null>, chatId: string | undefined) {
  useEffect(() => {
    let frame = 0;
    const onRequest = (e: Event) => {
      if ((e as CustomEvent<{ chatId: string }>).detail?.chatId !== (chatId ?? 'default')) return;
      cancelAnimationFrame(frame);
      // Wait a frame so a just-switched buffer is visible and can take focus.
      frame = requestAnimationFrame(() => {
        const input = containerRef.current?.querySelector<HTMLTextAreaElement>('textarea:not([disabled])');
        if (input && input.getClientRects().length > 0) input.focus();
      });
    };
    window.addEventListener(FOCUS_COMPOSER_EVENT, onRequest);
    return () => {
      window.removeEventListener(FOCUS_COMPOSER_EVENT, onRequest);
      cancelAnimationFrame(frame);
    };
  }, [containerRef, chatId]);
}
