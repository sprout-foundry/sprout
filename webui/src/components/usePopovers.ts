import { useState, useEffect, useRef } from 'react';

interface UsePopoversReturn {
  showQueuePanel: boolean;
  setShowQueuePanel: (v: boolean | ((prev: boolean) => boolean)) => void;
  showHints: boolean;
  setShowHints: (v: boolean | ((prev: boolean) => boolean)) => void;
  queuePanelRef: React.RefObject<HTMLDivElement>;
}

export function usePopovers(): UsePopoversReturn {
  const [showQueuePanel, setShowQueuePanel] = useState(false);
  const [showHints, setShowHints] = useState(false);
  const queuePanelRef = useRef<HTMLDivElement>(null);

  // Click-outside handler for the queue panel popover
  useEffect(() => {
    if (!showQueuePanel) return;
    const handleClickOutside = (e: MouseEvent) => {
      if (queuePanelRef.current && !queuePanelRef.current.contains(e.target as Node)) {
        setShowQueuePanel(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [showQueuePanel]);

  // Click-outside handler for the hints popover
  useEffect(() => {
    if (!showHints) return;
    const handleClickOutside = (e: MouseEvent) => {
      const target = e.target as HTMLElement;
      if (!target.closest('.hints-popover') && !target.closest('.hints-button')) {
        setShowHints(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [showHints]);

  return {
    showQueuePanel,
    setShowQueuePanel,
    showHints,
    setShowHints,
    queuePanelRef,
  };
}
