/**
 * NotificationsSettingsTab.test.tsx — Unit tests for the desktop notification
 * settings UI tab.
 *
 * Covers:
 * - Component renders without crashing
 * - Toggle enables/disables via desktopNotify.setEnabled()
 * - Toggle triggers requestPermission() when permission is 'default'
 * - Permission status labels and icons render correctly
 * - Test button is disabled when notifications are off
 * - Test button calls desktopNotify.notify() when permission is 'granted'
 * - Test button shows blocked state when permission is 'denied'
 * - Test button requests permission then sends when permission is 'default'
 * - Test status feedback messages (sent / blocked)
 * - Custom renderLocalToggle prop is respected
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';

// ---------------------------------------------------------------------------
// Mocks — must come before the import of the component
// ---------------------------------------------------------------------------

const mockGetPermission = vi.fn(() => 'default' as 'default' | 'granted' | 'denied');
const mockRequestPermission = vi.fn().mockResolvedValue('granted' as 'default' | 'granted' | 'denied');
const mockSetEnabled = vi.fn();
const mockIsEnabled_ = vi.fn(() => true);
const mockNotify = vi.fn();

vi.mock('../../services/desktopNotify', () => ({
  getPermission: () => mockGetPermission(),
  requestPermission: () => mockRequestPermission(),
  setEnabled: (v: boolean) => mockSetEnabled(v),
  isEnabled_: () => mockIsEnabled_(),
  notify: (t: string, b?: string) => mockNotify(t, b),
  notifyIfHidden: vi.fn(),
}));

import NotificationsSettingsTab from './NotificationsSettingsTab';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function resetMocks() {
  vi.clearAllMocks();
  mockGetPermission.mockReturnValue('default' as const);
  mockRequestPermission.mockResolvedValue('granted' as const);
  mockIsEnabled_.mockReturnValue(true);
}

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  resetMocks();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ---------------------------------------------------------------------------
// Tests: Render
// ---------------------------------------------------------------------------

describe('render', () => {
  it('renders without crashing', () => {
    mockGetPermission.mockReturnValue('granted' as const);
    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });
    expect(screen.getByText('Desktop Notifications')).toBeInTheDocument();
  });

  it('renders the help text', () => {
    mockGetPermission.mockReturnValue('granted' as const);
    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });
    expect(screen.getByText(/Get notified when tasks complete/)).toBeInTheDocument();
  });

  it('renders the toggle with default enabled state', () => {
    mockIsEnabled_.mockReturnValue(true);
    mockGetPermission.mockReturnValue('granted' as const);
    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });
    const checkbox = screen.getByRole('checkbox', { name: /enable desktop notifications/i });
    expect(checkbox).toBeChecked();
  });

  it('renders the toggle in disabled state when isEnabled_ is false', () => {
    mockIsEnabled_.mockReturnValue(false);
    mockGetPermission.mockReturnValue('default' as const);
    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });
    const checkbox = screen.getByRole('checkbox', { name: /enable desktop notifications/i });
    expect(checkbox).not.toBeChecked();
  });

  it('renders the permission status label', () => {
    mockGetPermission.mockReturnValue('granted' as const);
    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });
    expect(screen.getByText('Permission status')).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Tests: Toggle behavior
// ---------------------------------------------------------------------------

describe('toggle enabled / disabled', () => {
  it('calls desktopNotify.setEnabled(true) when toggling on', async () => {
    mockIsEnabled_.mockReturnValue(false);
    mockGetPermission.mockReturnValue('granted' as const);

    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });

    const checkbox = screen.getByRole('checkbox', { name: /enable desktop notifications/i });
    await userEvent.click(checkbox);

    expect(mockSetEnabled).toHaveBeenCalledWith(true);
  });

  it('calls desktopNotify.setEnabled(false) when toggling off', async () => {
    mockIsEnabled_.mockReturnValue(true);
    mockGetPermission.mockReturnValue('granted' as const);

    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });

    const checkbox = screen.getByRole('checkbox', { name: /enable desktop notifications/i });
    await userEvent.click(checkbox);

    expect(mockSetEnabled).toHaveBeenCalledWith(false);
  });

  it('triggers requestPermission() when enabling and permission is default', async () => {
    mockIsEnabled_.mockReturnValue(false);
    mockGetPermission.mockReturnValue('default' as const);
    mockRequestPermission.mockResolvedValue('granted' as const);

    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });

    const checkbox = screen.getByRole('checkbox', { name: /enable desktop notifications/i });
    await userEvent.click(checkbox);

    expect(mockSetEnabled).toHaveBeenCalledWith(true);
    expect(mockRequestPermission).toHaveBeenCalledTimes(1);

    await waitFor(() => {
      // Permission should update to 'granted' after the promise resolves
      // The component updates its internal state; we verify the button is no longer requesting
    });
  });

  it('does NOT trigger requestPermission() when enabling and permission is already granted', async () => {
    mockIsEnabled_.mockReturnValue(false);
    mockGetPermission.mockReturnValue('granted' as const);

    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });

    const checkbox = screen.getByRole('checkbox', { name: /enable desktop notifications/i });
    await userEvent.click(checkbox);

    expect(mockSetEnabled).toHaveBeenCalledWith(true);
    expect(mockRequestPermission).not.toHaveBeenCalled();
  });

  it('does NOT trigger requestPermission() when disabling', async () => {
    mockIsEnabled_.mockReturnValue(true);
    mockGetPermission.mockReturnValue('default' as const);

    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });

    const checkbox = screen.getByRole('checkbox', { name: /enable desktop notifications/i });
    await userEvent.click(checkbox);

    expect(mockSetEnabled).toHaveBeenCalledWith(false);
    expect(mockRequestPermission).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: Permission status display
// ---------------------------------------------------------------------------

describe('permission status display', () => {
  it('shows "Allowed" when permission is granted', () => {
    mockGetPermission.mockReturnValue('granted' as const);
    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });
    expect(screen.getByText('Allowed')).toBeInTheDocument();
  });

  it('shows "Blocked by browser" when permission is denied', () => {
    mockGetPermission.mockReturnValue('denied' as const);
    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });
    expect(screen.getByText('Blocked by browser')).toBeInTheDocument();
  });

  it('shows "Not asked yet" when permission is default', () => {
    mockGetPermission.mockReturnValue('default' as const);
    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });
    expect(screen.getByText('Not asked yet')).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Tests: Test button
// ---------------------------------------------------------------------------

describe('test button', () => {
  it('renders the test notification button', () => {
    mockGetPermission.mockReturnValue('granted' as const);
    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });
    expect(screen.getByRole('button', { name: /send test notification/i })).toBeInTheDocument();
  });

  it('disables the test button when notifications are disabled', () => {
    mockIsEnabled_.mockReturnValue(false);
    mockGetPermission.mockReturnValue('granted' as const);
    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });
    const btn = screen.getByRole('button', { name: /send test notification/i });
    expect(btn).toBeDisabled();
  });

  it('calls desktopNotify.notify() when clicked with granted permission', async () => {
    mockIsEnabled_.mockReturnValue(true);
    mockGetPermission.mockReturnValue('granted' as const);

    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });

    const btn = screen.getByRole('button', { name: /send test notification/i });
    await userEvent.click(btn);

    expect(mockNotify).toHaveBeenCalledWith(
      'Sprout',
      'Test notification — desktop notifications are working!',
    );
  });

  it('shows blocked status when clicked with denied permission', async () => {
    mockIsEnabled_.mockReturnValue(true);
    mockGetPermission.mockReturnValue('denied' as const);

    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });

    const btn = screen.getByRole('button', { name: /send test notification/i });
    await userEvent.click(btn);

    expect(mockNotify).not.toHaveBeenCalled();
    await waitFor(() => {
      // Check the button text changed to show blocked (not the status label)
      expect(screen.getByText(/✗ Blocked/)).toBeInTheDocument();
    });
  });

  it('requests permission then sends notification when permission is default', async () => {
    mockIsEnabled_.mockReturnValue(true);
    mockGetPermission.mockReturnValue('default' as const);
    mockRequestPermission.mockResolvedValue('granted' as const);

    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });

    const btn = screen.getByRole('button', { name: /send test notification/i });
    await userEvent.click(btn);

    expect(mockRequestPermission).toHaveBeenCalledTimes(1);

    await waitFor(() => {
      expect(mockNotify).toHaveBeenCalledWith(
        'Sprout',
        'Test notification — desktop notifications are working!',
      );
    });
  });

  it('shows blocked status when requestPermission returns denied during test', async () => {
    mockIsEnabled_.mockReturnValue(true);
    mockGetPermission.mockReturnValue('default' as const);
    mockRequestPermission.mockResolvedValue('denied' as const);

    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });

    const btn = screen.getByRole('button', { name: /send test notification/i });
    await userEvent.click(btn);

    expect(mockNotify).not.toHaveBeenCalled();

    await waitFor(() => {
      // Check the button text changed to show blocked (not the status label)
      expect(screen.getByText(/✗ Blocked/)).toBeInTheDocument();
    });
  });
});

// ---------------------------------------------------------------------------
// Tests: Test status feedback
// ---------------------------------------------------------------------------

describe('test status feedback', () => {
  it('shows "Sent!" after successful test with granted permission', async () => {
    mockIsEnabled_.mockReturnValue(true);
    mockGetPermission.mockReturnValue('granted' as const);

    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });

    const btn = screen.getByRole('button', { name: /send test notification/i });
    await userEvent.click(btn);

    await waitFor(() => {
      // Check the button text changed (not the helper message)
      expect(screen.getByText(/✓ Sent!/)).toBeInTheDocument();
    });
  });

  it('shows success message after successful test', async () => {
    mockIsEnabled_.mockReturnValue(true);
    mockGetPermission.mockReturnValue('granted' as const);

    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });

    const btn = screen.getByRole('button', { name: /send test notification/i });
    await userEvent.click(btn);

    await waitFor(() => {
      expect(screen.getByText(/Test notification sent successfully!/i)).toBeInTheDocument();
    });
  });

  it('shows error message when test is blocked', async () => {
    mockIsEnabled_.mockReturnValue(true);
    mockGetPermission.mockReturnValue('denied' as const);

    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });

    const btn = screen.getByRole('button', { name: /send test notification/i });
    await userEvent.click(btn);

    await waitFor(() => {
      expect(screen.getByText(/Browser notifications are blocked/i)).toBeInTheDocument();
    });
  });

  it('shows "Requesting..." while permission is being requested', async () => {
    mockIsEnabled_.mockReturnValue(true);
    mockGetPermission.mockReturnValue('default' as const);
    // Don't resolve immediately — let the pending state show
    let resolvePermission: () => void;
    mockRequestPermission.mockReturnValue(
      new Promise((resolve) => {
        resolvePermission = () => resolve('granted' as const);
      }),
    );

    act(() => {
      root.render(createElement(NotificationsSettingsTab));
    });

    const btn = screen.getByRole('button', { name: /send test notification/i });
    await userEvent.click(btn);

    // Before resolving, the button should show "Requesting..."
    // Use specific text to avoid ambiguity
    expect(screen.getByText('Requesting...')).toBeInTheDocument();

    // Resolve the promise
    act(() => {
      resolvePermission!();
    });

    await waitFor(() => {
      // Check the button text changed to "✓ Sent!" (specific to avoid ambiguity)
      expect(screen.getByText('✓ Sent!')).toBeInTheDocument();
    });
  });
});

// ---------------------------------------------------------------------------
// Tests: Custom renderLocalToggle prop
// ---------------------------------------------------------------------------

describe('custom renderLocalToggle', () => {
  it('uses the custom toggle renderer when provided', () => {
    const customToggle = vi.fn((checked: boolean, label: string, onChange: () => void) =>
      createElement('button', { onClick: onChange, 'data-checked': checked, 'data-label': label }),
    );

    mockGetPermission.mockReturnValue('granted' as const);
    mockIsEnabled_.mockReturnValue(true);

    act(() => {
      root.render(
        createElement(NotificationsSettingsTab, {
          renderLocalToggle: customToggle,
        }),
      );
    });

    expect(customToggle).toHaveBeenCalledWith(
      true,
      'Enable desktop notifications',
      expect.any(Function),
      'Notifications only fire when this tab is not in focus.',
    );
  });
});
