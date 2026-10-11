import type { EditorBuffer } from '../types/editor';
import { showThemedConfirm } from './ThemedDialog';

/**
 * Close every closable, unpinned buffer that matches `predicate`, asking once
 * before discarding unsaved changes. Shared by the tab context menu and the
 * close-all / close-others commands.
 */
export async function closeBuffersMatching(
  buffers: () => Map<string, EditorBuffer>,
  closeBuffer: (bufferId: string) => void | Promise<void>,
  predicate: (buffer: EditorBuffer) => boolean,
): Promise<void> {
  const targets = () =>
    Array.from(buffers().values()).filter(
      (buffer) => buffer.isClosable !== false && !buffer.isPinned && predicate(buffer),
    );
  const modifiedTargets = targets().filter((buffer) => buffer.isModified);

  if (modifiedTargets.length > 0) {
    const names = modifiedTargets.map((b) => b.file.name).join(', ');
    const display = names.length > 60 ? `${names.slice(0, 60)}… and ${modifiedTargets.length - 1} more` : names;
    const ok = await showThemedConfirm(`You have unsaved changes in:\n${display}\n\nClose all without saving?`, {
      title: 'Unsaved changes',
      type: 'danger',
      confirmLabel: 'Close without saving',
    });
    if (!ok) return;
  }

  // Re-read at the moment of confirm so the snapshot reflects the latest
  // buffer state (a file might have been saved while the dialog was open).
  targets().forEach((buffer) => void closeBuffer(buffer.id));
}
