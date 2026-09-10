import { useEffect, useState } from 'react';

/**
 * P4.2: coarse-pointer viewport detection for the mobile peer-buffer topology.
 *
 * Mirrors the CSS breakpoints (index.css): mobile is <768px. Kept as a
 * separate hook (not reuseSidebarState) so the sheet layer does not
 * depend on sidebar state plumbing — EditorWorkspace renders below
 * AppContent's sidebar context in some test harnesses.
 */
export function useIsMobileViewport(): boolean {
  const [isMobile, setIsMobile] = useState(false);

  useEffect(() => {
    if (typeof window === 'undefined' || !window.matchMedia) return undefined;
    const mq = window.matchMedia('(max-width: 768px)');
    const update = () => setIsMobile(mq.matches);
    update();
    if (mq.addEventListener) {
      mq.addEventListener('change', update);
      return () => mq.removeEventListener('change', update);
    }
    // Safari <14 fallback
    mq.addListener(update);
    return () => mq.removeListener(update);
  }, []);

  return isMobile;
}
