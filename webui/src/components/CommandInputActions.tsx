import { Plus, SquarePen, Send, Square, ListPlus, Database } from 'lucide-react';
import type { FormEvent, ChangeEvent } from 'react';
import QueuedMessagesPanel from './QueuedMessagesPanel';

interface CommandInputActionsProps {
  effectiveIndexEnabled: boolean;
  isIndexBuilding?: boolean;
  disabled: boolean;
  isConnected: boolean;
  isProcessing: boolean;
  canSend: boolean;
  onToggleIndex?: (enabled: boolean) => void;
  onToggleIndexClick: () => void;
  handleUploadClick: () => void;
  handleNewSession: () => void;
  handleSubmit: (e: FormEvent<HTMLFormElement>) => void;
  handleQueue: () => void;
  onStop?: () => void;
  onQueue?: (command: string) => void;
  showQueuePanel: boolean;
  setShowQueuePanel: (v: boolean | ((prev: boolean) => boolean)) => void;
  queuePanelRef: React.RefObject<HTMLDivElement>;
  queuedCount: number;
  queuedMessages: string[];
  onQueueMessageRemove?: (index: number) => void;
  onQueueMessageEdit?: (index: number, newText: string) => void;
  onQueueReorder?: (fromIndex: number, toIndex: number) => void;
  onClearQueuedMessages?: () => void;
  fileInputRef: React.RefObject<HTMLInputElement>;
  handleFileSelect: (e: ChangeEvent<HTMLInputElement>) => void;
}

export function CommandInputActions({
  effectiveIndexEnabled,
  isIndexBuilding = false,
  disabled,
  isConnected,
  isProcessing,
  canSend,
  onToggleIndex,
  onToggleIndexClick,
  handleUploadClick,
  handleNewSession,
  handleSubmit,
  handleQueue,
  onStop,
  onQueue,
  showQueuePanel,
  setShowQueuePanel,
  queuePanelRef,
  queuedCount,
  queuedMessages,
  onQueueMessageRemove,
  onQueueMessageEdit,
  onQueueReorder,
  onClearQueuedMessages,
  fileInputRef,
  handleFileSelect,
}: CommandInputActionsProps): JSX.Element {
  return (
    <div className="input-actions">
      {onToggleIndex !== undefined && (
        <button
          type="button"
          className={`index-badge ${effectiveIndexEnabled ? 'enabled' : 'disabled'}`}
          onClick={onToggleIndexClick}
          data-tooltip={
            effectiveIndexEnabled
              ? isIndexBuilding
                ? 'Building index...'
                : 'Indexing enabled — click to disable'
              : 'Enable workspace indexing for semantic search'
          }
          aria-label={effectiveIndexEnabled ? 'Disable workspace indexing' : 'Enable workspace indexing'}
          aria-pressed={effectiveIndexEnabled}
        >
          <Database size={14} />
          {!effectiveIndexEnabled && <span className="index-badge-slash" />}
        </button>
      )}
      <button
        type="button"
        className="upload-button"
        onClick={handleUploadClick}
        disabled={disabled}
        data-tooltip="Attach image"
        aria-label="Attach image"
      >
        <Plus size={16} />
      </button>
      <input ref={fileInputRef} type="file" accept="image/*" style={{ display: 'none' }} onChange={handleFileSelect} />
      <button
        type="button"
        className="new-session-button"
        onClick={handleNewSession}
        disabled={disabled}
        data-tooltip="New Session (/clear)"
        aria-label="New Session"
      >
        <SquarePen size={16} />
      </button>
      <button
        type="submit"
        disabled={disabled || !canSend || !isConnected}
        className="send-button"
        data-tooltip={!isConnected ? 'Reconnecting...' : isProcessing ? 'Steer running request' : 'Send message'}
        aria-label="Send message"
      >
        <Send size={16} />
      </button>
      {isProcessing && (
        <button
          type="button"
          onClick={onStop}
          disabled={disabled}
          className="stop-button"
          data-tooltip="Stop processing"
          aria-label="Stop processing"
        >
          <Square size={15} />
        </button>
      )}
      {isProcessing && onQueue && (
        <button
          type="button"
          onClick={handleQueue}
          disabled={disabled || !canSend}
          className="queue-add-button"
          data-tooltip="Queue for after current run"
          aria-label="Queue message"
        >
          <ListPlus size={16} />
        </button>
      )}
      {(queuedCount > 0 || showQueuePanel) && (
        <div className="queue-button-wrapper" ref={queuePanelRef}>
          <button
            type="button"
            onClick={() => {
              setShowQueuePanel((prev) => !prev);
            }}
            disabled={queuedCount === 0}
            className="queue-button"
            data-tooltip={`${queuedCount} queued message${queuedCount !== 1 ? 's' : ''} — click to manage`}
            aria-label={`View ${queuedCount} queued message${queuedCount !== 1 ? 's' : ''}`}
          >
            <ListPlus size={16} />
            {queuedCount > 0 && <span className="queue-count">{queuedCount}</span>}
          </button>
          {showQueuePanel && (
            <div className="queue-popover-overlay">
              <QueuedMessagesPanel
                messages={queuedMessages}
                onRemove={
                  onQueueMessageRemove ||
                  (() => {
                    /* noop */
                  })
                }
                onEdit={
                  onQueueMessageEdit ||
                  (() => {
                    /* noop */
                  })
                }
                onReorder={
                  onQueueReorder ||
                  (() => {
                    /* noop */
                  })
                }
                onClear={
                  onClearQueuedMessages ||
                  (() => {
                    /* noop */
                  })
                }
                onClose={() => setShowQueuePanel(false)}
              />
            </div>
          )}
        </div>
      )}
    </div>
  );
}
