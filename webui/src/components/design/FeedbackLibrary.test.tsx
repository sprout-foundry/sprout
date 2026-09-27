import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import FeedbackLibrary from './FeedbackLibrary';
import type { DesignFeedbackEntry } from '../../services/api/types';

/**
 * SP-140-8 §8a's Feedback library view (the one holdout, now shipped): a
 * global queue over every design/feedback/*.json — one row per target with
 * status and open-annotation count, click-through to the target screen's
 * workbench, resolved rows folded under a divider, and the empty state
 * pointing at where annotations come from.
 */
function entry(overrides: Partial<DesignFeedbackEntry> = {}): DesignFeedbackEntry {
  return {
    name: 'login',
    path: 'design/feedback/login.json',
    status: 'changes-requested',
    annotationCount: 2,
    resolvedCount: 0,
    ...overrides,
  };
}

describe('FeedbackLibrary (SP-140-8 §8a)', () => {
  it('lists pending feedback rows with status and open counts', () => {
    render(
      <FeedbackLibrary
        feedback={[entry(), entry({ name: 'chat', status: 'acknowledged', annotationCount: 3, resolvedCount: 1 })]}
        onSelectAsset={() => {}}
        onSelectTab={() => {}}
      />,
    );
    expect(screen.getByTestId('feedback-library')).toBeTruthy();
    expect(screen.getByTestId('feedback-library-row-login').textContent).toContain('Changes requested');
    expect(screen.getByTestId('feedback-library-row-login').textContent).toContain('2 open');
    expect(screen.getByTestId('feedback-library-row-chat').textContent).toContain('2 open');
    expect(screen.queryByTestId('feedback-library-allclear')).toBeNull();
  });

  it('click-through selects the target screen and switches to the workbench', () => {
    const onSelectAsset = vi.fn();
    const onSelectTab = vi.fn();
    render(<FeedbackLibrary feedback={[entry()]} onSelectAsset={onSelectAsset} onSelectTab={onSelectTab} />);
    fireEvent.click(screen.getByTestId('feedback-library-row-login'));
    expect(onSelectAsset).toHaveBeenCalledWith('design/screens/login.html');
    expect(onSelectTab).toHaveBeenCalledWith('screens');
  });

  it('fully-resolved rows fold under the resolved divider, not pending', () => {
    render(
      <FeedbackLibrary
        feedback={[entry({ annotationCount: 2, resolvedCount: 2, status: 'approved' })]}
        onSelectAsset={() => {}}
        onSelectTab={() => {}}
      />,
    );
    expect(screen.getByTestId('feedback-library-allclear')).toBeTruthy();
    // The resolved row is inside the collapsed <details> — present in the
    // DOM, not in the pending list.
    expect(screen.getByTestId('feedback-library-row-login')).toBeTruthy();
  });

  it('renders the empty state when no feedback files exist', () => {
    render(<FeedbackLibrary feedback={[]} onSelectAsset={() => {}} onSelectTab={() => {}} />);
    expect(screen.getByTestId('feedback-library-empty')).toBeTruthy();
    expect(screen.getByTestId('feedback-library-empty').textContent).toContain('design/feedback');
  });
});
