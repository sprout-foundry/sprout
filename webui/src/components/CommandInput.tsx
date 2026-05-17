import { useRef, useEffect, memo } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent } from 'react';
import { useLog } from '../utils/log';
import './CommandInput.css';
import { CommandInputActions } from './CommandInputActions';
import { CommandInputHeader } from './CommandInputHeader';
import { ImagePreviewPanel } from './ImagePreviewPanel';
import { useCommandHistory } from './useCommandHistory';
import { useCommandSubmit } from './useCommandSubmit';
import { useImageUpload } from './useImageUpload';
import { useIndexToggle } from './useIndexToggle';
import { useInputHandling } from './useInputHandling';
import { usePopovers } from './usePopovers';

interface CommandInputProps {
  value?: string;
  onChange?: (value: string) => void;
  onSend?: (command: string) => void;
  onSendCommand?: (command: string) => void;
  onQueue?: (command: string) => void;
  placeholder?: string;
  disabled?: boolean;
  isConnected?: boolean;
  multiline?: boolean;
  autoFocus?: boolean;
  isProcessing?: boolean;
  queuedCount?: number;
  onStop?: () => void;
  queuedMessages?: string[];
  onQueueMessageRemove?: (index: number) => void;
  onQueueMessageEdit?: (index: number, newText: string) => void;
  onQueueReorder?: (fromIndex: number, toIndex: number) => void;
  onClearQueuedMessages?: () => void;
  /** Whether embedding indexing is enabled for the workspace */
  isIndexEnabled?: boolean;
  /** Whether indexing is currently building */
  isIndexBuilding?: boolean;
  /** Callback to toggle indexing on/off */
  onToggleIndex?: (enabled: boolean) => void;
}

function CommandInput({
  value = '',
  onChange,
  onSend,
  onSendCommand,
  onQueue,
  placeholder = 'Ask me anything about your code...',
  disabled = false,
  isConnected = true,
  multiline = true,
  autoFocus = false,
  isProcessing = false,
  queuedCount = 0,
  onStop,
  queuedMessages = [],
  onQueueMessageRemove,
  onQueueMessageEdit,
  onQueueReorder,
  onClearQueuedMessages,
  isIndexEnabled = false,
  isIndexBuilding = false,
  onToggleIndex,
}: CommandInputProps): JSX.Element {
  const log = useLog();
  const inputRef = useRef<HTMLTextAreaElement>(null);

  // Image upload hook
  const {
    attachedImages,
    previewImageId,
    setPreviewImageId,
    previewImage,
    handlePaste,
    handleUploadClick,
    removeImage,
    fileInputRef,
    clearImages,
    handleFileSelect,
  } = useImageUpload({ inputRef });

  // Input handling hook
  const {
    draftValue,
    isComposingRef,
    updateValue,
    trackUpcomingSelection,
    handleTabCompletion,
    handleCompositionStart,
    handleCompositionEnd,
    setSelection,
  } = useInputHandling({ value, onChange, inputRef, attachedImageCount: attachedImages.length });

  // Command history hook
  const {
    history,
    isHistoryMode,
    isLoadingHistory,
    loadHistory,
    saveToHistory,
    resetHistoryNavigation,
    navigateHistory,
  } = useCommandHistory({ log, draftValue, updateValue });

  // Index toggle hook
  const { effectiveIndexEnabled, handleToggleIndexClick } = useIndexToggle({
    isIndexEnabled,
    onToggleIndex,
  });

  // Command submit hook
  const { handleSend, handleQueue, handleNewSession, handleSubmit, canSend } = useCommandSubmit({
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
  });

  // Popovers hook
  const { showQueuePanel, setShowQueuePanel, showHints, setShowHints, queuePanelRef } = usePopovers();

  // Focus input if autoFocus is true
  useEffect(() => {
    if (autoFocus && inputRef.current) {
      inputRef.current.focus();
    }
  }, [autoFocus, inputRef]);

  // Load history on mount
  useEffect(() => {
    loadHistory();
  }, [loadHistory]);

  const handleKeyDown = (e: ReactKeyboardEvent) => {
    if (disabled) return;
    const textarea = inputRef.current;
    if (!textarea) return;

    trackUpcomingSelection(e as ReactKeyboardEvent<HTMLTextAreaElement>);

    switch (e.key) {
      case 'ArrowUp': {
        const shouldNavigateHistory =
          !e.altKey &&
          !e.ctrlKey &&
          !e.metaKey &&
          !e.shiftKey &&
          (isHistoryMode || draftValue.length === 0) &&
          (isHistoryMode || (textarea.selectionStart === 0 && textarea.selectionEnd === 0));

        if (!shouldNavigateHistory) {
          break;
        }
        e.preventDefault();
        navigateHistory(1);
        break;
      }
      case 'ArrowDown': {
        const shouldNavigateHistory =
          !e.altKey &&
          !e.ctrlKey &&
          !e.metaKey &&
          !e.shiftKey &&
          isHistoryMode &&
          textarea.selectionStart === draftValue.length &&
          textarea.selectionEnd === draftValue.length;

        if (!shouldNavigateHistory) {
          break;
        }
        e.preventDefault();
        navigateHistory(-1);
        break;
      }
      case 'Tab':
        e.preventDefault();
        handleTabCompletion();
        break;
      case 'Enter':
        if (multiline) {
          if (e.shiftKey) {
            e.preventDefault();
            const autoTextarea = inputRef.current;
            if (!autoTextarea) return;
            const start = autoTextarea.selectionStart;
            const end = autoTextarea.selectionEnd;
            const currentValue = draftValue;
            const newValue = `${currentValue.substring(0, start)}\n${currentValue.substring(end)}`;
            updateValue(newValue, { start: start + 1, end: start + 1 });
            setTimeout(() => {
              if (inputRef.current) {
                inputRef.current.setSelectionRange(start + 1, start + 1);
              }
            }, 0);
          } else {
            if (isComposingRef.current) {
              return;
            }
            e.preventDefault();
            void handleSend();
          }
        } else {
          if (isComposingRef.current) {
            return;
          }
          e.preventDefault();
          void handleSend();
        }
        break;
      case 'Escape':
        e.preventDefault();
        if (isHistoryMode) {
          // Restore temp input and exit history mode
          resetHistoryNavigation();
          updateValue(history.tempInput, {
            start: history.tempInput.length,
            end: history.tempInput.length,
          });
        } else {
          // Clear input if not in history mode
          resetHistoryNavigation();
          updateValue('', { start: 0, end: 0 });
        }
        break;
    }

    // Reset history navigation when user starts typing or deleting content
    if ((e.key.length === 1 || e.key === 'Backspace' || e.key === 'Delete') && isHistoryMode) {
      resetHistoryNavigation();
    }
  };

  return (
    <form className="command-input" onSubmit={handleSubmit}>
      <CommandInputHeader
        isHistoryMode={isHistoryMode}
        isLoadingHistory={isLoadingHistory}
        historyIndex={history.index}
        historyLength={history.commands.length}
        draftValueLength={draftValue.length}
        tempInput={history.tempInput}
        resetHistoryNavigation={resetHistoryNavigation}
        updateValue={updateValue}
        showHints={showHints}
        setShowHints={setShowHints}
      />

      <textarea
        ref={inputRef}
        value={draftValue}
        onChange={(e) => {
          const newValue = e.target.value;
          updateValue(newValue, {
            start: e.target.selectionStart,
            end: e.target.selectionEnd,
          });
        }}
        onSelect={(e) => {
          const target = e.target as HTMLTextAreaElement;
          setSelection(target.selectionStart, target.selectionEnd);
        }}
        onKeyDown={handleKeyDown}
        onPaste={handlePaste}
        onCompositionStart={handleCompositionStart}
        onCompositionEnd={handleCompositionEnd}
        placeholder={placeholder}
        disabled={disabled}
        className={`input-field autoscaling ${isHistoryMode ? 'history-mode' : ''}`}
        rows={2}
        spellCheck={false}
        data-testid="command-input"
      />

      <ImagePreviewPanel
        attachedImages={attachedImages}
        previewImageId={previewImageId}
        setPreviewImageId={setPreviewImageId}
        previewImage={previewImage}
        removeImage={removeImage}
      />

      <CommandInputActions
        effectiveIndexEnabled={effectiveIndexEnabled}
        isIndexBuilding={isIndexBuilding}
        disabled={disabled}
        isConnected={isConnected}
        isProcessing={isProcessing}
        canSend={canSend}
        onToggleIndex={onToggleIndex}
        onToggleIndexClick={handleToggleIndexClick}
        handleUploadClick={handleUploadClick}
        handleNewSession={handleNewSession}
        handleSubmit={handleSubmit}
        handleQueue={handleQueue}
        onStop={onStop}
        onQueue={onQueue}
        showQueuePanel={showQueuePanel}
        setShowQueuePanel={setShowQueuePanel}
        queuePanelRef={queuePanelRef}
        queuedCount={queuedCount}
        queuedMessages={queuedMessages}
        onQueueMessageRemove={onQueueMessageRemove}
        onQueueMessageEdit={onQueueMessageEdit}
        onQueueReorder={onQueueReorder}
        onClearQueuedMessages={onClearQueuedMessages}
        fileInputRef={fileInputRef}
        handleFileSelect={handleFileSelect}
      />
    </form>
  );
}

// Memoize to prevent unnecessary re-renders that cause cursor jumping
const MemoizedCommandInput = memo(CommandInput);
MemoizedCommandInput.displayName = 'CommandInput';

export default MemoizedCommandInput;
