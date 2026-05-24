/**
 * Tests for the RolesSettingsTab component.
 *
 * Covers:
 * - Loading state and empty state rendering
 * - Role list rendering with names and descriptions
 * - Add role form: opening, field presence, name disabled when editing
 * - Form submission: create, update, delete
 * - Cancel button behavior
 * - Name validation (submit without name shows error notification)
 * - Error handling for API failures
 */

import { act, createElement } from 'react';
import { fireEvent } from '@testing-library/react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, beforeEach, beforeAll, afterEach, test, describe, expect } from 'vitest';

// ---------------------------------------------------------------------------
// Mock lucide-react icons (required for jsdom — icons fail to render)
// ---------------------------------------------------------------------------

vi.mock('lucide-react', () => ({
  Pencil: vi.fn(({ children, ...props }) =>
    createElement('svg', { ...props, 'data-icon': 'Pencil' }, children),
  ),
  Plus: vi.fn(({ children, ...props }) =>
    createElement('svg', { ...props, 'data-icon': 'Plus' }, children),
  ),
  Trash2: vi.fn(({ children, ...props }) =>
    createElement('svg', { ...props, 'data-icon': 'Trash2' }, children),
  ),
}));

// ---------------------------------------------------------------------------
// Mock rolesApi — all mocks created inside the factory
// ---------------------------------------------------------------------------

vi.mock('../../services/api/rolesApi', () => ({
  listRoles: vi.fn(),
  createRole: vi.fn(),
  updateRole: vi.fn(),
  deleteRole: vi.fn(),
  getRole: vi.fn(),
}));

// ---------------------------------------------------------------------------
// Mock SproutAdapterContext — fully self-contained factory
// ---------------------------------------------------------------------------

vi.mock('../../contexts/SproutAdapterContext', () => {
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

vi.mock('./RoleEditor.css', () => ({}));

// ---------------------------------------------------------------------------
// Import the component AFTER mocks are set up
// ---------------------------------------------------------------------------

import * as rolesApi from '../../services/api/rolesApi';
import { RolesSettingsTab } from './RolesSettingsTab';

// ---------------------------------------------------------------------------
// Test Setup
// ---------------------------------------------------------------------------

const mockAddNotification = vi.fn();

let container: HTMLDivElement | null = null;
let root: Root | null = null;

beforeAll(() => {
  // @ts-expect-error — React expects this global
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  // Mock window.confirm for delete confirmation dialogs
  global.confirm = vi.fn(() => true);
});

beforeEach(() => {
  vi.clearAllMocks();
  mockAddNotification.mockClear();
  // @ts-expect-error — reset confirm mock
  global.confirm.mockClear();
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

function renderComponent() {
  act(() => {
    root?.render(
      createElement(RolesSettingsTab, {
        addNotification: mockAddNotification,
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

function getAllBySelector(selector: string): HTMLElement[] {
  if (!container) return [];
  return Array.from(container.querySelectorAll(selector));
}

function getButtonByText(text: string | RegExp): HTMLButtonElement | null {
  if (!container) return null;
  const buttons = Array.from(container.querySelectorAll('button'));
  for (const btn of buttons) {
    const content = (btn.textContent ?? '').trim();
    if (text instanceof RegExp && text.test(content)) {
      return btn;
    }
    if (!(text instanceof RegExp) && content === text) {
      return btn;
    }
  }
  return null;
}

function clickButton(btn: HTMLButtonElement) {
  act(() => {
    btn.click();
  });
}

function setInputValue(input: HTMLInputElement | HTMLTextAreaElement, value: string) {
  act(() => {
    fireEvent.change(input, { target: { value } });
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
// Tests: Loading & Empty States
// ---------------------------------------------------------------------------

describe('RolesSettingsTab - Loading & Empty States', () => {
  test('shows loading state initially while fetching roles', async () => {
    rolesApi.listRoles.mockImplementation(
      () =>
        new Promise((resolve) => {
          setTimeout(() => resolve([]), 100);
        }),
    );

    renderComponent();

    expect(getByText('Loading roles...')).toBeTruthy();
  });

  test('shows "No roles yet" when API returns empty list', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    expect(getByText(/No roles yet/)).toBeTruthy();
  });

  test('calls listRoles on mount', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    expect(rolesApi.listRoles).toHaveBeenCalledTimes(1);
  });
});

// ---------------------------------------------------------------------------
// Tests: Role List Rendering
// ---------------------------------------------------------------------------

describe('RolesSettingsTab - Role List Rendering', () => {
  test('renders list of roles with names and descriptions', async () => {
    rolesApi.listRoles.mockResolvedValue([
      { name: 'agent', description: 'Default agent role' },
      { name: 'coder', description: 'Software development role', system_prompt: 'You are a coder' },
    ]);

    renderComponent();

    await waitForRender();

    expect(getByText('agent')).toBeTruthy();
    expect(getByText('Default agent role')).toBeTruthy();
    expect(getByText('coder')).toBeTruthy();
    expect(getByText('Software development role')).toBeTruthy();
  });

  test('renders roles with no description', async () => {
    rolesApi.listRoles.mockResolvedValue([{ name: 'minimal' }]);

    renderComponent();

    await waitForRender();

    expect(getByText('minimal')).toBeTruthy();
  });

  test('renders action buttons for each role', async () => {
    rolesApi.listRoles.mockResolvedValue([
      { name: 'agent', description: 'Test role' },
      { name: 'coder', description: 'Another role' },
    ]);

    renderComponent();

    await waitForRender();

    // 2 roles * 2 buttons (edit + delete) + 1 add role button = 5 minimum
    const buttons = getAllBySelector('button');
    expect(buttons.length).toBeGreaterThanOrEqual(5);
  });

  test('renders "Add role" button', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    expect(getButtonByText(/Add role/)).toBeTruthy();
  });
});

// ---------------------------------------------------------------------------
// Tests: Add Role
// ---------------------------------------------------------------------------

describe('RolesSettingsTab - Add Role', () => {
  test('clicking "Add role" opens the form with fields visible', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    const addButton = getButtonByText(/Add role/);
    expect(addButton).toBeTruthy();
    clickButton(addButton!);

    const formContainer = getBySelector('.crud-inline-form');
    expect(formContainer).toBeTruthy();
    expect(getByText('New Role')).toBeTruthy();
  });

  test('form includes Name and Persona fields', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    const addButton = getButtonByText(/Add role/);
    clickButton(addButton!);

    expect(getByText('Name')).toBeTruthy();
    expect(getByText('Persona')).toBeTruthy();
  });

  test('form includes Description and System Prompt fields', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    const addButton = getButtonByText(/Add role/);
    clickButton(addButton!);

    expect(getByText('Description')).toBeTruthy();
    expect(getByText('System Prompt')).toBeTruthy();
  });

  test('form includes Temperature and Max Tokens fields', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    const addButton = getButtonByText(/Add role/);
    clickButton(addButton!);

    expect(getByText('Temperature')).toBeTruthy();
    expect(getByText('Max Tokens')).toBeTruthy();
  });

  test('form includes Allowed Tools field', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    const addButton = getButtonByText(/Add role/);
    clickButton(addButton!);

    expect(getByText('Allowed Tools')).toBeTruthy();
  });

  test('submitting form without name shows error notification', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    const addButton = getButtonByText(/Add role/);
    clickButton(addButton!);

    const submitBtn = getButtonByText('Create');
    expect(submitBtn).toBeTruthy();
    clickButton(submitBtn!);

    await waitForAsync();

    expect(mockAddNotification).toHaveBeenCalledWith(
      'error',
      'Validation Error',
      'Name is required',
    );
  });

  test('submitting form with name creates a new role', async () => {
    rolesApi.listRoles.mockResolvedValue([]);
    rolesApi.createRole.mockResolvedValue({ name: 'newrole', description: 'My new role' });

    renderComponent();

    await waitForRender();

    const addButton = getButtonByText(/Add role/);
    clickButton(addButton!);

    // Fill the name field (first input)
    const inputs = getAllBySelector('input');
    expect(inputs.length).toBeGreaterThan(0);
    setInputValue(inputs[0] as HTMLInputElement, 'newrole');

    // Submit
    const submitBtn = getButtonByText('Create');
    clickButton(submitBtn!);

    await waitForAsync();

    expect(rolesApi.createRole).toHaveBeenCalled();
    expect(mockAddNotification).toHaveBeenCalledWith(
      'success',
      'Role created',
      '"newrole" has been created.',
    );
  });

  test('cancel button closes the form', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    const addButton = getButtonByText(/Add role/);
    clickButton(addButton!);

    // Form is open
    expect(getBySelector('.crud-inline-form')).toBeTruthy();

    const cancelButton = getButtonByText('Cancel');
    clickButton(cancelButton!);

    // Form should be closed
    expect(getBySelector('.crud-inline-form')).toBeFalsy();
    expect(getButtonByText(/Add role/)).toBeTruthy();
  });
});

// ---------------------------------------------------------------------------
// Tests: Edit Role
// ---------------------------------------------------------------------------

describe('RolesSettingsTab - Edit Role', () => {
  test('clicking edit opens form populated with role data', async () => {
    rolesApi.listRoles.mockResolvedValue([
      { name: 'agent', description: 'Default agent', system_prompt: 'You are helpful' },
    ]);

    renderComponent();

    await waitForRender();

    // Find edit button (button containing Pencil icon)
    const editBtn = getAllBySelector('button').find((b) =>
      b.innerHTML.includes('Pencil'),
    );
    expect(editBtn).toBeTruthy();
    clickButton(editBtn as HTMLButtonElement);

    // Form should be open with "Edit Role" title
    expect(getByText('Edit Role')).toBeTruthy();

    // Name field should be pre-filled
    const inputs = getAllBySelector('input');
    expect((inputs[0] as HTMLInputElement).value).toBe('agent');
  });

  test('name field is disabled when editing', async () => {
    rolesApi.listRoles.mockResolvedValue([{ name: 'agent', description: 'Default agent' }]);

    renderComponent();

    await waitForRender();

    const editBtn = getAllBySelector('button').find((b) =>
      b.innerHTML.includes('Pencil'),
    );
    clickButton(editBtn as HTMLButtonElement);

    // Name input (first input) should be disabled
    const inputs = getAllBySelector('input');
    expect(inputs[0].hasAttribute('disabled')).toBeTruthy();
  });

  test('submitting edit form calls updateRole API', async () => {
    rolesApi.listRoles.mockResolvedValue([{ name: 'agent', description: 'Old description' }]);
    rolesApi.updateRole.mockResolvedValue({ name: 'agent', description: 'Updated description' });

    renderComponent();

    await waitForRender();

    const editBtn = getAllBySelector('button').find((b) =>
      b.innerHTML.includes('Pencil'),
    );
    clickButton(editBtn as HTMLButtonElement);

    // Update the description field (first textarea)
    const textareas = getAllBySelector('textarea');
    expect(textareas.length).toBeGreaterThan(0);
    setInputValue(textareas[0] as HTMLTextAreaElement, 'Updated description');

    // Submit (button says "Update" for edit mode)
    const submitBtn = getButtonByText('Update');
    expect(submitBtn).toBeTruthy();
    clickButton(submitBtn!);

    await waitForAsync();

    expect(rolesApi.updateRole).toHaveBeenCalledWith(
      expect.any(Function),
      'agent',
      expect.objectContaining({ name: 'agent', description: 'Updated description' }),
    );
    expect(mockAddNotification).toHaveBeenCalledWith(
      'success',
      'Role updated',
      '"agent" has been updated.',
    );
  });
});

// ---------------------------------------------------------------------------
// Tests: Delete Role
// ---------------------------------------------------------------------------

describe('RolesSettingsTab - Delete Role', () => {
  test('clicking delete calls deleteRole API', async () => {
    rolesApi.listRoles.mockResolvedValue([{ name: 'agent', description: 'Default agent' }]);
    rolesApi.deleteRole.mockResolvedValue(undefined);

    renderComponent();

    await waitForRender();

    const deleteBtn = getAllBySelector('button').find((b) =>
      b.innerHTML.includes('Trash2'),
    );
    expect(deleteBtn).toBeTruthy();
    clickButton(deleteBtn as HTMLButtonElement);

    await waitForAsync();

    expect(rolesApi.deleteRole).toHaveBeenCalledWith(
      expect.any(Function),
      'agent',
    );
  });

  test('shows success notification after deletion', async () => {
    rolesApi.listRoles.mockResolvedValue([{ name: 'agent', description: 'Default agent' }]);
    rolesApi.deleteRole.mockResolvedValue(undefined);

    renderComponent();

    await waitForRender();

    const deleteBtn = getAllBySelector('button').find((b) =>
      b.innerHTML.includes('Trash2'),
    );
    clickButton(deleteBtn as HTMLButtonElement);

    await waitForAsync();

    expect(mockAddNotification).toHaveBeenCalledWith(
      'success',
      'Role Deleted',
      'Role "agent" has been deleted',
    );
  });

  test('shows error notification when delete fails', async () => {
    rolesApi.listRoles.mockResolvedValue([{ name: 'agent', description: 'Default agent' }]);
    rolesApi.deleteRole.mockRejectedValue(new Error('Failed to delete'));

    renderComponent();

    await waitForRender();

    const deleteBtn = getAllBySelector('button').find((b) =>
      b.innerHTML.includes('Trash2'),
    );
    clickButton(deleteBtn as HTMLButtonElement);

    await waitForAsync();

    expect(mockAddNotification).toHaveBeenCalledWith(
      'error',
      'Delete failed',
      'Failed to delete',
    );
  });

  test('confirm dialog is used for delete', async () => {
    rolesApi.listRoles.mockResolvedValue([{ name: 'agent', description: 'Default agent' }]);
    rolesApi.deleteRole.mockResolvedValue(undefined);

    renderComponent();

    await waitForRender();

    const deleteBtn = getAllBySelector('button').find((b) =>
      b.innerHTML.includes('Trash2'),
    );
    clickButton(deleteBtn as HTMLButtonElement);

    await waitForAsync();

    // @ts-expect-error — confirm is mocked
    expect(global.confirm).toHaveBeenCalledWith('Delete role "agent"? This cannot be undone.');
  });

  test('canceling confirm prevents deletion', async () => {
    // @ts-expect-error — confirm is mocked
    global.confirm.mockReturnValue(false);
    rolesApi.listRoles.mockResolvedValue([{ name: 'agent', description: 'Default agent' }]);
    rolesApi.deleteRole.mockResolvedValue(undefined);

    renderComponent();

    await waitForRender();

    const deleteBtn = getAllBySelector('button').find((b) =>
      b.innerHTML.includes('Trash2'),
    );
    clickButton(deleteBtn as HTMLButtonElement);

    await waitForAsync();

    // deleteRole should NOT have been called
    expect(rolesApi.deleteRole).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Tests: Error Handling
// ---------------------------------------------------------------------------

describe('RolesSettingsTab - Error Handling', () => {
  test('shows error notification when listRoles fails', async () => {
    rolesApi.listRoles.mockRejectedValue(new Error('Network error'));

    renderComponent();

    await waitForAsync();

    expect(mockAddNotification).toHaveBeenCalledWith(
      'error',
      'Load roles failed',
      'Network error',
    );
  });

  test('shows error notification when createRole fails', async () => {
    rolesApi.listRoles.mockResolvedValue([]);
    rolesApi.createRole.mockRejectedValue(new Error('Failed to create'));

    renderComponent();

    await waitForRender();

    const addButton = getButtonByText(/Add role/);
    clickButton(addButton!);

    // Fill name
    const inputs = getAllBySelector('input');
    setInputValue(inputs[0] as HTMLInputElement, 'newrole');

    const submitBtn = getButtonByText('Create');
    clickButton(submitBtn!);

    await waitForAsync();

    expect(mockAddNotification).toHaveBeenCalledWith(
      'error',
      'Create failed',
      'Failed to create',
    );
  });

  test('shows error notification when updateRole fails', async () => {
    rolesApi.listRoles.mockResolvedValue([{ name: 'agent', description: 'Old' }]);
    rolesApi.updateRole.mockRejectedValue(new Error('Server error'));

    renderComponent();

    await waitForRender();

    const editBtn = getAllBySelector('button').find((b) =>
      b.innerHTML.includes('Pencil'),
    );
    clickButton(editBtn as HTMLButtonElement);

    const submitBtn = getButtonByText('Update');
    clickButton(submitBtn!);

    await waitForAsync();

    expect(mockAddNotification).toHaveBeenCalledWith(
      'error',
      'Update failed',
      'Server error',
    );
  });
});

// ---------------------------------------------------------------------------
// Tests: Special Characters
// ---------------------------------------------------------------------------

describe('RolesSettingsTab - Special Characters', () => {
  test('renders roles with special characters in name', async () => {
    rolesApi.listRoles.mockResolvedValue([{ name: 'my/role', description: 'Has slash' }]);

    renderComponent();

    await waitForRender();

    expect(getByText('my/role')).toBeTruthy();
  });
});

// ---------------------------------------------------------------------------
// Tests: Form Visibility
// ---------------------------------------------------------------------------

describe('RolesSettingsTab - Form Visibility', () => {
  test('Add role button is hidden when form is open', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    // Button visible initially
    expect(getButtonByText(/Add role/)).toBeTruthy();

    const addButton = getButtonByText(/Add role/);
    clickButton(addButton!);

    // Button should be hidden when form is open
    expect(getButtonByText(/Add role/)).toBeFalsy();
  });

  test('form is hidden initially before clicking Add role', async () => {
    rolesApi.listRoles.mockResolvedValue([]);

    renderComponent();

    await waitForRender();

    expect(getBySelector('.crud-inline-form')).toBeFalsy();
  });
});
