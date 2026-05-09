import { useRef, useCallback, useState, useMemo, useLayoutEffect, CSSProperties } from 'react';
import { Virtuoso, type VirtuosoHandle } from 'react-virtuoso';
import { ChevronDown } from 'lucide-react';
import CommandInput from './CommandInput';
import { ChatMessageContextMenu } from '@sprout/ui';
import { supportsSSH } from '../config/mode';
import { requiresBackendHealthCheck } from '../services/apiAdapter';
import { clientFetch } from '../services/clientSession';
import {
  ChatFooter,
  ChatHeader,
  EmptyChatPanel,
  MessageItem,
} from './chat';
import type { ChatProps, ToolExecution } from './chat/types';
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
    currentTodos: _currentTodos = [],
    subagentActivities = [],
    onToolPillClick,
    onStopProcessing,
    chatId: _chatId,
    worktreePath,
    workspaceRoot: _workspaceRoot,
    onWorktreeChange: _onWorktreeChange,
    providerAvailable,
    onRequestProviderSetup,
    stats,
    isConnected,
    backendReachable,
    onRetryConnection,
  } = props;

  const chatShellRef = useRef<HTMLDivElement>(null);
  const chatContainerRef = useRef<HTMLDivElement>(null);
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const inputContainerRef = useRef<HTMLDivElement>(null);
  const [isAtBottom, setIsAtBottom] = useState(true);
  const [inputContainerHeight, setInputContainerHeight] = useState(0);

  const inputValueRef = useRef(inputValue);
  inputValueRef.current = inputValue;

  const hasSubagentActivity = subagentActivities.length > 0;
  const needsHealthCheck = requiresBackendHealthCheck();

  const currentQueryCount = stats?.queryCount as number | undefined;
  const filteredToolExecutions = useMemo(() => {
    if (!currentQueryCount) {
      return toolExecutions;
    }
    return toolExecutions.filter(
      (tool: ToolExecution) => tool.queryId === undefined || tool.queryId === currentQueryCount,
    );
  }, [toolExecutions, currentQueryCount]);

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

  const formatTime = (date: Date) => {
    return new Date(date).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  };

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

  const handleInsertAtCursor = useCallback(
    (text: string) => {
      const separator = inputValueRef.current ? '\n' : '';
      onInputChange(inputValueRef.current + separator + text);
    },
    [onInputChange],
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
    >
      <div className="chat-main">
        {showOffline
          ? (
            <EmptyChatPanel
              ref={chatContainerRef}
              showOffline
              onRetryConnection={onRetryConnection}
            />
          ) : messages.length === 0
            ? (
              <EmptyChatPanel
                ref={chatContainerRef}
                providerAvailable={providerAvailable}
                onRequestProviderSetup={onRequestProviderSetup}
              />
            ) : (
              <div ref={chatContainerRef} style={{ flex: 1, minHeight: 0, position: 'relative' }}>
                <Virtuoso
                  ref={virtuosoRef}
                  data={messages}
                  followOutput={(isAtBottom) => (isAtBottom ? 'smooth' : false)}
                  initialTopMostItemIndex={messages.length - 1}
                  increaseViewportBy={{ top: 400, bottom: 400 }}
                  atBottomStateChange={(atBottom) => setIsAtBottom(atBottom)}
                  itemContent={(_index, message) => (
                    <MessageItem
                      message={message}
                      onToolPillClick={onToolPillClick}
                      findMatchingToolExecution={findMatchingToolExecution}
                      filteredToolExecutions={filteredToolExecutions}
                      formatTime={formatTime}
                    />
                  )}
                  components={{
                    Header: () => <ChatHeader worktreePath={worktreePath} />,
                    Footer: () => (
                      <ChatFooter
                        hasSubagentActivity={hasSubagentActivity}
                        subagentActivities={subagentActivities}
                        queryProgress={queryProgress}
                        isProcessing={isProcessing}
                        filteredToolExecutions={filteredToolExecutions}
                        lastError={lastError}
                        showExpiredSessionRecovery={showExpiredSessionRecovery}
                        handleReloadWithoutSSHPath={handleReloadWithoutSSHPath}
                      />
                    ),
                  }}
                  className="chat-virtuoso"
                  style={{ height: '100%' }}
                />
                {!isAtBottom && (
                  <button
                    className="scroll-to-bottom-btn"
                    onClick={() => virtuosoRef.current?.scrollToIndex({ index: 'LAST', behavior: 'smooth', align: 'end' })}
                    type="button"
                    aria-label="Scroll to bottom"
                  >
                    <ChevronDown size={18} />
                  </button>
                )}
              </div>
            )}
      </div>

      <div className="input-container" ref={inputContainerRef}>
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
          isIndexEnabled={!!(stats as Record<string, unknown>)?.embedding_index_enabled}
          isIndexBuilding={!!(stats as Record<string, unknown>)?.embedding_index_building}
          onToggleIndex={handleToggleIndex}
        />
      </div>

      <ChatMessageContextMenu containerRef={chatContainerRef} onInsertAtCursor={handleInsertAtCursor} />
    </div>
  );
}

export default Chat;
