import { useState, useEffect, useCallback } from 'react';

interface UseIndexToggleOptions {
  isIndexEnabled?: boolean;
  onToggleIndex?: (enabled: boolean) => void;
}

interface UseIndexToggleReturn {
  effectiveIndexEnabled: boolean;
  handleToggleIndexClick: () => void;
}

export function useIndexToggle({ isIndexEnabled = false, onToggleIndex }: UseIndexToggleOptions): UseIndexToggleReturn {
  // Optimistic state for the index toggle — provides immediate visual feedback
  // while waiting for the stats poll to confirm. Reset whenever the prop changes.
  const [optimisticIndexEnabled, setOptimisticIndexEnabled] = useState<boolean | null>(null);
  const effectiveIndexEnabled = optimisticIndexEnabled !== null ? optimisticIndexEnabled : isIndexEnabled;

  // Sync optimistic state back to prop when it catches up
  useEffect(() => {
    if (optimisticIndexEnabled !== null && optimisticIndexEnabled === isIndexEnabled) {
      setOptimisticIndexEnabled(null);
    }
  }, [optimisticIndexEnabled, isIndexEnabled]);

  const handleToggleIndexClick = useCallback(() => {
    const next = !effectiveIndexEnabled;
    setOptimisticIndexEnabled(next);
    onToggleIndex?.(next);
  }, [effectiveIndexEnabled, onToggleIndex]);

  return {
    effectiveIndexEnabled,
    handleToggleIndexClick,
  };
}
