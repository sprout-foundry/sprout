/**
 * Tests for the RoleSelector component.
 *
 * Covers:
 * - Empty state rendering (no roles returned from API)
 * - Role list rendering with names and descriptions
 * - Loading state while fetching roles
 * - Error state when API fails
 * - Role selection triggers onRoleChange with role name
 * - Deselection (None) triggers onRoleChange with null
 * - Refresh button reloads roles from API
 * - Default selected state when selectedRole prop is null
 * - Selected state when selectedRole prop is set to a role name
 */

import { act, createElement, type ReactNode } from 'react';
import { fireEvent } from '@testing-library/react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, beforeEach, beforeAll, afterEach, test, describe, expect } from 'vitest';

// ---------------------------------------------------------------------------
// Mock rolesApi — all mocks created inside the factory
// ---------------------------------------------------------------------------

vi.mock('../services/api/rolesApi', () => ({
  listRoles: vi.fn(),
  createRole: vi.fn(),
  updateRole: vi.fn(),
  deleteRole: vi.fn(),
  getRole: vi.fn(),
}));

// ---------------------------------------------------------------------------
// Mock SproutAdapterContext — fully self-contained factory
// ---------------------------------------------------------------------------

vi.mock('../contexts/SproutAdapterContext', () => {
  const _mockFetch = vi.fn();
  return {
    useSproutFetch: vi.fn(() => _mockFetch),
    useSproutAdapter: vi.fn(() => ({ fetch: _mockFetch })),
    SproutAdapterContext: {
      Provider: ({ children }: { children: any }) => children,
    },
    SproutAdapterProvider: ({ children }: { children: any }) => children,
  };
});

// ---------------------------------------------------------------------------
// Mock CSS import
// ---------------------------------------------------------------------------

vi.mock('./RoleSelector.css', () => ({}));

// ---------------------------------------------------------------------------
// Import the component AFTER mocks are set up
// ---------------------------------------------------------------------------

import * as rolesApi from '../services/api/rolesApi';
import { RoleSelector } from './RoleSelector';

// ---------------------------------------------------------------------------
// Test Setup
// ---------------------------------------------------------------------------

let container: HTMLDivElement | null = null;
let root: Root | null = null;

beforeAll(() => {
  // @ts-expect-error — React expects this global
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  vi.clearAllMocks();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  if (container) {
    document.body.removeChild(container);
    container = null;
  }
});

// ---------------------------------------------------------------------------
// Helper Functions
// ---------------------------------------------------------------------------

function renderComponent(selectedRole: string | null = null, onRoleChange?: (role: string | null) => void) {
  act(() => {
    root?.render(
      createElement(RoleSelector, {
        selectedRole,
        onRoleChange: onRoleChange ?? vi.fn(),
      }),
    );
  });
}

function getByText(text: string | RegExp): HTMLElement | null {
  if (!container) return null;
  const elements = Array.from(container.querySelectorAll('*'));
  for (const el of elements) {
    if (el.children.length === 0) {
      const content = (el.textContent ?? '').trim();
      if (content.length > 0) {
        if (text instanceof RegExp && text.test(content)) {
          return el;
        }
        if (!(text instanceof RegExp) && content === text) {
          return el;
        }
      }
    }
  }
  // Fallback: search elements with children
  for (const el of elements) {
    const content = (el.textContent ?? '').trim();
    if (content.length > 0 && content.length <= 100) {
      if (text instanceof RegExp && text.test(content)) {
        return el;
      }
      if (!(text instanceof RegExp) && content === text) {
        return el;
      }
    }
  }
  return null;
}

function getBySelector(selector: string): HTMLElement | null {
  if (!container) return null;
  return container.querySelector(selector);
}

function getSelect(): HTMLSelectElement | null {
  return getBySelector('select#role-select') as HTMLSelectElement | null;
}

function getRefreshButton(): HTMLButtonElement | null {
  return getBySelector('button.role-selector-refresh') as HTMLButtonElement | null;
}

function selectOption(value: string) {
  act(() => {
    fireEvent.change(getSelect()!, { target: { value } });
  });
}

async function waitForRender() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

async function waitForAsync() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 100));
  });
}

// ---------------------------------------------------------------------------
// Tests: Initial Loading State
// ---------------------------------------------------------------------------

describe('RoleSelector - Loading State', () => {
  test('shows "Loading roles..." while API call is pending', async () => {
    rolesApi.listRoles.mockImplementation(
      () =>
        new Promise((resolve) => {
          setTimeout(() => resolve([]), 100);
        }),
    );

    renderComponent();

    expect(getByText('Loading roles...')).toBeTruthy();
  });

  test('disables the select and refresh button while loading', async () => {
    rolesApi.listRoles.mockImplementation(
      () =>
        new Promise((resolve) => {
          setTimeout(() => resolve([]), 100);
        }),
    );

    renderComponent();

    const select = getSelect();
    expect(select).toBeTruthy();
    expect(select!.hasAttribute('disabled')).toBeTruthy();

    const refreshBtn = getRefreshButton();
    expect(refreshBtn).toBeTruthy();
    expect(refreshBtn!.hasAttribute('disabled')).toBeTruthy();
  });
});

// ---------------------------------------------------------------------------
// Tests: Empty State (No Roles)
// ---------------------------------------------------------------------------

describe('RoleSelector - Empty State', () => {
  test('shows "None" option when API returns empty list', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    expect(getByText('None')).toBeTruthy();
  });

  test('shows "No custom roles defined" when API returns empty list', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    expect(getByText('No custom roles defined')).toBeTruthy();
  });

  test('calls listRoles on mount', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    expect(rolesApi.listRoles).toHaveBeenCalledTimes(1);
  });

  test('enables select after loading completes', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    const select = getSelect();
    expect(select).toBeTruthy();
    expect(select!.hasAttribute('disabled')).toBeFalsy();
  });
});

// ---------------------------------------------------------------------------
// Tests: Role List Rendering
// ---------------------------------------------------------------------------

describe('RoleSelector - Role List Rendering', () => {
  test('renders roles with names and descriptions', async () => {
    rolesApi.listRoles.mockResolvedValue([
      { name: 'agent', description: 'Default agent role' },
      { name: 'coder', description: 'Software development role' },
    ]);

    renderComponent();

    await waitForRender();

    expect(getByText('agent — Default agent role')).toBeTruthy();
    expect(getByText('coder — Software development role')).toBeTruthy();
  });

  test('renders roles with no description', async () => {
    rolesApi.listRoles.mockResolvedValue([{ name: 'minimal' }]);

    renderComponent();

    await waitForRender();

    expect(getByText('minimal')).toBeTruthy();
  });

  test('renders roles with special characters in name', async () => {
    rolesApi.listRoles.mockResolvedValue([{ name: 'my/role', description: 'Has slash' }]);

    renderComponent();

    await waitForRender();

    expect(getByText('my/role — Has slash')).toBeTruthy();
  });

  test('renders roles with empty description', async () => {
    rolesApi.listRoles.mockResolvedValue([{ name: 'nolabel', description: '' }]);

    renderComponent();

    await waitForRender();

    expect(getByText('nolabel')).toBeTruthy();
  });

  test('renders "None" option alongside roles', async () => {
    rolesApi.listRoles.mockResolvedValue([{ name: 'agent', description: 'Agent role' }]);

    renderComponent();

    await waitForRender();

    expect(getByText('None')).toBeTruthy();
    expect(getByText('agent — Agent role')).toBeTruthy();
  });

  test('renders Custom Roles optgroup label', async () => {
    rolesApi.listRoles.mockResolvedValue([{ name: 'test', description: 'Test role' }]);

    renderComponent();

    await waitForRender();

    const optgroup = getBySelector('optgroup');
    expect(optgroup).toBeTruthy();
    expect(optgroup!.getAttribute('label')).toBe('Custom Roles');
  });

  test('renders refresh button with aria-label', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    const refreshBtn = getRefreshButton();
    expect(refreshBtn).toBeTruthy();
    expect(refreshBtn!.getAttribute('aria-label')).toBe('Refresh roles');
  });

  test('renders Role label', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    const label = container!.querySelector('label[for="role-select"]');
    expect(label).toBeTruthy();
    expect(label!.textContent).toContain('Role');
  });
});

// ---------------------------------------------------------------------------
// Tests: Role Selection
// ---------------------------------------------------------------------------

describe('RoleSelector - Role Selection', () => {
  test('calls onRoleChange with role name when a role is selected', async () => {
    const onRoleChangeMock = vi.fn();
    rolesApi.listRoles.mockResolvedValue([
      { name: 'agent', description: 'Agent role' },
    ]);

    renderComponent(null, onRoleChangeMock);

    await waitForRender();

    selectOption('agent');

    expect(onRoleChangeMock).toHaveBeenCalledWith('agent');
  });

  test('calls onRoleChange with null when None is selected', async () => {
    const onRoleChangeMock = vi.fn();
    rolesApi.listRoles.mockResolvedValue([
      { name: 'agent', description: 'Agent role' },
    ]);

    renderComponent(null, onRoleChangeMock);

    await waitForRender();

    selectOption('');

    expect(onRoleChangeMock).toHaveBeenCalledWith(null);
  });

  test('sets select value to selectedRole when prop is provided', async () => {
    rolesApi.listRoles.mockResolvedValue([
      { name: 'agent', description: 'Agent role' },
      { name: 'coder', description: 'Coder role' },
    ]);

    renderComponent('coder');

    await waitForRender();

    const select = getSelect();
    expect(select).toBeTruthy();
    expect(select!.value).toBe('coder');
  });

  test('sets select value to empty string when selectedRole is null', async () => {
    rolesApi.listRoles.mockResolvedValue([
      { name: 'agent', description: 'Agent role' },
    ]);

    renderComponent(null);

    await waitForRender();

    const select = getSelect();
    expect(select).toBeTruthy();
    expect(select!.value).toBe('');
  });

  test('deselecting from a selected role calls onRoleChange with null', async () => {
    const onRoleChangeMock = vi.fn();
    rolesApi.listRoles.mockResolvedValue([
      { name: 'agent', description: 'Agent role' },
    ]);

    renderComponent('agent', onRoleChangeMock);

    await waitForRender();

    // Verify select shows the pre-selected role
    const select = getSelect();
    expect(select!.value).toBe('agent');

    // Now deselect
    selectOption('');

    expect(onRoleChangeMock).toHaveBeenCalledWith(null);
  });
});

// ---------------------------------------------------------------------------
// Tests: Refresh Button
// ---------------------------------------------------------------------------

describe('RoleSelector - Refresh Button', () => {
  test('clicking refresh button calls listRoles again', async () => {
    rolesApi.listRoles.mockResolvedValue([{ name: 'agent', description: 'Agent' }]);

    renderComponent();

    await waitForRender();

    expect(rolesApi.listRoles).toHaveBeenCalledTimes(1);

    const refreshBtn = getRefreshButton();
    act(() => {
      refreshBtn!.click();
    });

    await waitForRender();

    expect(rolesApi.listRoles).toHaveBeenCalledTimes(2);
  });

  test('refresh button is disabled during loading', async () => {
    rolesApi.listRoles.mockImplementation(
      () =>
        new Promise((resolve) => {
          setTimeout(() => resolve([{ name: 'agent' }]), 100);
        }),
    );

    renderComponent();

    await waitForRender();

    // Now click refresh to trigger loading
    rolesApi.listRoles.mockImplementation(
      () =>
        new Promise((resolve) => {
          setTimeout(() => resolve([{ name: 'agent' }]), 100);
        }),
    );

    const refreshBtn = getRefreshButton();
    act(() => {
      refreshBtn!.click();
    });

    expect(refreshBtn!.hasAttribute('disabled')).toBeTruthy();
  });

  test('refresh after error state reloads roles successfully', async () => {
    rolesApi.listRoles.mockRejectedValue(new Error('Network error'));

    renderComponent();

    await waitForAsync();

    // Should show error state
    expect(getByText(/Error:/)).toBeTruthy();

    // Now make listRoles succeed
    rolesApi.listRoles.mockResolvedValue([{ name: 'recovered', description: 'Recovered role' }]);

    const refreshBtn = getRefreshButton();
    act(() => {
      refreshBtn!.click();
    });

    await waitForRender();

    expect(getByText('recovered — Recovered role')).toBeTruthy();
    expect(getByText(/Error:/)).toBeFalsy();
  });
});

// ---------------------------------------------------------------------------
// Tests: Error State
// ---------------------------------------------------------------------------

describe('RoleSelector - Error State', () => {
  test('shows error message when listRoles fails', async () => {
    rolesApi.listRoles.mockRejectedValue(new Error('Network error'));

    renderComponent();

    await waitForAsync();

    expect(getByText(/Error: Failed to load roles\. Click refresh to try again/)).toBeTruthy();
  });

  test('shows error message for non-Error thrown values', async () => {
    rolesApi.listRoles.mockRejectedValue('Just a string error');

    renderComponent();

    await waitForAsync();

    expect(getByText(/Error:.*Failed to load roles/)).toBeTruthy();
  });

  test('clears error when refresh succeeds after a failure', async () => {
    rolesApi.listRoles.mockRejectedValue(new Error('Network error'));

    renderComponent();

    await waitForAsync();

    expect(getByText(/Error:/)).toBeTruthy();

    // Refresh with successful response
    rolesApi.listRoles.mockResolvedValue([{ name: 'good', description: 'Good role' }]);

    const refreshBtn = getRefreshButton();
    act(() => {
      refreshBtn!.click();
    });

    await waitForRender();

    expect(getByText(/Error:/)).toBeFalsy();
    expect(getByText('good — Good role')).toBeTruthy();
  });

  test('enables select after error state resolves', async () => {
    rolesApi.listRoles.mockRejectedValue(new Error('Network error'));

    renderComponent();

    await waitForAsync();

    const select = getSelect();
    expect(select!.hasAttribute('disabled')).toBeFalsy();
  });
});

// ---------------------------------------------------------------------------
// Tests: Component Structure
// ---------------------------------------------------------------------------

describe('RoleSelector - Component Structure', () => {
  test('renders with role-selector class', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    expect(getBySelector('.role-selector')).toBeTruthy();
  });

  test('renders select with correct id', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    const select = getBySelector('#role-select');
    expect(select).toBeTruthy();
  });

  test('renders label with htmlFor matching select id', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    const label = container!.querySelector('label[for="role-select"]');
    expect(label).toBeTruthy();
  });
});
