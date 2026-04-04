import { useCallback } from 'react';
import type { EditorBuffer } from '../types/editor';
import type { Dispatch, SetStateAction } from 'react';

/**
 * Pure mutation callbacks for the buffers Map.
 * Each function immutably updates a single buffer field via `setBuffers`.
 */
export function useBufferMutations(
  setBuffers: Dispatch<SetStateAction<Map<string, EditorBuffer>>>,
) {
  const updateBufferContent = useCallback((bufferId: string, content: string) => {
    setBuffers(prev => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, { ...buffer, content, isModified: content !== buffer.originalContent });
      }
      return next;
    });
  }, [setBuffers]);

  const updateBufferCursor = useCallback((bufferId: string, position: { line: number; column: number }) => {
    setBuffers(prev => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, { ...buffer, cursorPosition: position });
      }
      return next;
    });
  }, [setBuffers]);

  const updateBufferScroll = useCallback((bufferId: string, position: { top: number; left: number }) => {
    setBuffers(prev => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, { ...buffer, scrollPosition: position });
      }
      return next;
    });
  }, [setBuffers]);

  const updateBufferMetadata = useCallback((bufferId: string, updates: Record<string, any>) => {
    setBuffers(prev => {
      const buf = prev.get(bufferId);
      if (!buf) return prev;
      const next = new Map(prev);
      next.set(bufferId, { ...buf, metadata: { ...buf.metadata, ...updates } });
      return next;
    });
  }, [setBuffers]);

  const updateBufferTitle = useCallback((bufferId: string, title: string) => {
    setBuffers(prev => {
      const buf = prev.get(bufferId);
      if (!buf) return prev;
      const next = new Map(prev);
      next.set(bufferId, { ...buf, file: { ...buf.file, name: title } });
      return next;
    });
  }, [setBuffers]);

  const setBufferModified = useCallback((bufferId: string, isModified: boolean) => {
    setBuffers(prev => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, { ...buffer, isModified });
      }
      return next;
    });
  }, [setBuffers]);

  // Set the original content baseline for a buffer (e.g., after loading from disk).
  // This also resets isModified to false if the current content matches the new baseline.
  const setBufferOriginalContent = useCallback((bufferId: string, originalContent: string) => {
    setBuffers(prev => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, {
          ...buffer,
          originalContent,
          isModified: buffer.content !== originalContent ? buffer.isModified : false,
        });
      }
      return next;
    });
  }, [setBuffers]);

  // Set or clear the language override for a buffer.
  // Pass null to revert to auto-detection by file extension.
  const setBufferLanguageOverride = useCallback((bufferId: string, languageId: string | null) => {
    setBuffers(prev => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, { ...buffer, languageOverride: languageId });
      }
      return next;
    });
  }, [setBuffers]);

  // Revert a buffer's content back to the last-saved state.
  // After calling this, the EditorPane is responsible for syncing
  // the CodeMirror editor view so the visual content matches.
  const revertBufferToOriginal = useCallback((bufferId: string) => {
    setBuffers(prev => {
      const next = new Map(prev);
      const buf = next.get(bufferId);
      if (!buf || buf.kind !== 'file') return prev;
      next.set(bufferId, {
        ...buf,
        content: buf.originalContent,
        isModified: false,
      });
      return next;
    });
  }, [setBuffers]);

  return {
    updateBufferContent,
    updateBufferCursor,
    updateBufferScroll,
    updateBufferMetadata,
    updateBufferTitle,
    setBufferModified,
    setBufferOriginalContent,
    setBufferLanguageOverride,
    revertBufferToOriginal,
  };
}
