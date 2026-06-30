import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import React from 'react';
import ShellApprovalPanel from './ShellApprovalPanel';

vi.mock('./ShellApprovalPanel.css', () => ({}));

// ── Helpers ────────────────────────────────────────────────────────────────

const TEST_PARTS = [
  { id: 'part-1', text: 'echo hello', kind: 'builtin', semantic: 'print text', risk: 'Low' },
  { id: 'part-2', text: 'rm -rf /tmp/data', kind: 'dangerous', semantic: 'recursive delete', risk: 'High' },
];

const TEST_REQUEST = {
  request_id: 'test-req-001',
  command: 'echo hello && rm -rf /tmp/data',
  parts: TEST_PARTS,
  unified_view: 'echo hello\nrm -rf /tmp/data',
  risk_level: 'High',
};

function renderPanel(props?: Partial<React.ComponentProps<typeof ShellApprovalPanel>>) {
  return render(
    <ShellApprovalPanel
      request={props?.request ?? TEST_REQUEST}
      onSubmit={props?.onSubmit ?? vi.fn()}
      onCancel={props?.onCancel ?? vi.fn()}
    />,
  );
}

// ── Tests ──────────────────────────────────────────────────────────────────

describe('ShellApprovalPanel', () => {
  it('renders for a 2-part proposal with one Low-risk and one High-risk part', () => {
    renderPanel();

    // Title is present
    expect(screen.getByText('Shell Command Approval')).toBeInTheDocument();

    // Full command is rendered
    expect(screen.getByTestId('shell-approval-command')).toHaveTextContent('echo hello && rm -rf /tmp/data');

    // Both parts are rendered
    expect(screen.getByTestId('shell-approval-part-part-1')).toBeInTheDocument();
    expect(screen.getByTestId('shell-approval-part-part-2')).toBeInTheDocument();

    // Part text is visible
    expect(screen.getByText('echo hello')).toBeInTheDocument();
    expect(screen.getByText('rm -rf /tmp/data')).toBeInTheDocument();
  });

  it('default state: High-risk part is pending, Low-risk part is approved', () => {
    renderPanel();

    // Low-risk part (part-1) should be approved by default → shows ✓
    const part1 = screen.getByTestId('shell-approval-part-part-1');
    expect(part1).toHaveClass('shell-approval-part--approved');
    expect(part1.querySelector('.shell-approval-part-icon--approved')).toBeInTheDocument();

    // High-risk part (part-2) should be pending by default → shows ?
    const part2 = screen.getByTestId('shell-approval-part-part-2');
    expect(part2).toHaveClass('shell-approval-part--pending');
    expect(part2.querySelector('.shell-approval-part-icon--pending')).toBeInTheDocument();
  });

  it('clicking the toggle on a pending part → approved', async () => {
    renderPanel();

    // part-2 is pending by default
    const toggle = screen.getByTestId('shell-approval-part-toggle-part-2');
    expect(toggle).toHaveTextContent('Pending');

    fireEvent.click(toggle);

    // Should cycle to Approved
    expect(toggle).toHaveTextContent('Approved');
    expect(screen.getByTestId('shell-approval-part-part-2')).toHaveClass('shell-approval-part--approved');
  });

  it('clicking the toggle on an approved part → rejected', async () => {
    renderPanel();

    // part-1 is approved by default
    const toggle = screen.getByTestId('shell-approval-part-toggle-part-1');
    expect(toggle).toHaveTextContent('Approved');

    fireEvent.click(toggle);

    // Should cycle to Rejected
    expect(toggle).toHaveTextContent('Rejected');
    expect(screen.getByTestId('shell-approval-part-part-1')).toHaveClass('shell-approval-part--rejected');
  });

  it('clicking "Accept all" → all parts are approved', () => {
    renderPanel();

    // part-2 starts as pending
    expect(screen.getByTestId('shell-approval-part-part-2')).toHaveClass('shell-approval-part--pending');

    fireEvent.click(screen.getByTestId('shell-approval-accept-all'));

    expect(screen.getByTestId('shell-approval-part-part-1')).toHaveClass('shell-approval-part--approved');
    expect(screen.getByTestId('shell-approval-part-part-2')).toHaveClass('shell-approval-part--approved');
  });

  it('clicking "Reject all" → all parts are rejected', () => {
    renderPanel();

    // part-1 starts as approved
    expect(screen.getByTestId('shell-approval-part-part-1')).toHaveClass('shell-approval-part--approved');

    fireEvent.click(screen.getByTestId('shell-approval-reject-all'));

    expect(screen.getByTestId('shell-approval-part-part-1')).toHaveClass('shell-approval-part--rejected');
    expect(screen.getByTestId('shell-approval-part-part-2')).toHaveClass('shell-approval-part--rejected');
  });

  it('submit fires onSubmit with a Record<string, boolean> decisions map', async () => {
    const onSubmit = vi.fn();
    renderPanel({ onSubmit });

    // Default: part-1 = approved (true), part-2 = pending (false for safety)
    fireEvent.click(screen.getByTestId('shell-approval-submit'));

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith({
        'part-1': true,
        'part-2': false,
      });
    });
  });
});
