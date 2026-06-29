/**
 * SkillsSettingsTab.test.tsx — Unit tests for the skill install/manage UI tab.
 *
 * Covers:
 * - Empty state when no skills installed.
 * - Loading state.
 * - Install form is rendered with all data-testid hooks.
 * - Clicking a registry option fills the source input.
 * - Submitting the form calls installSkill and refreshes the list.
 * - Installed skill rows have working Update + Remove buttons.
 * - Force checkbox toggles the payload.
 * - API errors surface as a visible error message.
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';

// ---------------------------------------------------------------------------
// Mocks — must come before the import of the component
// ---------------------------------------------------------------------------

const mockListInstalledSkills = vi.fn().mockResolvedValue([]);
const mockListSkillRegistry = vi.fn().mockResolvedValue([]);
const mockInstallSkill = vi.fn().mockResolvedValue([]);
const mockUpdateSkill = vi.fn().mockResolvedValue([]);
const mockRemoveSkill = vi.fn().mockResolvedValue({ status: 'removed', id: 'demo' });

vi.mock('../../services/api', () => {
  return {
    ApiService: {
      getInstance: () => ({
        listInstalledSkills: () => mockListInstalledSkills(),
        listSkillRegistry: () => mockListSkillRegistry(),
        installSkill: (source: string, opts?: { ref?: string; force?: boolean }) =>
          mockInstallSkill(source, opts),
        updateSkill: (id: string) => mockUpdateSkill(id),
        removeSkill: (id: string) => mockRemoveSkill(id),
      }),
    },
  };
});

import SkillsSettingsTab from './SkillsSettingsTab';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function resetMocks() {
  vi.clearAllMocks();
  mockListInstalledSkills.mockResolvedValue([]);
  mockListSkillRegistry.mockResolvedValue([]);
  mockInstallSkill.mockResolvedValue([]);
  mockUpdateSkill.mockResolvedValue([]);
  mockRemoveSkill.mockResolvedValue({ status: 'removed', id: 'demo' });
}

const settings = { skills: {} } as any;
const toggleSkill = vi.fn().mockResolvedValue(undefined);

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  resetMocks();
  // Stub window.confirm to auto-accept
  vi.spyOn(window, 'confirm').mockReturnValue(true);
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  vi.restoreAllMocks();
});

function renderTab() {
  act(() => {
    root.render(createElement(SkillsSettingsTab, { settings, toggleSkill }));
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('render', () => {
  it('shows loading state initially', () => {
    mockListInstalledSkills.mockReturnValue(new Promise(() => {}));
    mockListSkillRegistry.mockReturnValue(new Promise(() => {}));
    renderTab();
    expect(screen.getByText(/loading/i)).toBeInTheDocument();
  });

  it('shows empty state when no skills installed', async () => {
    mockListInstalledSkills.mockResolvedValue([]);
    mockListSkillRegistry.mockResolvedValue([]);
    renderTab();
    await waitFor(() => {
      expect(screen.getByText(/no skills installed/i)).toBeInTheDocument();
    });
  });

  it('renders all install form inputs', async () => {
    mockListInstalledSkills.mockResolvedValue([]);
    mockListSkillRegistry.mockResolvedValue([
      { id: 'security-review', name: 'Security Review', description: 'desc', git_url: 'x', git_ref: 'main', path_in_repo: 'a' },
    ]);
    renderTab();
    await waitFor(() => {
      expect(screen.getByTestId('skills-install-source')).toBeInTheDocument();
    });
    expect(screen.getByTestId('skills-install-ref')).toBeInTheDocument();
    expect(screen.getByTestId('skills-install-force')).toBeInTheDocument();
    expect(screen.getByTestId('skills-install-button')).toBeInTheDocument();
    expect(screen.getByTestId('skills-registry-dropdown')).toBeInTheDocument();
  });
});

describe('registry dropdown', () => {
  it('selecting a registry option fills the source input', async () => {
    const user = userEvent.setup();
    mockListInstalledSkills.mockResolvedValue([]);
    mockListSkillRegistry.mockResolvedValue([
      { id: 'security-review', name: 'Security Review', description: 'desc', git_url: 'x', git_ref: 'main', path_in_repo: 'a' },
    ]);
    renderTab();
    const select = await waitFor(() => screen.getByTestId('skills-registry-dropdown'));
    await user.selectOptions(select, 'security-review');
    const sourceInput = screen.getByTestId('skills-install-source') as HTMLInputElement;
    expect(sourceInput.value).toBe('security-review');
  });
});

describe('install form', () => {
  it('submitting the form calls installSkill and refreshes list', async () => {
    const user = userEvent.setup();
    mockListInstalledSkills
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([{ id: 'demo', origin: { type: 'path' }, installed_at: '2025-01-01T00:00:00Z' }]);
    mockListSkillRegistry.mockResolvedValue([]);
    mockInstallSkill.mockResolvedValue([{ skill_id: 'demo', install_dir: '/x/demo', origin: { type: 'path' } }]);

    renderTab();
    const sourceInput = await waitFor(() => screen.getByTestId('skills-install-source'));
    await user.type(sourceInput, './my-skill');
    const installBtn = screen.getByTestId('skills-install-button');
    await user.click(installBtn);

    await waitFor(() => {
      expect(mockInstallSkill).toHaveBeenCalledWith('./my-skill', { ref: undefined, force: false });
    });
    await waitFor(() => {
      expect(mockListInstalledSkills).toHaveBeenCalledTimes(2);
    });
  });

  it('force checkbox toggles the payload flag', async () => {
    const user = userEvent.setup();
    mockListInstalledSkills.mockResolvedValue([]);
    mockListSkillRegistry.mockResolvedValue([]);
    mockInstallSkill.mockResolvedValue([{ skill_id: 'x', install_dir: '/x', origin: { type: 'path' } }]);

    renderTab();
    const sourceInput = await waitFor(() => screen.getByTestId('skills-install-source'));
    await user.type(sourceInput, '/some/path');
    const force = screen.getByTestId('skills-install-force') as HTMLInputElement;
    await user.click(force);
    const btn = screen.getByTestId('skills-install-button');
    await user.click(btn);

    await waitFor(() => {
      expect(mockInstallSkill).toHaveBeenCalledWith('/some/path', { ref: undefined, force: true });
    });
  });
});

describe('installed skills list', () => {
  it('renders one row per installed skill with Update + Remove buttons', async () => {
    mockListInstalledSkills.mockResolvedValue([
      { id: 'alpha', origin: { type: 'path', path: '/x' }, installed_at: '2025-01-01T00:00:00Z' },
      { id: 'beta', origin: { type: 'git', url: 'git@example.com:x' }, installed_at: '2025-01-01T00:00:00Z' },
    ]);
    mockListSkillRegistry.mockResolvedValue([]);
    renderTab();
    await waitFor(() => {
      expect(screen.getByTestId('skills-list-item-alpha')).toBeInTheDocument();
    });
    expect(screen.getByTestId('skills-list-item-beta')).toBeInTheDocument();
    expect(screen.getByTestId('skills-update-button-alpha')).toBeInTheDocument();
    expect(screen.getByTestId('skills-remove-button-alpha')).toBeInTheDocument();
    expect(screen.getByTestId('skills-update-button-beta')).toBeInTheDocument();
    expect(screen.getByTestId('skills-remove-button-beta')).toBeInTheDocument();
  });

  it('Update button calls api.updateSkill', async () => {
    const user = userEvent.setup();
    mockListInstalledSkills.mockResolvedValue([
      { id: 'alpha', origin: { type: 'git' }, installed_at: '2025-01-01T00:00:00Z' },
    ]);
    mockListSkillRegistry.mockResolvedValue([]);
    mockUpdateSkill.mockResolvedValue([{ skill_id: 'alpha', install_dir: '/x', origin: { type: 'git' } }]);

    renderTab();
    const updateBtn = await waitFor(() => screen.getByTestId('skills-update-button-alpha'));
    await user.click(updateBtn);

    await waitFor(() => {
      expect(mockUpdateSkill).toHaveBeenCalledWith('alpha');
    });
  });

  it('Remove button calls api.removeSkill', async () => {
    const user = userEvent.setup();
    mockListInstalledSkills.mockResolvedValue([
      { id: 'alpha', origin: { type: 'git' }, installed_at: '2025-01-01T00:00:00Z' },
    ]);
    mockListSkillRegistry.mockResolvedValue([]);

    renderTab();
    const removeBtn = await waitFor(() => screen.getByTestId('skills-remove-button-alpha'));
    await user.click(removeBtn);

    await waitFor(() => {
      expect(mockRemoveSkill).toHaveBeenCalledWith('alpha');
    });
  });
});

describe('errors', () => {
  it('shows an error message when the API call fails', async () => {
    mockListInstalledSkills.mockRejectedValue(new Error('boom'));
    mockListSkillRegistry.mockResolvedValue([]);
    renderTab();
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/boom/);
    });
  });
});

describe('toggle enabled (preserved original UI)', () => {
  it('renders the toggle section when settings has skills', async () => {
    const settingsWithSkills = {
      skills: {
        'demo': { id: 'demo', name: 'Demo', description: '', path: '', enabled: true },
      },
    } as any;
    act(() => {
      root.render(createElement(SkillsSettingsTab, { settings: settingsWithSkills, toggleSkill }));
    });
    await waitFor(() => {
      expect(screen.getByText('Toggle enabled (1/1 enabled)')).toBeInTheDocument();
    });
    // Find checkbox inside the skill-item that contains the "demo" name
    const skillItem = screen.getByText('demo').closest('.skill-item');
    expect(skillItem).not.toBeNull();
    const checkbox = skillItem!.querySelector('input[type="checkbox"]') as HTMLInputElement;
    expect(checkbox).toBeChecked();
  });

  it('clicking the toggle calls toggleSkill', async () => {
    const user = userEvent.setup();
    const settingsWithSkills = {
      skills: {
        'demo': { id: 'demo', name: 'Demo', description: '', path: '', enabled: true },
      },
    } as any;
    act(() => {
      root.render(createElement(SkillsSettingsTab, { settings: settingsWithSkills, toggleSkill }));
    });
    // Wait for the toggle section to render, then find the checkbox
    await waitFor(() => {
      expect(screen.getByText('Toggle enabled (1/1 enabled)')).toBeInTheDocument();
    });
    const skillItem = screen.getByText('demo').closest('.skill-item');
    const checkbox = skillItem!.querySelector('input[type="checkbox"]') as HTMLInputElement;
    await user.click(checkbox);
    await waitFor(() => {
      expect(toggleSkill).toHaveBeenCalledWith('demo', false);
    });
  });
});
