import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { ChatFooter } from './ChatFooter';
import type { ToolExecution } from './types';

/**
 * SP-142 item 142.4 — the workspace-busy notice and send-anyway queueing.
 *
 * The composer footer renders the inline notice when the server rejects a
 * send with 409 workspace_busy (another chat in this client context holds
 * the workspace gate): it names the running chat, offers Send-anyway
 * (queues locally behind the runner; the queue drains on completion), and
 * dismisses. The notice retires when the running chat's query_completed
 * arrives (the handler clears state.workspaceBusy — pinned in
 * useWebSocketEventHandler.test.ts's completed-path coverage) and the
 * queued entry's cancel path is the existing queue-remove action.
 */
describe('ChatFooter workspace-busy notice (SP-142 §3)', () => {
  const baseProps = {
    queryProgress: null,
    isProcessing: false,
    filteredToolExecutions: [] as ToolExecution[],
    lastError: null,
    showExpiredSessionRecovery: false,
    handleReloadWithoutSSHPath: () => {},
  };

  it('renders the busy notice naming the running chat', () => {
    render(
      <ChatFooter {...baseProps} workspaceBusy={{ runningChatId: 'chat-2', runningChatName: 'Refactor sweep' }} />,
    );
    expect(screen.getByTestId('workspace-busy-notice')).toBeTruthy();
    expect(screen.getByText(/Refactor sweep/)).toBeTruthy();
    expect(screen.getByText(/send anyway queues after it/)).toBeTruthy();
  });

  it('Send-anyway queues the draft and clears the notice', () => {
    const onSendAnyway = vi.fn();
    const onDismissBusy = vi.fn();
    const { rerender } = render(
      <ChatFooter
        {...baseProps}
        workspaceBusy={{ runningChatId: 'chat-2', runningChatName: 'Sweep' }}
        onSendAnyway={onSendAnyway}
        onDismissBusy={onDismissBusy}
        pendingDraft="  fix the flaky test  "
      />,
    );
    fireEvent.click(screen.getByTestId('workspace-busy-send-anyway'));
    // The handler receives the raw draft; trimming is the handler's job
    // (AppContent's handleSendAnyway trims before queueing).
    expect(onSendAnyway).toHaveBeenCalledWith('  fix the flaky test  ');

    // After the state clears (completion or the send itself), the notice
    // is gone.
    rerender(
      <ChatFooter
        {...baseProps}
        workspaceBusy={null}
        onSendAnyway={onSendAnyway}
        onDismissBusy={onDismissBusy}
        pendingDraft=""
      />,
    );
    expect(screen.queryByTestId('workspace-busy-notice')).toBeNull();
  });

  it('dismiss clears the notice without queueing', () => {
    const onSendAnyway = vi.fn();
    const onDismissBusy = vi.fn();
    render(
      <ChatFooter
        {...baseProps}
        workspaceBusy={{ runningChatId: 'chat-2', runningChatName: 'Sweep' }}
        onSendAnyway={onSendAnyway}
        onDismissBusy={onDismissBusy}
        pendingDraft="draft"
      />,
    );
    fireEvent.click(screen.getByTestId('workspace-busy-dismiss'));
    expect(onDismissBusy).toHaveBeenCalledTimes(1);
    expect(onSendAnyway).not.toHaveBeenCalled();
  });

  it('omits Send-anyway when there is no draft to queue', () => {
    render(
      <ChatFooter
        {...baseProps}
        workspaceBusy={{ runningChatId: 'chat-2', runningChatName: 'Sweep' }}
        onSendAnyway={() => {}}
        onDismissBusy={() => {}}
        pendingDraft="   "
      />,
    );
    expect(screen.queryByTestId('workspace-busy-send-anyway')).toBeNull();
    // The notice itself stays — the user may type a draft next.
    expect(screen.getByTestId('workspace-busy-notice')).toBeTruthy();
  });

  it('clearing on completion: the notice disappears when workspaceBusy clears', async () => {
    const { rerender } = render(
      <ChatFooter {...baseProps} workspaceBusy={{ runningChatId: 'c', runningChatName: 'Runner' }} />,
    );
    expect(screen.getByTestId('workspace-busy-notice')).toBeTruthy();
    rerender(<ChatFooter {...baseProps} workspaceBusy={null} />);
    await waitFor(() => {
      expect(screen.queryByTestId('workspace-busy-notice')).toBeNull();
    });
  });
});
