/**
 * ExampleEmbedding.
 *
 * Pins the acceptance criterion — "An example embedding composes chat +
 * preview from exported views" — and the embedding-supplied arrangement:
 * the example's own composition, props passthrough, and a caller-supplied
 * arrangement moving the views. The chat and preview panel are mocked to
 * stubs (they need the webui context stack); the stubs echo the props the
 * example is responsible for delivering.
 */

import { render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
vi.mock('../components/ChatView', () => ({
  default: (props: { inputValue?: string }) => <div data-testid="views-test-chat">input={props.inputValue ?? ''}</div>,
}));
vi.mock('../components/PreviewPanel', () => ({
  PreviewPanel: (props: { open?: boolean }) => (
    <div data-testid="views-test-preview-panel" data-open={String(!!props.open)} />
  ),
}));

import ExampleEmbedding from './ExampleEmbedding';
import type { ChatProps } from './index';

/** Minimal ChatProps, the same shape views.test.ts accepts at type level. */
function makeChatProps(overrides: Partial<ChatProps> = {}): ChatProps {
  return {
    messages: [{ id: 'm1', type: 'user', content: 'hello', timestamp: new Date(0) }],
    onSendMessage: () => undefined,
    onQueueMessage: () => undefined,
    queuedMessagesCount: 0,
    inputValue: '',
    onInputChange: () => undefined,
    ...overrides,
  };
}

describe('ExampleEmbedding', () => {
  it('composes chat (center) + preview (overlay) from the exported views', () => {
    render(<ExampleEmbedding chat={makeChatProps()} />);
    expect(screen.getByTestId('views-example-embedding')).toBeInTheDocument();
    expect(within(screen.getByTestId('views-slot-center')).getByTestId('views-test-chat')).toBeInTheDocument();
    expect(
      within(screen.getByTestId('views-slot-overlay')).getByTestId('views-test-preview-panel'),
    ).toBeInTheDocument();
  });

  it('delivers the host state: chat input through, preview open', () => {
    render(<ExampleEmbedding chat={makeChatProps({ inputValue: 'host-owned input' })} />);
    expect(screen.getByTestId('views-test-chat')).toHaveTextContent('input=host-owned input');
    expect(screen.getByTestId('views-test-preview-panel')).toHaveAttribute('data-open', 'true');
  });

  it('passes the supplied preview props to the panel', () => {
    render(<ExampleEmbedding chat={makeChatProps()} preview={{ open: false, onClose: () => undefined }} />);
    expect(screen.getByTestId('views-test-preview-panel')).toHaveAttribute('data-open', 'false');
  });

  it('honors an embedding-supplied arrangement', () => {
    render(<ExampleEmbedding chat={makeChatProps()} arrangement={{ left: ['chat'], center: ['previewPanel'] }} />);
    expect(within(screen.getByTestId('views-slot-left')).getByTestId('views-test-chat')).toBeInTheDocument();
    expect(within(screen.getByTestId('views-slot-center')).getByTestId('views-test-preview-panel')).toBeInTheDocument();
  });
});
