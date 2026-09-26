import { render, screen, fireEvent } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { ChatProps } from '../chat/types';
import DesignAgentPanel from './DesignAgentPanel';
import type { DesignAgentPanelProps } from './DesignAgentPanel';

vi.mock('../ChatView', () => ({
  default: function MockChat() {
    return <div data-testid="mock-chat" />;
  },
}));

/**
 * SP-142 item 142.5 — the Design agent panel's header: the design chat's
 * name and a New Chat affordance scoped to the design lane.
 *
 * The header renders only when the host supplies the lane identity
 * (chatName/onCreateDesignChat) — the bare mounts (tests, hosts without
 * chat sessions) keep the headerless chat.
 */
const chatProps = {
  messages: [],
  onSendMessage: () => {},
  onQueueMessage: () => {},
  queuedMessagesCount: 0,
  inputValue: '',
  onInputChange: () => {},
} as unknown as DesignAgentPanelProps['chatProps'];

describe('DesignAgentPanel header (SP-142 142.5)', () => {
  it('renders the header naming the active design chat', () => {
    render(<DesignAgentPanel chatProps={chatProps} chatName="Critique round 2" onCreateDesignChat={() => {}} />);
    expect(screen.getByTestId('design-agent-head')).toBeTruthy();
    expect(screen.getByTestId('design-agent-chat-name').textContent).toBe('Critique round 2');
    expect(screen.getByTestId('design-agent-new-chat')).toBeTruthy();
  });

  it('New Chat fires the design-lane create action', () => {
    const onCreateDesignChat = vi.fn();
    render(<DesignAgentPanel chatProps={chatProps} chatName="A" onCreateDesignChat={onCreateDesignChat} />);
    fireEvent.click(screen.getByTestId('design-agent-new-chat'));
    expect(onCreateDesignChat).toHaveBeenCalledTimes(1);
  });

  it('omits the header when the host supplies no lane identity', () => {
    render(<DesignAgentPanel chatProps={chatProps} />);
    expect(screen.queryByTestId('design-agent-head')).toBeNull();
    expect(screen.queryByTestId('design-agent-new-chat')).toBeNull();
    // The chat itself still mounts.
    expect(screen.getByTestId('design-agent-panel')).toBeTruthy();
  });

  it('falls back to a generic label when only the create action is present', () => {
    render(<DesignAgentPanel chatProps={chatProps} onCreateDesignChat={() => {}} />);
    expect(screen.getByTestId('design-agent-chat-name').textContent).toBe('Design chat');
  });
});
