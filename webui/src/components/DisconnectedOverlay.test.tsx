import { render, screen, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { DisconnectedOverlay } from './DisconnectedOverlay';

vi.mock('./DisconnectedOverlay.css', () => ({}));

const GRACE = 10_000;

describe('DisconnectedOverlay', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('renders nothing while connected', () => {
    render(<DisconnectedOverlay isConnected={true} />);
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('stays hidden for a short disconnect that reconnects within the grace', () => {
    const { rerender } = render(<DisconnectedOverlay isConnected={false} />);
    act(() => void vi.advanceTimersByTime(GRACE - 1000)); // blip, still in grace
    rerender(<DisconnectedOverlay isConnected={true} />); // reconnected
    act(() => void vi.advanceTimersByTime(GRACE));
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('shows the blocking overlay after a sustained disconnect', () => {
    render(<DisconnectedOverlay isConnected={false} />);
    expect(screen.queryByRole('alertdialog')).toBeNull(); // not yet — within grace
    act(() => void vi.advanceTimersByTime(GRACE));
    expect(screen.getByRole('alertdialog')).toBeTruthy();
    expect(screen.getByText(/Disconnected from sprout/i)).toBeTruthy();
  });

  it('hides again once the connection is restored', () => {
    const { rerender } = render(<DisconnectedOverlay isConnected={false} />);
    act(() => void vi.advanceTimersByTime(GRACE));
    expect(screen.getByRole('alertdialog')).toBeTruthy();
    rerender(<DisconnectedOverlay isConnected={true} />);
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });
});
