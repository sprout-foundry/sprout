/**
 * CommandPolicyEditor.test.tsx — Unit tests for the command policy editor UI.
 *
 * Covers:
 * - Renders three sections (Always Allow / Always Ask / Never Allow)
 * - Help text renders
 * - Empty state renders "No rules yet."
 * - Adding a rule calls updateSetting with correct structure
 * - Removing a rule calls updateSetting with the rule filtered out
 * - Existing rules display correctly
 */

import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createElement } from 'react';
import { vi, describe, it, expect, beforeEach } from 'vitest';

import CommandPolicyEditor from './CommandPolicyEditor';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function createMockSettings(commandPolicies?: unknown) {
  return commandPolicies ? { command_policies: commandPolicies } : ({});
}

function renderEditor(settings: Record<string, unknown> | null, updateSetting: (key: string, value: unknown) => Promise<void>) {
  return render(
    createElement(CommandPolicyEditor, {
      settings: settings as any,
      updateSetting,
    }),
  );
}

// ---------------------------------------------------------------------------
// Tests: Render
// ---------------------------------------------------------------------------

describe('render', () => {
  it('renders the three policy sections', async () => {
    const updateSetting = vi.fn().mockResolvedValue(undefined);
    renderEditor({}, updateSetting);

    expect(screen.getByText('Always Allow')).toBeInTheDocument();
    expect(screen.getByText('Always Ask')).toBeInTheDocument();
    expect(screen.getByText('Never Allow')).toBeInTheDocument();
  });

  it('renders the help text', async () => {
    const updateSetting = vi.fn().mockResolvedValue(undefined);
    renderEditor({}, updateSetting);

    expect(screen.getByText(/Rules are checked first-match-wins/)).toBeInTheDocument();
  });

  it('renders "Command Policies" heading', async () => {
    const updateSetting = vi.fn().mockResolvedValue(undefined);
    renderEditor({}, updateSetting);

    expect(screen.getByText('Command Policies')).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Tests: Empty state
// ---------------------------------------------------------------------------

describe('empty state', () => {
  it('shows "No rules yet." for each section when no rules exist', async () => {
    const updateSetting = vi.fn().mockResolvedValue(undefined);
    renderEditor({}, updateSetting);

    const emptyStates = screen.getAllByText('No rules yet.');
    expect(emptyStates).toHaveLength(3);
  });

  it('shows "No rules yet." when settings is null', async () => {
    const updateSetting = vi.fn().mockResolvedValue(undefined);
    renderEditor(null, updateSetting);

    const emptyStates = screen.getAllByText('No rules yet.');
    expect(emptyStates).toHaveLength(3);
  });
});

// ---------------------------------------------------------------------------
// Tests: Adding rules
// ---------------------------------------------------------------------------

describe('adding rules', () => {
  it('adds an "allow" rule when typing in the Always Allow input', async () => {
    const updateSetting = vi.fn().mockResolvedValue(undefined);
    renderEditor({}, updateSetting);

    const allowInput = screen.getAllByPlaceholderText('e.g. npm test')[0];
    await userEvent.type(allowInput, 'npm test');
    await userEvent.type(allowInput, '{Enter}');

    await waitFor(() => {
      expect(updateSetting).toHaveBeenCalledWith('command_policies', {
        rules: [{ pattern: 'npm test', action: 'allow' }],
      });
    });
  });

  it('adds an "ask" rule when typing in the Always Ask input', async () => {
    const updateSetting = vi.fn().mockResolvedValue(undefined);
    renderEditor({}, updateSetting);

    const askInput = screen.getByPlaceholderText('e.g. git push*');
    await userEvent.type(askInput, 'git push*');
    await userEvent.type(askInput, '{Enter}');

    await waitFor(() => {
      expect(updateSetting).toHaveBeenCalledWith('command_policies', {
        rules: [{ pattern: 'git push*', action: 'ask' }],
      });
    });
  });

  it('adds a "deny" rule when typing in the Never Allow input', async () => {
    const updateSetting = vi.fn().mockResolvedValue(undefined);
    renderEditor({}, updateSetting);

    const denyInput = screen.getByPlaceholderText('e.g. kubectl delete*');
    await userEvent.type(denyInput, 'kubectl delete*');
    await userEvent.type(denyInput, '{Enter}');

    await waitFor(() => {
      expect(updateSetting).toHaveBeenCalledWith('command_policies', {
        rules: [{ pattern: 'kubectl delete*', action: 'deny' }],
      });
    });
  });

  it('adds a rule via the Add button', async () => {
    const updateSetting = vi.fn().mockResolvedValue(undefined);
    renderEditor({}, updateSetting);

    const allowInput = screen.getAllByPlaceholderText('e.g. npm test')[0];
    await userEvent.type(allowInput, 'make build');

    // Find the Add button in the same section
    const addButtons = screen.getAllByRole('button', { name: 'Add' });
    await userEvent.click(addButtons[0]);

    await waitFor(() => {
      expect(updateSetting).toHaveBeenCalledWith('command_policies', {
        rules: [{ pattern: 'make build', action: 'allow' }],
      });
    });
  });

  it('does not add an empty rule', async () => {
    const updateSetting = vi.fn().mockResolvedValue(undefined);
    renderEditor({}, updateSetting);

    const allowInput = screen.getAllByPlaceholderText('e.g. npm test')[0];
    await userEvent.type(allowInput, '{Enter}');

    expect(updateSetting).not.toHaveBeenCalled();
  });

  it('accumulates rules across sections', async () => {
    const updateSetting = vi.fn().mockResolvedValue(undefined);
    renderEditor(
      createMockSettings({
        rules: [{ pattern: 'existing', action: 'allow' }],
      }),
      updateSetting,
    );

    const askInput = screen.getByPlaceholderText('e.g. git push*');
    await userEvent.type(askInput, 'git push*');
    await userEvent.type(askInput, '{Enter}');

    await waitFor(() => {
      expect(updateSetting).toHaveBeenCalledWith('command_policies', {
        rules: [
          { pattern: 'existing', action: 'allow' },
          { pattern: 'git push*', action: 'ask' },
        ],
      });
    });
  });
});

// ---------------------------------------------------------------------------
// Tests: Removing rules
// ---------------------------------------------------------------------------

describe('removing rules', () => {
  it('removes a rule when clicking the trash icon', async () => {
    const updateSetting = vi.fn().mockResolvedValue(undefined);
    renderEditor(
      createMockSettings({
        rules: [
          { pattern: 'npm test', action: 'allow' },
          { pattern: 'git push*', action: 'ask' },
        ],
      }),
      updateSetting,
    );

    // Verify the rule is displayed
    expect(screen.getByText('npm test')).toBeInTheDocument();

    // Click the remove button
    const removeBtn = screen.getByRole('button', { name: 'Remove npm test' });
    await userEvent.click(removeBtn);

    await waitFor(() => {
      expect(updateSetting).toHaveBeenCalledWith('command_policies', {
        rules: [{ pattern: 'git push*', action: 'ask' }],
      });
    });
  });

  it('only removes the specific rule, not others with same pattern but different action', async () => {
    const updateSetting = vi.fn().mockResolvedValue(undefined);
    renderEditor(
      createMockSettings({
        rules: [
          { pattern: 'git push', action: 'allow' },
          { pattern: 'git push', action: 'deny' },
        ],
      }),
      updateSetting,
    );

    // There are two remove buttons for the same pattern; click the first (in the Allow section)
    const removeBtns = screen.getAllByRole('button', { name: 'Remove git push' });
    expect(removeBtns).toHaveLength(2);
    await userEvent.click(removeBtns[0]);

    await waitFor(() => {
      expect(updateSetting).toHaveBeenCalledWith('command_policies', {
        rules: [{ pattern: 'git push', action: 'deny' }],
      });
    });
  });
});

// ---------------------------------------------------------------------------
// Tests: Displaying existing rules
// ---------------------------------------------------------------------------

describe('displaying existing rules', () => {
  it('displays rules with their patterns in code tags', async () => {
    const updateSetting = vi.fn().mockResolvedValue(undefined);
    renderEditor(
      createMockSettings({
        rules: [
          { pattern: 'npm test', action: 'allow' },
          { pattern: 'cargo build*', action: 'ask' },
          { pattern: 'sudo rm -rf /', action: 'deny' },
        ],
      }),
      updateSetting,
    );

    const codes = screen.getAllByText('npm test');
    expect(codes.length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('cargo build*')).toBeInTheDocument();
    expect(screen.getByText('sudo rm -rf /')).toBeInTheDocument();
  });

  it('displays optional reason text', async () => {
    const updateSetting = vi.fn().mockResolvedValue(undefined);
    renderEditor(
      createMockSettings({
        rules: [{ pattern: 'git push*', action: 'ask', reason: 'prevent accidental force pushes' }],
      }),
      updateSetting,
    );

    expect(screen.getByText('prevent accidental force pushes')).toBeInTheDocument();
  });
});
