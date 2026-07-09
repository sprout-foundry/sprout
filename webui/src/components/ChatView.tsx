import { ChatMessageContextMenu } from '@sprout/ui';
import { ChevronDown, Download } from 'lucide-react';
import { useRef, useCallback, useState, useMemo, useLayoutEffect } from 'react';
import type { CSSProperties } from 'react';
import { Virtuoso, type VirtuosoHandle } from 'react-virtuoso';
import { supportsSSH } from '../config/mode';
import { rewindQuery } from '../services/api/chatApi';
import { requiresBackendHealthCheck } from '../services/apiAdapter';
import { clientFetch } from '../services/clientSession';
import type { QueryProgress } from '../types/app';
import { ChatFooter, ChatHeader, EmptyChatPanel, MessageItem } from './chat';
import type { ChatProps, Message, ToolExecution } from './chat/types';
import CommandInput from './CommandInput';
import ExportDialog from './ExportDialog';
import InlineTodoSummary from './InlineTodoSummary';
import { ToolTimelineBar } from './chat/ToolTimelineBar';
import { showThemedAlert, showThemedConfirm } from './ThemedDialog';
import './Chat.css';

function Chat(props: ChatProps): JSX.Element {
  const {
    messages,
    onSendMessage,
    onQueueMessage,
    queuedMessagesCount,
    queuedMessages = [],
    onQueueMessageRemove,
    onQueueMessageEdit,
    onQueueReorder,
    onClearQueuedMessages,
    inputValue,
    onInputChange,
    isProcessing = false,
    lastError = null,
    toolExecutions = [],
    queryProgress = null,
    currentTodos = [],
    subagentActivities: _subagentActivities = [],
    onToolPillClick,
    onStopProcessing,
    chatId,
    worktreePath,
    workspaceRoot: _workspaceRoot,
    onWorktreeChange: _onWorktreeChange,
    providerAvailable,
    onRequestProviderSetup,
    stats,
    isConnected,
    backendReachable,
    onRetryConnection,
    outputVerbosity = 'default',
  } = props;

  const chatShellRef = useRef<HTMLDivElement>(null);
  const chatContainerRef = useRef<HTMLDivElement>(null);
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const inputContainerRef = useRef<HTMLDivElement>(null);
  const [isAtBottom, setIsAtBottom] = useState(true);
  const [inputContainerHeight, setInputContainerHeight] = useState(0);
  const [isRewinding, setIsRewinding] = useState(false);
  const [isExportDialogOpen, setIsExportDialogOpen] = useState(false);

  const sessionId = chatId ?? '';

  const inputValueRef = useRef(inputValue);
  inputValueRef.current = inputValue;

  const needsHealthCheck = requiresBackendHealthCheck();

  const currentQueryCount = typeof stats?.queryCount === 'number' ? stats.queryCount : undefined;
  // Show all tool executions — don't filter by queryId. The queryId filter
  // caused tools from the previous query to vanish when a new query started,
  // making the badges show and hide at random intervals.
  const filteredToolExecutions = toolExecutions;

  // Map of toolId → status across ALL queries (not filtered).
  // MessageSegments uses this to decide pill-vs-footnote rendering. Filtering
  // here would make a completed tool from a previous query register as
  // `undefined`, which falls back to the running-pill render — that's the
  // root of the visible flicker where a tool badge appears, switches to its
  // compact footnote, and then snaps back to the larger pill once the next
  // query starts.
  const toolStatusById = useMemo(() => {
    const map = new Map<string, ToolExecution['status']>();
    for (const t of toolExecutions) {
      map.set(t.id, t.status);
    }
    return map;
  }, [toolExecutions]);
  const getToolStatusForMessage = useCallback((toolId: string) => toolStatusById.get(toolId), [toolStatusById]);

  useLayoutEffect(() => {
    const node = inputContainerRef.current;
    if (!node) return;
    const updateHeight = () => setInputContainerHeight(node.getBoundingClientRect().height);
    updateHeight();
    if (typeof ResizeObserver === 'undefined') {
      window.addEventListener('resize', updateHeight);
      return () => window.removeEventListener('resize', updateHeight);
    }
    const observer = new ResizeObserver(updateHeight);
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  const findMatchingToolExecution = useCallback(
    (toolName: string) => {
      const normalized = toolName.split('(')[0];
      for (let i = filteredToolExecutions.length - 1; i >= 0; i -= 1) {
        if (filteredToolExecutions[i].tool === normalized) {
          return filteredToolExecutions[i];
        }
      }
      return undefined;
    },
    [filteredToolExecutions],
  );

  // Stable reference — passed to memoized MessageItem. An inline arrow
  // here would create a new function every render (e.g. on every chat
  // input keystroke), breaking MessageItem's memo so every message
  // would re-execute its markdown + MessageSegments pipeline. That was
  // the visible "footnote flicker on every keypress" bug.
  const formatTime = useCallback((date: Date) => {
    return new Date(date).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }, []);

  // SP-076: messages ref so renderMessageItem can read the next message
  // without putting `messages` in its useCallback deps (which would
  // recreate the callback on every streaming chunk and defeat
  // MessageItem's memo, re-running markdown + MessageSegments for
  // every visible row).
  const messagesRef = useRef<Message[]>(messages);
  messagesRef.current = messages;

  // Stable Virtuoso itemContent — see the `formatTime` comment above.
  // An inline arrow recreates the function on every parent render,
  // and Virtuoso then treats every row as new and re-runs all the
  // MessageItem renders even though props are identical.
  const renderMessageItem = useCallback(
    (index: number, message: Message) => {
      // SP-076: the next message in the visible list. If it's another
      // assistant message, this one is inter-tool narration (not the
      // terminal answer) — compact mode hides it. Computed here so
      // MessageItem stays a pure presentational component.
      const nextMessage = index + 1 < messagesRef.current.length ? messagesRef.current[index + 1] : null;
      const hasNextAssistantMessage = nextMessage?.type === 'assistant';
      return (
        <MessageItem
          message={message}
          onToolPillClick={onToolPillClick}
          findMatchingToolExecution={findMatchingToolExecution}
          getToolStatus={getToolStatusForMessage}
          formatTime={formatTime}
          messageIndex={index}
          outputVerbosity={outputVerbosity}
          hasNextAssistantMessage={hasNextAssistantMessage}
        />
      );
    },
    [onToolPillClick, findMatchingToolExecution, getToolStatusForMessage, formatTime, outputVerbosity],
  );

  const handleReloadWithoutSSHPath = useCallback(() => {
    const { origin, pathname } = window.location;
    if (pathname.startsWith('/ssh/')) {
      window.location.assign(`${origin}/`);
      return;
    }
    window.location.reload();
  }, []);

  const showExpiredSessionRecovery =
    supportsSSH && !!lastError && lastError.toLowerCase().includes('ssh session not found or expired');

  // Stable Footer/Header component references — prevents Virtuoso from
  // unmounting/remounting Footer (ChatFooter → ToolTimelineBar) on every
  // keystroke. Without useCallback, the inline arrow functions change
  // reference on every render (e.g. during typing), causing ToolTimelineBar
  // to lose internal state (shouldRender, completedAtRef) and its badges
  // to flash in/out.
  const VirtuosoHeader = useCallback(() => <ChatHeader worktreePath={worktreePath} />, [worktreePath]);
  const VirtuosoFooter = useCallback(
    () => (
      <ChatFooter
        queryProgress={queryProgress as QueryProgress | null}
        isProcessing={isProcessing}
        filteredToolExecutions={filteredToolExecutions}
        lastError={lastError}
        showExpiredSessionRecovery={showExpiredSessionRecovery}
        handleReloadWithoutSSHPath={handleReloadWithoutSSHPath}
        currentTodos={currentTodos}
      />
    ),
    [
      queryProgress,
      isProcessing,
      filteredToolExecutions,
      lastError,
      showExpiredSessionRecovery,
      handleReloadWithoutSSHPath,
      currentTodos,
    ],
  );

  const handleInsertAtCursor = useCallback(
    (text: string) => {
      const separator = inputValueRef.current ? '\n' : '';
      onInputChange(inputValueRef.current + separator + text);
    },
    [onInputChange],
  );

  const handleRewindAndResend = useCallback(
    async (messageContent: string, messageIndex: number) => {
      if (isRewinding) return;
      // The toTurn is a checkpoint index: user messages are at even indices (0, 2, 4, ...)
      // and correspond to checkpoints 0, 1, 2, ....  To rewind BEFORE this checkpoint
      // pass k - 1 (where k = Math.floor(messageIndex / 2)).  For the very first message
      // the result is 0 which is a harmless no-op.
      const toTurn = Math.max(0, Math.floor(messageIndex / 2) - 1);

      const confirmed = await showThemedConfirm(`Discard all turns after this message and revert file changes?`, {
        title: 'Edit & Resend',
        confirmLabel: 'Resend',
        cancelLabel: 'Cancel',
        type: 'danger',
      });
      if (!confirmed) return;

      setIsRewinding(true);
      try {
        await rewindQuery(clientFetch, toTurn, true, chatId);
        onInputChange(messageContent);
      } catch (e) {
        console.error('Rewind failed:', e);
        await showThemedAlert(`Rewind failed: ${e instanceof Error ? e.message : 'Unknown error'}`, {
          title: 'Rewind Failed',
          type: 'error',
        });
      } finally {
        setIsRewinding(false);
      }
    },
    [isRewinding, onInputChange, chatId],
  );

  const handleToggleIndex = useCallback(async (enabled: boolean) => {
    try {
      const response = await clientFetch('/api/embedding-index', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled }),
      });
      if (!response.ok) {
        const text = await response.text();
        console.error('Failed to toggle indexing:', response.status, text);
      }
    } catch (e) {
      console.error('Failed to toggle indexing:', e);
    }
  }, []);

  const showOffline = needsHealthCheck && backendReachable === false && !isProcessing && messages.length === 0;

  return (
    <div
      className="chat-shell"
      ref={chatShellRef}
      style={{ '--chat-input-height': `${inputContainerHeight}px` } as CSSProperties}
      data-testid="chat-shell"
    >
      <div className="chat-main" data-testid="chat-main">
        {/* Export button — shown when a session is active */}
        {sessionId && (
          <div className="chat-toolbar">
            <button
              type="button"
              className="chat-export-btn"
              onClick={() => setIsExportDialogOpen(true)}
              data-testid="chat-export-button"
            >
              <Download size={14} />
              Export
            </button>
          </div>
        )}

        <InlineTodoSummary todos={currentTodos} isLoading={isProcessing && currentTodos.length === 0} />
        {showOffline ? (
          <EmptyChatPanel ref={chatContainerRef} showOffline onRetryConnection={onRetryConnection} />
        ) : messages.length === 0 ? (
          <EmptyChatPanel
            ref={chatContainerRef}
            providerAvailable={providerAvailable}
            onRequestProviderSetup={onRequestProviderSetup}
          />
        ) : (
          <div
            ref={chatContainerRef}
            role="log"
            aria-label="Chat messages"
            data-testid="chat-message-list"
            style={{ flex: 1, minHeight: 0, position: 'relative' }}
          >
            <Virtuoso
              ref={virtuosoRef}
              data={messages}
              followOutput={(isAtBottom) => (isAtBottom ? 'smooth' : false)}
              initialTopMostItemIndex={messages.length - 1}
              increaseViewportBy={{ top: 400, bottom: 400 }}
              atBottomStateChange={setIsAtBottom}
              itemContent={renderMessageItem}
              components={{ Header: VirtuosoHeader, Footer: VirtuosoFooter }}
              className="chat-virtuoso"
              style={{ height: '100%' }}
            />
            {!isAtBottom && (
              <button
                className="scroll-to-bottom-btn"
                onClick={() => virtuosoRef.current?.scrollToIndex({ index: 'LAST', behavior: 'smooth', align: 'end' })}
                type="button"
                aria-label="Scroll to bottom"
                data-testid="chat-scroll-bottom"
              >
                <ChevronDown size={18} />
              </button>
            )}
          </div>
        )}
      </div>

      <div className="input-container" ref={inputContainerRef}>
        <ToolTimelineBar toolExecutions={filteredToolExecutions} />
        {isProcessing && filteredToolExecutions.length === 0 && (
          <div className="thinking-indicator" role="status" aria-live="polite">
            <span className="thinking-indicator-dots">
              <span className="thinking-dot" />
              <span className="thinking-dot" />
              <span className="thinking-dot" />
            </span>
            <span className="thinking-indicator-text">
              {isProcessing ? 'Thinking' : 'Sending…'}
            </span>
          </div>
        )}
        <CommandInput
          value={inputValue}
          onChange={onInputChange}
          onSend={onSendMessage}
          onQueue={onQueueMessage}
          onStop={onStopProcessing}
          placeholder={
            providerAvailable === false
              ? 'Configure a provider to start chatting...'
              : needsHealthCheck && backendReachable === false
                ? 'Waiting for server connection...'
                : 'Ask me anything about your code...'
          }
          multiline={true}
          autoFocus={providerAvailable !== false && !(needsHealthCheck && backendReachable === false)}
          isProcessing={isProcessing}
          isConnected={isConnected}
          disabled={providerAvailable === false || (needsHealthCheck && backendReachable === false)}
          queuedCount={queuedMessagesCount}
          queuedMessages={queuedMessages}
          onQueueMessageRemove={onQueueMessageRemove}
          onQueueMessageEdit={onQueueMessageEdit}
          onQueueReorder={onQueueReorder}
          onClearQueuedMessages={onClearQueuedMessages}
          isIndexEnabled={!!stats?.embedding_index_enabled}
          isIndexBuilding={!!stats?.embedding_index_building}
          onToggleIndex={handleToggleIndex}
        />
      </div>

      <ChatMessageContextMenu
        containerRef={chatContainerRef}
        onInsertAtCursor={handleInsertAtCursor}
        onRewindAndResend={isRewinding ? undefined : handleRewindAndResend}
      />

      <ExportDialog isOpen={isExportDialogOpen} onClose={() => setIsExportDialogOpen(false)} sessionId={sessionId} />
    </div>
  );
}

export default Chat;
