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
  // Helper to call onSend/onSendCommand with a command string
  const commandRef = useCallback(
    async (command: string) => {
      resetHistoryNavigation();

      if (onSend) {
        onSend(command);
      } else if (onSendCommand) {
        onSendCommand(command);
      }

      updateValue('', { start: 0, end: 0 });

      setTimeout(() => {
        if (inputRef.current) {
          inputRef.current.focus();
        }
      }, 100);
    },
    [onSend, onSendCommand, updateValue, resetHistoryNavigation, inputRef],
  );

  const handleSend = async () => {
    const textareaValue = draftValue;
    if (textareaValue.trim() === '') return;

    // Build query with image paths
    let commandToSend = textareaValue.trim();
    const uploadedImages = attachedImages.filter((img) => img.uploadedPath);
    if (uploadedImages.length > 0) {
      const imagePaths = uploadedImages.map((img) => `Pasted image saved to disk: ${img.uploadedPath}`).join('\n');
      commandToSend = `${imagePaths}\n\n${commandToSend}`;
    }

    // Reset history navigation
    resetHistoryNavigation();

    // Call the appropriate send handler
    if (onSend) {
      onSend(commandToSend);
    } else if (onSendCommand) {
      onSendCommand(commandToSend);
    }

    void saveToHistory(commandToSend);

    // Clear textarea using onChange for controlled component
    updateValue('', { start: 0, end: 0 });

    // Clear attached images and revoke URLs
    clearImages();

    // Focus back to input
    setTimeout(() => {
      if (inputRef.current) {
        inputRef.current.focus();
      }
    }, 100);
  };

  const handleQueue = async () => {
    const textareaValue = draftValue;
    if (textareaValue.trim() === '') return;

    // Build query with image paths
    let commandToQueue = textareaValue.trim();
    const uploadedImages = attachedImages.filter((img) => img.uploadedPath);
    if (uploadedImages.length > 0) {
      const imagePaths = uploadedImages.map((img) => `Pasted image saved to disk: ${img.uploadedPath}`).join('\n');
      commandToQueue = `${imagePaths}\n\n${commandToQueue}`;
    }

    resetHistoryNavigation();
    onQueue?.(commandToQueue);
    void saveToHistory(commandToQueue);
    updateValue('', { start: 0, end: 0 });

    // Clear attached images and revoke URLs
    clearImages();

    setTimeout(() => {
      if (inputRef.current) {
        inputRef.current.focus();
      }
    }, 100);
  };

  const handleNewSession = useCallback(async () => {
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
