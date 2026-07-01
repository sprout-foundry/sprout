/**
 * SettingsPanel.test.tsx — Unit tests for the collapsible section panel.
 *
 * Covers:
 * - All 5 SECTION_GROUPS render with correct labels
 * - Scope badges render per section
 * - Collapse/expand toggle via section header click
 * - Section body visibility tied to expanded state
 * - localStorage persistence (write + restore)
 * - activeSubsection persistence
 * - aria-expanded accessibility attribute
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach, beforeAll, afterAll } from 'vitest';
import SettingsPanel from './SettingsPanel';

// ---------------------------------------------------------------------------
// Mocks — must come before imports that transitively depend on them
// ---------------------------------------------------------------------------

// Mock @sprout/ui (Skeleton, NotificationStack, notificationBus, useNotifications)
vi.mock('@sprout/ui', async () => {
  const actual = await vi.importActual<any>('@sprout/ui');
  return {
    ...actual,
    Skeleton: ({ width, height }: { width?: string | number; height?: string | number }) =>
      createElement('div', {
        'data-testid': 'skeleton',
        style: { width, height },
      }),
    NotificationStack: () => createElement('div', { 'data-testid': 'mock-notification-stack' }),
    useNotifications: () => ({
      addNotification: vi.fn(),
      notifications: [],
      removeNotification: vi.fn(),
    }),
  };
});

// Mock the API service so hooks don't fire real HTTP requests
vi.mock('../../services/api', async () => {
  const actual = await vi.importActual<any>('../../services/api');
  return {
    ...actual,
    ApiService: {
      getInstance: () => ({
        getSettings: vi.fn().mockResolvedValue({}),
        updateSettings: vi.fn().mockResolvedValue(undefined),
        getSettingsLayer: vi.fn().mockResolvedValue({}),
        getSettingsProvenance: vi.fn().mockResolvedValue({ sources: {} }),
        getProviders: vi.fn().mockResolvedValue({ providers: [], current_provider: '', current_model: '' }),
        getOnboardingStatus: vi.fn().mockResolvedValue({ providers: [], current_provider: '', current_model: '' }),
        getSubagentTypes: vi.fn().mockResolvedValue({ subagent_types: {} }),
        updateSkills: vi.fn().mockResolvedValue(undefined),
      }),
    },
  };
});

// Mock the provider catalog context (used by useSettingsState)
vi.mock('../../contexts/ProviderCatalogContext', () => ({
  useProviderCatalog: () => ({
    providers: [],
    isLoading: false,
    currentProvider: '',
    currentModel: '',
    refresh: vi.fn(),
    getProviderName: (id: string | undefined | null) => id ?? '',
  }),
}));

// Mock all settings tabs so we don't render their full logic.
// Each factory is inlined to avoid hoisting issues.
vi.mock('./settings/AgentBehaviorSettingsTab', () => ({ default: function TabMock({ settings }) { return createElement('div', { 'data-testid': 'settings-tab-mock', 'data-has-settings': !!settings }); } }));
vi.mock('./settings/SubagentSettingsTab', () => ({ default: function TabMock({ settings }) { return createElement('div', { 'data-testid': 'settings-tab-mock', 'data-has-settings': !!settings }); } }));
vi.mock('./settings/SkillsSettingsTab', () => ({ default: function TabMock({ settings }) { return createElement('div', { 'data-testid': 'settings-tab-mock', 'data-has-settings': !!settings }); } }));
vi.mock('./settings/PersistentContextSettingsTab', () => ({ default: function TabMock({ settings }) { return createElement('div', { 'data-testid': 'settings-tab-mock', 'data-has-settings': !!settings }); } }));
vi.mock('./settings/EmbeddingSettingsTab', () => ({ default: function TabMock({ settings }) { return createElement('div', { 'data-testid': 'settings-tab-mock', 'data-has-settings': !!settings }); } }));
vi.mock('./settings/LanguageServersSettingsTab', () => ({ default: function TabMock({ settings }) { return createElement('div', { 'data-testid': 'settings-tab-mock', 'data-has-settings': !!settings }); } }));
vi.mock('./settings/MCPSettingsTab', () => ({ default: function TabMock({ settings }) { return createElement('div', { 'data-testid': 'settings-tab-mock', 'data-has-settings': !!settings }); } }));
vi.mock('./settings/ProviderSettingsTab', () => ({ default: function TabMock({ settings }) { return createElement('div', { 'data-testid': 'settings-tab-mock', 'data-has-settings': !!settings }); } }));
vi.mock('./settings/AdvancedSettingsTab', () => ({ default: function TabMock({ settings }) { return createElement('div', { 'data-testid': 'settings-tab-mock', 'data-has-settings': !!settings }); } }));
vi.mock('./settings/GeneralSettingsTab', () => ({ default: function TabMock({ settings }) { return createElement('div', { 'data-testid': 'settings-tab-mock', 'data-has-settings': !!settings }); } }));
vi.mock('./settings/NotificationsSettingsTab', () => ({ default: function TabMock({ settings }) { return createElement('div', { 'data-testid': 'settings-tab-mock', 'data-has-settings': !!settings }); } }));
vi.mock('./settings/ComputerUseSettingsTab', () => ({ default: function TabMock({ settings }) { return createElement('div', { 'data-testid': 'settings-tab-mock', 'data-has-settings': !!settings }); } }));
vi.mock('./CredentialsSettingsTab', () => ({ default: function TabMock({ settings }) { return createElement('div', { 'data-testid': 'settings-tab-mock', 'data-has-settings': !!settings }); } }));

// Mock desktopNotify (used by NotificationsSettingsTab)
vi.mock('../../services/desktopNotify', () => ({
  getPermission: () => 'default' as const,
  requestPermission: vi.fn().mockResolvedValue('granted' as const),
  setEnabled: vi.fn(),
  isEnabled_: () => true,
  notify: vi.fn(),
  notifyIfHidden: vi.fn(),
}));

// Mock log utility
vi.mock('../../utils/log', () => ({
  debugLog: vi.fn(),
}));

// Mock config/mode
vi.mock('../../config/mode', () => ({
  supportsSettings: true,
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const STORAGE_KEY = 'sprout.settingsPanel.state.v1';

let container: HTMLDivElement;
let root: Root;

// Capture original console methods so test stderr stays clean. The
// SettingsPanel renders inside the real useSettingsState hook which can
// fall through to console.error when its api call fails; we don't want
// that noise polluting every test's output.
let originalLog: typeof console.log;
let originalWarn: typeof console.warn;
let originalError: typeof console.error;

beforeAll(() => {
  // @ts-expect-error
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  originalLog = console.log;
  originalWarn = console.warn;
  originalError = console.error;
  console.log = vi.fn();
  console.warn = vi.fn();
  console.error = vi.fn();
});

afterAll(() => {
  console.log = originalLog;
  console.warn = originalWarn;
  console.error = originalError;
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  localStorage.clear();
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

function renderPanel(extraProps?: Record<string, unknown>) {
  const baseProps = {
    settings: null,
    onSettingsChanged: vi.fn(),
    onRequestProviderSetup: vi.fn(),
    editorPreferences: null,
    onEditorPreferenceChanged: vi.fn(),
    agentConfig: null,
    ...extraProps,
  };
  act(() => {
    root.render(createElement(SettingsPanel, baseProps));
  });
}

/** Find section header by looking at the label child */
function findSectionHeaderByLabel(label: string): HTMLButtonElement | null {
  const labels = container.querySelectorAll('.settings-section-label');
  for (const lbl of labels) {
    if (lbl.textContent?.trim() === label) {
      return lbl.closest('.settings-section-header') as HTMLButtonElement | null;
    }
  }
  return null;
}

// ---------------------------------------------------------------------------
// Tests: Section groups render
// ---------------------------------------------------------------------------

describe('SECTION_GROUPS rendering', () => {
  it('renders all 5 section labels (Agent, Workspace, Environment, Editor, Experimental)', () => {
    renderPanel();

    const labels = Array.from(container.querySelectorAll('.settings-section-label')).map(
      (el) => el.textContent?.trim(),
    );

    expect(labels).toContain('Agent');
    expect(labels).toContain('Workspace');
    expect(labels).toContain('Environment');
    expect(labels).toContain('Editor');
    expect(labels).toContain('Experimental');
  });

  it('renders the correct scope badge for each section', () => {
    renderPanel();

    const sections = container.querySelectorAll('.settings-section');
    expect(sections.length).toBe(5);

    // Agent → session
    const agentBadge = sections[0].querySelector('.settings-scope-badge');
    expect(agentBadge?.textContent?.trim()).toBe('session');
    expect(agentBadge?.classList.contains('scope-session')).toBe(true);

    // Workspace → workspace
    const workspaceBadge = sections[1].querySelector('.settings-scope-badge');
    expect(workspaceBadge?.textContent?.trim()).toBe('workspace');
    expect(workspaceBadge?.classList.contains('scope-workspace')).toBe(true);

    // Environment → global
    const envBadge = sections[2].querySelector('.settings-scope-badge');
    expect(envBadge?.textContent?.trim()).toBe('global');
    expect(envBadge?.classList.contains('scope-global')).toBe(true);

    // Editor → runtime
    const editorBadge = sections[3].querySelector('.settings-scope-badge');
    expect(editorBadge?.textContent?.trim()).toBe('runtime');
    expect(editorBadge?.classList.contains('scope-runtime')).toBe(true);

    // Experimental → global
    const expBadge = sections[4].querySelector('.settings-scope-badge');
    expect(expBadge?.textContent?.trim()).toBe('global');
    expect(expBadge?.classList.contains('scope-global')).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Tests: Section collapse / expand
// ---------------------------------------------------------------------------

describe('section collapse and expand', () => {
  it('collapses an expanded section when its header is clicked', () => {
    renderPanel();

    // Agent section is expanded by default
    const agentSection = container.querySelector('.settings-section');
    expect(agentSection?.classList.contains('expanded')).toBe(true);
    expect(container.querySelector('.settings-section-body')).not.toBeNull();

    // Click the Agent header to collapse
    const agentHeader = findSectionHeaderByLabel('Agent');
    act(() => {
      agentHeader?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Agent section should now be collapsed
    expect(agentSection?.classList.contains('expanded')).toBe(false);
    // The body inside the agent section should be gone
    const agentBody = agentSection?.querySelector('.settings-section-body');
    expect(agentBody).toBeNull();
  });

  it('expands a collapsed section when its header is clicked', () => {
    renderPanel();

    // Workspace section is collapsed by default
    const workspaceSection = container.querySelectorAll('.settings-section')[1];
    expect(workspaceSection.classList.contains('expanded')).toBe(false);

    // Click the Workspace header to expand
    const workspaceHeader = findSectionHeaderByLabel('Workspace');
    act(() => {
      workspaceHeader?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(workspaceSection.classList.contains('expanded')).toBe(true);
  });

  it('hides section body when collapsed', () => {
    renderPanel();

    // Workspace is collapsed by default
    const workspaceSection = container.querySelectorAll('.settings-section')[1];
    const body = workspaceSection.querySelector('.settings-section-body');
    expect(body).toBeNull();
  });

  it('shows section body when expanded', () => {
    renderPanel();

    // Agent is expanded by default
    const agentSection = container.querySelectorAll('.settings-section')[0];
    const body = agentSection.querySelector('.settings-section-body');
    expect(body).not.toBeNull();

    // The body should contain the section description
    expect(body?.querySelector('.settings-section-desc')).not.toBeNull();

    // The body should contain the subsection tab list
    expect(body?.querySelector('.settings-subsection-list')).not.toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Tests: localStorage persistence
// ---------------------------------------------------------------------------

describe('localStorage persistence', () => {
  it('writes expanded sections to localStorage on state change', () => {
    renderPanel();

    // Agent is expanded by default; the effect should write
    const stored = localStorage.getItem(STORAGE_KEY);
    expect(stored).not.toBeNull();

    const parsed = JSON.parse(stored!);
    expect(parsed.expanded).toContain('agent');

    // Click to expand Workspace
    const workspaceHeader = findSectionHeaderByLabel('Workspace');
    act(() => {
      workspaceHeader?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const updated = JSON.parse(localStorage.getItem(STORAGE_KEY)!);
    expect(updated.expanded).toContain('workspace');
  });

  it('restores expanded sections from localStorage on mount', () => {
    // Pre-populate localStorage with workspace expanded
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({
        expanded: ['workspace'],
        activeSubsection: 'workspace-embeddings',
      }),
    );

    renderPanel();

    // Workspace section should be expanded
    const workspaceSection = container.querySelectorAll('.settings-section')[1];
    expect(workspaceSection.classList.contains('expanded')).toBe(true);

    // Agent should NOT be expanded (overridden by localStorage)
    const agentSection = container.querySelectorAll('.settings-section')[0];
    expect(agentSection.classList.contains('expanded')).toBe(false);
  });

  it('writes activeSubsection to localStorage when a subsection tab is clicked', () => {
    renderPanel();

    // Click the "Embeddings" subsection tab inside the Workspace section
    // First expand Workspace
    const workspaceHeader = findSectionHeaderByLabel('Workspace');
    act(() => {
      workspaceHeader?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // Now click the Embeddings tab
    const embeddingsTab = container.querySelector('[data-testid="settings-workspace-embeddings-tab"]');
    act(() => {
      embeddingsTab?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const stored = JSON.parse(localStorage.getItem(STORAGE_KEY)!);
    expect(stored.activeSubsection).toBe('workspace-embeddings');
  });
});

// ---------------------------------------------------------------------------
// Tests: aria-expanded accessibility
// ---------------------------------------------------------------------------

describe('aria-expanded accessibility', () => {
  it('aria-expanded reflects section expanded state', () => {
    renderPanel();

    // Agent is expanded by default
    const agentHeader = findSectionHeaderByLabel('Agent');
    expect(agentHeader?.getAttribute('aria-expanded')).toBe('true');

    // Workspace is collapsed by default
    const workspaceHeader = findSectionHeaderByLabel('Workspace');
    expect(workspaceHeader?.getAttribute('aria-expanded')).toBe('false');

    // Click Workspace to expand it
    act(() => {
      workspaceHeader?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(workspaceHeader?.getAttribute('aria-expanded')).toBe('true');

    // Click Agent to collapse it
    act(() => {
      agentHeader?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(agentHeader?.getAttribute('aria-expanded')).toBe('false');
  });
});

// ---------------------------------------------------------------------------
// Tests: Subsection tabs render
// ---------------------------------------------------------------------------

describe('subsection tabs', () => {
  it('renders subsection tabs inside an expanded section', () => {
    renderPanel();

    // Agent is expanded by default; find its subsection list
    const agentSection = container.querySelectorAll('.settings-section')[0];
    const tabs = agentSection.querySelectorAll('.settings-subsection-btn');

    // Agent has 5 subsections: General, Security, Subagents, Skills, Memory
    expect(tabs.length).toBe(5);

    const tabLabels = Array.from(tabs).map((t) => t.textContent?.trim());
    expect(tabLabels).toContain('General');
    expect(tabLabels).toContain('Security');
    expect(tabLabels).toContain('Subagents');
    expect(tabLabels).toContain('Skills');
    expect(tabLabels).toContain('Memory');
  });
});

// ---------------------------------------------------------------------------
// Tests: Filter bar
// ---------------------------------------------------------------------------

describe('filter bar', () => {
  it('renders the filter input', () => {
    renderPanel();
    const filterInput = container.querySelector('.settings-filter-input');
    expect(filterInput).not.toBeNull();
    expect(filterInput?.getAttribute('placeholder')).toContain('Filter settings');
  });
});
