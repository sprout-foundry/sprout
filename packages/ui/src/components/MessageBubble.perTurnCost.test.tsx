import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import MessageBubble from './MessageBubble';

// Mock lucide-react
vi.mock('lucide-react', () => ({
  Copy: () => <span data-testid="copy-icon" />,
}));

describe('MessageBubble per-turn cost display (SP-053-perTurnCost)', () => {
  it('renders cost line when cost prop is provided', () => {
    render(
      <MessageBubble type="assistant" ariaLabel="test" cost={0.0034}>
        Hello
      </MessageBubble>
    );
    expect(screen.getByText('$0.0034')).toBeInTheDocument();
  });

  it('renders tokens line when tokensUsed prop is provided', () => {
    render(
      <MessageBubble type="assistant" ariaLabel="test" tokensUsed={1200}>
        Hello
      </MessageBubble>
    );
    expect(screen.getByText('1.2k tokens')).toBeInTheDocument();
  });

  it('renders both tokens and cost with separator when both provided', () => {
    const { container } = render(
      <MessageBubble type="assistant" ariaLabel="test" tokensUsed={5000} cost={0.12}>
        Hello
      </MessageBubble>
    );
    expect(screen.getByText('5.0k tokens')).toBeInTheDocument();
    expect(screen.getByText('$0.1200')).toBeInTheDocument();
    expect(container.querySelector('.message-turn-cost-sep')).toBeInTheDocument();
  });

  it('renders model name when provided with cost', () => {
    render(
      <MessageBubble type="assistant" ariaLabel="test" tokensUsed={1000} cost={0.01} model="gpt-4o">
        Hello
      </MessageBubble>
    );
    expect(screen.getByText('gpt-4o')).toBeInTheDocument();
  });

  it('does not render cost line when neither cost nor tokensUsed provided', () => {
    const { container } = render(
      <MessageBubble type="assistant" ariaLabel="test">
        Hello
      </MessageBubble>
    );
    expect(container.querySelector('.message-turn-cost')).toBeNull();
  });

  it('does not render cost line when only model is provided without tokens or cost', () => {
    const { container } = render(
      <MessageBubble type="assistant" ariaLabel="test" model="gpt-4o">
        Hello
      </MessageBubble>
    );
    expect(container.querySelector('.message-turn-cost')).toBeNull();
  });

  it('shows small token counts without k suffix', () => {
    render(
      <MessageBubble type="assistant" ariaLabel="test" tokensUsed={500}>
        Hello
      </MessageBubble>
    );
    expect(screen.getByText('500 tokens')).toBeInTheDocument();
  });

  it('renders cost line with zero cost and tokens', () => {
    const { container } = render(
      <MessageBubble type="assistant" ariaLabel="test" tokensUsed={0} cost={0}>
        Hello
      </MessageBubble>
    );
    // 0 != null is true, so the cost line should render
    expect(container.querySelector('.message-turn-cost')).not.toBeNull();
    expect(screen.getByText('$0.0000')).toBeInTheDocument();
    expect(screen.getByText('0 tokens')).toBeInTheDocument();
  });

  it('renders cost-only without leading separator when no tokens', () => {
    const { container } = render(
      <MessageBubble type="assistant" ariaLabel="test" cost={0.05}>
        Hello
      </MessageBubble>
    );
    expect(screen.getByText('$0.0500')).toBeInTheDocument();
    // When only cost is present, there should be no separator
    expect(container.querySelectorAll('.message-turn-cost-sep').length).toBe(0);
  });

  it('renders model with cost-only (no tokens) with separator', () => {
    render(
      <MessageBubble type="assistant" ariaLabel="test" cost={0.05} model="claude-3.5">
        Hello
      </MessageBubble>
    );
    expect(screen.getByText('$0.0500')).toBeInTheDocument();
    expect(screen.getByText('claude-3.5')).toBeInTheDocument();
  });
});
