import { useState, useRef, useCallback, useLayoutEffect, useEffect } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent, RefObject } from 'react';

interface UseInputHandlingOptions {
  value: string;
  onChange?: (v: string) => void;
  inputRef: RefObject<HTMLTextAreaElement | null>;
  attachedImageCount?: number;
}

export function useInputHandling({ value, onChange, inputRef, attachedImageCount = 0 }: UseInputHandlingOptions) {
  const mountedRef = useRef(true);
  const [draftValue, setDraftValue] = useState(value);
  const selectionRef = useRef<{ start: number; end: number } | null>(null);
  const isComposingRef = useRef(false);

  const updateValue = useCallback(
    (nextValue: string, selection?: { start: number; end: number }) => {
      if (selection) {
        selectionRef.current = selection;
      }
      setDraftValue(nextValue);
      onChange?.(nextValue);
    },
    [onChange],
  );

  // Cleanup effect for mounted ref
  useEffect(() => {
    return () => {
      mountedRef.current = false;
    };
  }, []);

  // Sync external value prop changes to draftValue
  useEffect(() => {
    if (!mountedRef.current) return;
    if (value === draftValue) {
      return;
    }

    const isFocused = document.activeElement === inputRef.current;
    if (!isFocused) {
      setDraftValue(value);
      return;
    }

    if (value === '' || value.startsWith(draftValue)) {
      setDraftValue(value);
    }
  }, [value, draftValue]);

  // Restore selection after render
  useLayoutEffect(() => {
    if (!inputRef.current || !selectionRef.current) return;
    if (document.activeElement !== inputRef.current) return;

    const { start, end } = selectionRef.current;
    inputRef.current.setSelectionRange(Math.min(start, draftValue.length), Math.min(end, draftValue.length));
  }, [draftValue]);

  // Auto-resize textarea
  useLayoutEffect(() => {
    const textarea = inputRef.current;
    if (!textarea) return;

    textarea.style.height = '0px';
    const computed = window.getComputedStyle(textarea);
    const lineHeight = Number.parseFloat(computed.lineHeight) || 24;
    const minHeight = lineHeight * 2 + 20;
    const maxHeight = lineHeight * 10 + 20;
    const nextHeight = Math.min(maxHeight, Math.max(minHeight, textarea.scrollHeight));
    textarea.style.height = `${nextHeight}px`;
  }, [draftValue, attachedImageCount]);

  // Track upcoming selection for keyboard navigation
  const trackUpcomingSelection = useCallback(
    (e: ReactKeyboardEvent<HTMLTextAreaElement>) => {
      const textarea = inputRef.current;
      if (!textarea) {
        return;
      }

      const start = textarea.selectionStart ?? 0;
      const end = textarea.selectionEnd ?? start;

      if (!e.ctrlKey && !e.metaKey && !e.altKey && e.key.length === 1) {
        const next = start + 1;
        selectionRef.current = { start: next, end: next };
        return;
      }

      switch (e.key) {
        case 'Backspace': {
          const next = start === end ? Math.max(0, start - 1) : start;
          selectionRef.current = { start: next, end: next };
          return;
        }
        case 'Delete':
          selectionRef.current = { start, end: start };
          return;
        case 'ArrowLeft': {
          const next = start === end ? Math.max(0, start - 1) : start;
          selectionRef.current = { start: next, end: next };
          return;
        }
        case 'ArrowRight': {
          const next = start === end ? Math.min(draftValue.length, end + 1) : end;
          selectionRef.current = { start: next, end: next };
          return;
        }
        case 'Home':
          selectionRef.current = { start: 0, end: 0 };
          return;
        case 'End': {
          const next = draftValue.length;
          selectionRef.current = { start: next, end: next };
          return;
        }
      }
    },
    [draftValue.length],
  );

  const setSelection = useCallback((start: number, end: number) => {
    selectionRef.current = { start, end };
  }, []);

  const handleTabCompletion = () => {
    // Basic auto-completion logic could be added here
    // For now, just insert a tab character
    const textarea = inputRef.current;
    if (!textarea) return;

    const start = textarea.selectionStart;
    const end = textarea.selectionEnd;
    const newInput = `${draftValue.substring(0, start)}\t${draftValue.substring(end)}`;
    updateValue(newInput, { start: start + 1, end: start + 1 });
  };

  const handleCompositionStart = () => {
    isComposingRef.current = true;
  };

  const handleCompositionEnd = () => {
    isComposingRef.current = false;
  };

  return {
    draftValue,
    isComposingRef,
    updateValue,
    trackUpcomingSelection,
    handleTabCompletion,
    handleCompositionStart,
    handleCompositionEnd,
    setSelection,
  };
}
