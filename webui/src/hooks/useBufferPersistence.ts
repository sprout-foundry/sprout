import { useCallback } from 'react';
import type { MutableRefObject } from 'react';
import type { EditorBuffer } from '../types/editor';
import type { Dispatch, SetStateAction } from 'react';
import { writeFileWithConsent } from '../services/fileAccess';
import { showThemedPrompt } from '../components/ThemedDialog';
import { useLog, warn, debugLog } from '../utils/log';

interface UseBufferPersistenceParams {
  buffersRef: MutableRefObject<Map<string, EditorBuffer>>;
  setBuffers: Dispatch<SetStateAction<Map<string, EditorBuffer>>>;
}

/**
 * File-save (persistence) operations for buffers.
 * Provides `saveBuffer` and `saveAllBuffers`.
 */
export function useBufferPersistence({ buffersRef, setBuffers }: UseBufferPersistenceParams) {
  const log = useLog();

  // Save a buffer to the server
  const saveBuffer = useCallback(
    async (bufferId: string, options?: { silent?: boolean }) => {
      const buffer = buffersRef.current.get(bufferId);
      if (!buffer || buffer.kind !== 'file') return;

      const silent = options?.silent ?? false;

      // Handle virtual workspace buffers (untitled files created via Ctrl+N)
      if (buffer.file.path.startsWith('__workspace/')) {
        const filePath = await showThemedPrompt('Enter a file path for the new file:', {
          title: 'Save As',
          defaultValue: 'untitled',
          placeholder: 'path/to/file.ts',
        });

        if (!filePath || !filePath.trim()) {
          return; // User cancelled
        }

        const trimmedPath = filePath.trim();

        // Write the file to disk
        const response = await writeFileWithConsent(trimmedPath, buffer.content);
        if (!response.ok) {
          const errorText = await response.text().catch((err) => { debugLog('[saveNewFile] failed to read error response body:', err); return response.statusText; });
          throw new Error(errorText || `Failed to save file: ${response.statusText}`);
        }

        // Update the buffer path to the real file path
        const ext = trimmedPath.includes('.') ? trimmedPath.split('.').pop() : '';
        const name = trimmedPath.split('/').pop() || trimmedPath;

        setBuffers((prev) => {
          const next = new Map(prev);
          const buf = next.get(bufferId);
          if (buf) {
            next.set(bufferId, {
              ...buf,
              file: {
                ...buf.file,
                name,
                path: trimmedPath,
                ext: ext || undefined,
              },
              originalContent: buf.content,
              isModified: false,
            });
          }
          return next;
        });
        if (!silent) {
          log.success(`${trimmedPath} saved successfully`, { title: 'File Saved', duration: 3000 });
        }
        return;
      }

      // Normal save for existing files
      try {
        const response = await writeFileWithConsent(buffer.file.path, buffer.content);

        if (response.ok) {
          const data = await response.json();
          // Check for validation errors (hotkeys config)
          if (data.success === false) {
            throw new Error(data.error || 'Save validation failed');
          }
          // Check for success message
          if (data.message === 'File saved successfully' || data.success === true) {
            setBuffers((prev) => {
              const next = new Map(prev);
              const buf = next.get(bufferId);
              if (buf) {
                next.set(bufferId, { ...buf, originalContent: buf.content, isModified: false });
              }
              return next;
            });
            if (!silent) {
              log.success(`${buffer.file.path} saved successfully`, { title: 'File Saved', duration: 3000 });
            }
          }
        } else {
          // Server returned a non-2xx status (e.g., 400 validation error).
          const errorBody = await response.text().catch((err) => { debugLog('[saveBuffer] failed to read error response body:', err); return 'Unknown error'; });
          throw new Error(`Save failed (${response.status}): ${errorBody}`);
        }
      } catch (error) {
        const msg = error instanceof Error ? error.message : 'Failed to save buffer';
        log.error(msg, { title: 'Save Error' });
        throw error;
      }
    },
    [buffersRef, setBuffers, log],
  );

  // Save all modified buffers
  const saveAllBuffers = useCallback(
    async (options?: { silent?: boolean }) => {
      const currentBuffers = buffersRef.current;
      const savePromises = Array.from(currentBuffers.entries())
        .filter(([_, buffer]) => buffer.isModified && !buffer.file.path.startsWith('__workspace/'))
        .map(([bufferId, _]) => saveBuffer(bufferId, options).catch((err) => { warn(`Skipping failed buffer save in saveAllBuffers [${bufferId}]: ${err instanceof Error ? err.message : String(err)}`); return undefined; }));

      await Promise.all(savePromises);
    },
    [buffersRef, saveBuffer],
  );

  return { saveBuffer, saveAllBuffers };
}
