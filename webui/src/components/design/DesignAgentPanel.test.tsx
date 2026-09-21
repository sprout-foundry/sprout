/**
 * DesignAgentPanel tests (SP-140-6 §6f, reworked with the side column).
 *
 * Pins the panel's contract: it is just the chat (visibility belongs to the
 * side column's tabs); it mounts the real Chat with the shell's own chatProps
 * (no second chat implementation); the design-mode placeholder is applied;
 * prefill fills the input through the controlled onInputChange and is
 * consumed exactly once; prefill never sends.
 */

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { ChatProps } from '../chat/types';
import DesignAgentPanel from './DesignAgentPanel';

vi.mock('../ChatView', () => ({
  default: function MockChat(props: ChatProps) {
    return (
      <div data-testid="mock-chat">
        <input
          data-testid="mock-chat-input"
          value={props.inputValue ?? ''}
          onChange={(event) => props.onInputChange?.(event.target.value)}
        />
        <button type="button" data-testid="mock-chat-send" onClick={() => props.onSendMessage?.('')}>
          send
        </button>
      </div>
    );
  },
}));

function makeChatProps(overrides: Partial<ChatProps> = {}): ChatProps {
  return {
    messages: [],
    onSendMessage: vi.fn(),
    onInputChange: vi.fn(),
    inputValue: '',
    ...overrides,
  } as unknown as ChatProps;
}

describe('DesignAgentPanel', () => {
  it('renders the chat with the shell chatProps', () => {
    render(<DesignAgentPanel chatProps={makeChatProps()} />);
    expect(screen.getByTestId('design-agent-panel')).toBeInTheDocument();
    expect(screen.getByTestId('design-agent-chat')).toBeInTheDocument();
    expect(screen.getByTestId('mock-chat')).toBeInTheDocument();
  });

  it('prefill lands in the input via the controlled callback and is consumed once', async () => {
    const onInputChange = vi.fn();
    const onPrefillConsumed = vi.fn();
    const chatProps = makeChatProps({ onInputChange });
    const view = render(
      <DesignAgentPanel
        chatProps={chatProps}
        prefill="Run design_sync to import the implementation's semantic deltas into design/."
        onPrefillConsumed={onPrefillConsumed}
      />,
    );
    await waitFor(() => {
      expect(onInputChange).toHaveBeenCalledWith(
        "Run design_sync to import the implementation's semantic deltas into design/.",
      );
    });
    expect(onPrefillConsumed).toHaveBeenCalledTimes(1);

    // Re-rendering with the SAME prefill (parent not yet nulled it) must not
    // re-stamp the input: the effect keys on the prefill value.
    view.rerender(
      <DesignAgentPanel
        chatProps={chatProps}
        prefill="Run design_sync to import the implementation's semantic deltas into design/."
        onPrefillConsumed={onPrefillConsumed}
      />,
    );
    expect(onPrefillConsumed).toHaveBeenCalledTimes(1);
    const calls = onInputChange.mock.calls.map((c) => c[0]);
    expect(
      calls.filter((c) => c === "Run design_sync to import the implementation's semantic deltas into design/."),
    ).toHaveLength(1);
  });

  it('prefill never auto-sends', async () => {
    const onSendMessage = vi.fn();
    render(<DesignAgentPanel chatProps={makeChatProps({ onSendMessage })} prefill="some prompt" />);
    await screen.findByTestId('mock-chat');
    expect(onSendMessage).not.toHaveBeenCalled();
  });
});
