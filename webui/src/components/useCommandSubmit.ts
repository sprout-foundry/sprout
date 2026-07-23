import { useCallback } from 'react';
import type { FormEvent } from 'react';
import { showThemedConfirm } from './ThemedDialog';
import type { AttachedImage } from './useImageUpload';

interface UseCommandSubmitOptions {
  draftValue: string;
  updateValue: (val: string, sel?: { start: number; end: number }) => void;
  attachedImages: AttachedImage[];
  clearImages: () => void;
  isProcessing: boolean;
  inputRef: React.RefObject<HTMLTextAreaElement | null>;
  saveToHistory: (cmd: string) => Promise<void>;
  resetHistoryNavigation: () => void;
  onSend?: (command: string) => void;
  onSendCommand?: (command: string) => void;
  onQueue?: (command: string) => void;
  isComposingRef: React.RefObject<boolean>;
  disabled: boolean;
}

interface UseCommandSubmitReturn {
  handleSend: () => Promise<void>;
  handleQueue: () => Promise<void>;
  handleNewSession: () => Promise<void>;
  handleSubmit: (e: FormEvent<HTMLFormElement>) => void;
  canSend: boolean;
}

export function useCommandSubmit({
  draftValue,
  updateValue,
  attachedImages,
  clearImages,
  isProcessing,
  inputRef,
  saveToHistory,
  resetHistoryNavigation,
  onSend,
  onSendCommand,
  onQueue,
  isComposingRef,
  disabled,
}: UseCommandSubmitOptions): UseCommandSubmitReturn {
  const buildCommandWithImages = useCallback(
    (baseCommand: string) => {
      const trimmed = baseCommand.trim();
      const uploadedImages = attachedImages.filter((img) => img.uploadedPath);
      if (uploadedImages.length === 0) return trimmed;
      const imagePaths = uploadedImages.map((img) => `Pasted image saved to disk: ${img.uploadedPath}`).join('\n');
      return `${imagePaths}\n\n${trimmed}`;
    },
    [attachedImages],
  );

  const resetAndFocus = useCallback(() => {
    updateValue('', { start: 0, end: 0 });
    setTimeout(() => {
      if (inputRef.current) {
        inputRef.current.focus();
      }
    }, 100);
  }, [updateValue, inputRef]);

  // Helper to call onSend/onSendCommand with a command string.
  // Prefers onSendCommand (the dedicated /api/command/execute surface)
  // over onSend (/api/query) so that SteerCapable slash commands like
  // /clear (invoked from handleNewSession) bypass the WebUI safety gate.
  // Only falls back to onSend when no dedicated handler is wired up.
  const commandRef = useCallback(
    async (command: string) => {
      resetHistoryNavigation();

      if (onSendCommand) {
        onSendCommand(command);
      } else if (onSend) {
        onSend(command);
      }

      // Clear textarea and focus back to input
      resetAndFocus();
    },
    [onSendCommand, onSend, resetHistoryNavigation, resetAndFocus],
  );

  const handleSend = async () => {
    const textareaValue = draftValue;
    if (textareaValue.trim() === '') return;

    // Build query with image paths
    const commandToSend = buildCommandWithImages(textareaValue);

    // Reset history navigation
    resetHistoryNavigation();

    // Call the appropriate send handler
    if (onSend) {
      onSend(commandToSend);
    } else if (onSendCommand) {
      onSendCommand(commandToSend);
    }

    void saveToHistory(commandToSend);

    // Clear attached images and revoke URLs
    clearImages();

    // Clear textarea and focus back to input
    resetAndFocus();
  };

  const handleQueue = async () => {
    if (isComposingRef.current) return;
    const textareaValue = draftValue;
    if (textareaValue.trim() === '') return;

    // Build query with image paths
    const commandToQueue = buildCommandWithImages(textareaValue);

    resetHistoryNavigation();
    onQueue?.(commandToQueue);
    void saveToHistory(commandToQueue);

    // Clear attached images and revoke URLs
    clearImages();

    // Clear textarea and focus back to input
    resetAndFocus();
  };

  const handleNewSession = useCallback(async () => {
    if (isComposingRef.current) return;
    if (isProcessing) {
      const confirmed = await showThemedConfirm('A request is currently processing. Stop it and start a new session?', {
        type: 'warning',
      });
      if (!confirmed) {
        return;
      }
      commandRef('/clear');
      return;
    }
    commandRef('/clear');
  }, [isProcessing, commandRef]);

  const canSend = !!draftValue.trim() && !attachedImages.some((img) => !img.uploadedPath && !img.error);

  const handleSubmit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!canSend || disabled || isComposingRef.current) {
      return;
    }
    void handleSend();
  };

  return {
    handleSend,
    handleQueue,
    handleNewSession,
    handleSubmit,
    canSend,
  };
}
