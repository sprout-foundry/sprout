/**
 * CommandOutputPanel tests — SP-114 Phase 2d.
 *
 * Validates the component renders the right DOM for each state slice:
 *   - empty state hidden
 *   - chunk arrival shows text
 *   - is_final removes the spinner
 *   - droppedBytes > 0 shows the warning banner
 *   - error state shows the error block
 *   - dismiss button fires onDismiss callback
 *
 * Pure presentational: no WS subscription, no hook wiring. Pass a
 * fully-formed CommandOutputState as a prop and assert the DOM.
 */

import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { CommandOutputState } from '../hooks/useCommandOutput';
import { CommandOutputPanel } from './CommandOutputPanel';

// ── Helpers ─────────────────────────────────────────────────────────────

function makeState(overrides: Partial<CommandOutputState> = {}): CommandOutputState {
  return {
    output: '',
    isRunning: false,
    droppedBytes: 0,
    command: null,
    error: null,
    ...overrides,
  };
}

// ── Tests ───────────────────────────────────────────────────────────────

describe('CommandOutputPanel', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders nothing when state is fully empty (no command, no output, not running)', () => {
    const { container } = render(<CommandOutputPanel state={makeState()} />);
    expect(container.firstChild).toBeNull();
  });

  it('renders when a chunk arrives (non-final)', () => {
    render(
      <CommandOutputPanel
        state={makeState({
          command: 'info',
          isRunning: true,
          output: 'streaming content',
        })}
      />,
    );

    // Component testid is the panel wrapper.
    expect(screen.getByTestId('command-output-panel')).toBeInTheDocument();
    expect(screen.getByText('streaming content')).toBeInTheDocument();
    // Header shows /info
    expect(screen.getByText('/info')).toBeInTheDocument();
    // Spinner while running
    expect(screen.getByTestId('command-output-panel').querySelector('.command-output-panel-spinner')).toBeTruthy();
  });

  it('removes the spinner on is_final:true', () => {
    const { rerender } = render(
      <CommandOutputPanel
        state={makeState({
          command: 'info',
          isRunning: true,
          output: 'partial output',
        })}
      />,
    );
    expect(screen.getByTestId('command-output-panel').querySelector('.command-output-panel-spinner')).toBeTruthy();

    rerender(
      <CommandOutputPanel
        state={makeState({
          command: 'info',
          isRunning: false,
          output: 'partial output',
        })}
      />,
    );
    expect(screen.getByTestId('command-output-panel').querySelector('.command-output-panel-spinner')).toBeNull();
  });

  it('shows the dropped-bytes warning banner when droppedBytes > 0', () => {
    render(
      <CommandOutputPanel
        state={makeState({
          command: 'info',
          isRunning: false,
          output: 'truncated',
          droppedBytes: 8192,
        })}
      />,
    );

    const warning = screen.getByRole('alert');
    expect(warning).toBeInTheDocument();
    expect(warning.textContent).toMatch(/Some output was dropped/i);
    expect(warning.textContent).toMatch(/8,?192/);
  });

  it('does NOT show the warning banner when droppedBytes === 0', () => {
    render(
      <CommandOutputPanel
        state={makeState({
          command: 'info',
          isRunning: false,
          output: 'complete',
          droppedBytes: 0,
        })}
      />,
    );

    // Only the role=alert banner is the dropped-bytes one. With droppedBytes=0,
    // there is no role=alert in the rendered tree at all (no error either).
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('renders the error state when state.error is set', () => {
    render(
      <CommandOutputPanel
        state={makeState({
          command: 'foo',
          isRunning: false,
          error: new Error('permission denied'),
        })}
      />,
    );

    const alert = screen.getByRole('alert');
    expect(alert.textContent).toMatch(/permission denied/);
  });

  it('renders the error state even when output is empty', () => {
    render(
      <CommandOutputPanel
        state={makeState({
          error: new Error('network unreachable'),
        })}
      />,
    );

    expect(screen.getByRole('alert').textContent).toMatch(/network unreachable/);
  });

  it('dismiss button fires onDismiss callback', () => {
    const onDismiss = vi.fn();
    render(
      <CommandOutputPanel
        state={makeState({
          command: 'info',
          isRunning: false,
          output: 'done',
        })}
        onDismiss={onDismiss}
      />,
    );

    fireEvent.click(screen.getByLabelText(/dismiss command output/i));
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it('renders the panel after the auto-hide timer when user is not hovering', () => {
    const onDismiss = vi.fn();
    const { container } = render(
      <CommandOutputPanel
        state={makeState({
          command: 'info',
          isRunning: false,
          output: 'done',
        })}
        onDismiss={onDismiss}
      />,
    );
    expect(container.firstChild).not.toBeNull();
    expect(onDismiss).not.toHaveBeenCalled();

    vi.advanceTimersByTime(1999);
    expect(onDismiss).not.toHaveBeenCalled();
    vi.advanceTimersByTime(10);
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it('paues the auto-hide timer while the user hovers', () => {
    const onDismiss = vi.fn();
    const { container } = render(
      <CommandOutputPanel
        state={makeState({
          command: 'info',
          isRunning: false,
          output: 'done',
        })}
        onDismiss={onDismiss}
      />,
    );
    expect(container.firstChild).not.toBeNull();

    // Simulate hover entering.
    fireEvent.mouseEnter(screen.getByTestId('command-output-panel'));
    // Run the full AUTO_HIDE duration with no action from the user. The
    // panel should NOT auto-dismiss while hovered.
    vi.advanceTimersByTime(5000);
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it('does NOT auto-dismiss while still running', () => {
    const onDismiss = vi.fn();
    render(
      <CommandOutputPanel
        state={makeState({
          command: 'info',
          isRunning: true,
          output: 'streaming',
        })}
        onDismiss={onDismiss}
      />,
    );

    vi.advanceTimersByTime(10000);
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it('falls back to "Command" when state.command is null but isRunning is true', () => {
    render(
      <CommandOutputPanel
        state={makeState({
          command: null,
          isRunning: true,
          output: 'unspecified',
        })}
      />,
    );

    // Header shows generic "Command" label.
    expect(screen.getByText(/^Command$/)).toBeInTheDocument();
  });

  it('does not throw or render when output is empty AND not running AND no error', () => {
    const { container } = render(
      <CommandOutputPanel
        state={makeState({
          command: 'info',
          isRunning: false,
          output: '',
        })}
      />,
    );
    // The panel IS rendered because command !== null, even with empty output.
    expect(container.firstChild).not.toBeNull();
    expect(screen.getByText(/no output captured/i)).toBeInTheDocument();
  });
});
