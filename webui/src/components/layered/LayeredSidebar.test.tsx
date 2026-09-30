import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../../config/mode', () => ({
  isCloud: false,
  supportsGit: true,
  supportsSettings: true,
  supportsAutomations: true,
}));
vi.mock('../../services/activeRepo', () => ({ useActiveRepoURL: () => undefined }));
vi.mock('../../services/recentRepos', () => ({ useRecentRepos: () => [] }));
vi.mock('../UserMenu', () => ({ UserMenu: () => null }));
vi.mock('../ThemedDialog', () => ({
  showThemedPrompt: vi.fn(),
  showThemedAlert: vi.fn(),
  showThemedConfirm: vi.fn(),
}));

import LayeredSidebar, { type LayeredSidebarProps } from './LayeredSidebar';
import { showThemedConfirm, showThemedPrompt } from '../ThemedDialog';

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

function renderSidebar(overrides: Partial<LayeredSidebarProps> = {}) {
  const props: LayeredSidebarProps = {
    conversations: {
      sessions: [
        { id: 'c1', name: 'Add checkout', is_active: true, is_default: true, active_query: false } as never,
        { id: 'c2', name: 'Fix CI', is_active: false, is_default: false, active_query: true } as never,
      ],
      activeId: 'c1',
      inMain: true,
      onSelect: vi.fn(),
      onOpen: vi.fn(),
      onCreate: vi.fn(),
    },
    currentView: 'chat',
    onViewChange: vi.fn(),
    modes: [{ id: 'code' }, { id: 'design' }] as never,
    activeModeId: 'code',
    onSelectMode: vi.fn(),
    onModeSectionChange: vi.fn(),
    onSectionChange: vi.fn(),
    workspaceRoot: '/home/ada/my-app',
    instances: [],
    renderSection: () => <div data-testid="section-panel">panel</div>,
    ...overrides,
  };
  act(() => root.render(<LayeredSidebar {...props} />));
  return props;
}

const click = (el: Element | null | undefined) =>
  act(() => {
    (el as HTMLElement).click();
  });
const itemByText = (text: string) =>
  Array.from(container.querySelectorAll('.project-nav-item')).find((b) => b.textContent?.includes(text));

describe('LayeredSidebar', () => {
  it('lists conversations and the project tools', () => {
    renderSidebar();
    expect(container.querySelector('.project-nav-title')?.textContent).toBe('my-app');
    for (const label of ['Add checkout', 'Fix CI', 'Files', 'Source control', 'Screens', 'Workflows', 'Settings']) {
      expect(itemByText(label)).toBeTruthy();
    }
    expect(itemByText('Fix CI')?.querySelector('.project-nav-running')).toBeTruthy();
  });

  it('trusts live working state over a stale list flag', () => {
    const props = renderSidebar();
    renderSidebar({ conversations: { ...props.conversations!, isWorking: (id) => id === 'c1' } });
    expect(itemByText('Fix CI')?.querySelector('.project-nav-running')).toBeNull();
    expect(itemByText('Add checkout')?.querySelector('.project-nav-running')).toBeTruthy();
  });

  it('opens a conversation in the main view', () => {
    const props = renderSidebar();
    click(itemByText('Fix CI'));
    expect(props.conversations!.onSelect).toHaveBeenCalledWith('c2');
    expect(props.conversations!.onOpen).not.toHaveBeenCalled();
  });

  it('highlights the conversation only while it is the main view', () => {
    const base = renderSidebar();
    expect(itemByText('Add checkout')?.classList.contains('active')).toBe(true);
    renderSidebar({ conversations: { ...base.conversations!, inMain: false } });
    expect(itemByText('Add checkout')?.classList.contains('active')).toBe(false);
  });

  it('drills into a tool panel and back', () => {
    const props = renderSidebar();
    click(itemByText('Files'));
    expect(props.onSectionChange).toHaveBeenCalledWith('files');
    expect(container.querySelector('[data-testid="section-panel"]')).toBeTruthy();
    expect(container.querySelector('.project-nav-drill-title')?.textContent).toBe('Files');
    click(container.querySelector('[aria-label="Back to project"]'));
    expect(container.querySelector('[data-testid="section-panel"]')).toBeNull();
  });

  it('switches to design mode for design entries', () => {
    const props = renderSidebar();
    click(itemByText('Tokens'));
    expect(props.onSelectMode).toHaveBeenCalledWith('design');
    expect(props.onModeSectionChange).toHaveBeenCalledWith('tokens');
  });

  it('closes the phone drawer when a project is picked on the rail', () => {
    const onCloseDrawer = vi.fn();
    const onInstanceChange = vi.fn();
    renderSidebar({
      isMobile: true,
      onCloseDrawer,
      onInstanceChange,
      instances: [
        { pid: 1, working_dir: '/home/ada/my-app', is_current: true },
        { pid: 2, working_dir: '/home/ada/api', is_current: false },
      ] as never,
    });
    click(container.querySelector('.project-rail-project[aria-label="api"]'));
    expect(onInstanceChange).toHaveBeenCalledWith(2);
    expect(onCloseDrawer).toHaveBeenCalled();
  });

  it('closes the phone drawer when a new conversation is created', () => {
    const onCloseDrawer = vi.fn();
    const base = renderSidebar();
    const props = renderSidebar({ isMobile: true, onCloseDrawer, conversations: base.conversations });
    click(container.querySelector('.project-nav-new'));
    expect(props.conversations!.onCreate).toHaveBeenCalled();
    expect(onCloseDrawer).toHaveBeenCalled();
  });

  it('renames and deletes a conversation from its menu', async () => {
    const onRename = vi.fn();
    const onDelete = vi.fn();
    const base = renderSidebar();
    renderSidebar({ conversations: { ...base.conversations!, onRename, onDelete, canDelete: (id) => id !== 'c1' } });

    const menuItems = () => Array.from(document.querySelectorAll('.context-menu-item')).map((b) => b.textContent);
    act(() => {
      itemByText('Add checkout')!.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }));
    });
    expect(menuItems()).toEqual(['Rename']);

    vi.mocked(showThemedPrompt).mockResolvedValueOnce('  Checkout flow ');
    await act(async () => {
      (Array.from(document.querySelectorAll('.context-menu-item'))[0] as HTMLElement).click();
    });
    expect(onRename).toHaveBeenCalledWith('c1', 'Checkout flow');

    act(() => {
      itemByText('Fix CI')!.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }));
    });
    expect(menuItems()).toEqual(['Rename', 'Delete conversation']);
    vi.mocked(showThemedConfirm).mockResolvedValueOnce(true);
    await act(async () => {
      (Array.from(document.querySelectorAll('.context-menu-item'))[1] as HTMLElement).click();
    });
    expect(onDelete).toHaveBeenCalledWith('c2');
  });

  it('offers Clear instead of Delete for a chat that is kept', async () => {
    const onClear = vi.fn();
    const onDelete = vi.fn();
    const base = renderSidebar();
    renderSidebar({ conversations: { ...base.conversations!, onDelete, onClear, canDelete: (id) => id !== 'c1' } });

    const menuItems = () => Array.from(document.querySelectorAll('.context-menu-item')).map((b) => b.textContent);
    act(() => {
      itemByText('Add checkout')!.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }));
    });
    expect(menuItems()).toEqual(['Clear conversation']);

    vi.mocked(showThemedConfirm).mockResolvedValueOnce(true);
    await act(async () => {
      (document.querySelector('.context-menu-item') as HTMLElement).click();
    });
    expect(onClear).toHaveBeenCalledWith('c1');
    expect(onDelete).not.toHaveBeenCalled();

    act(() => {
      itemByText('Fix CI')!.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }));
    });
    expect(menuItems()).toEqual(['Delete conversation']);
  });

  it('shows a project switcher in place of the title when given one', () => {
    renderSidebar({ projectSwitcher: <button data-testid="switcher">my-app ▾</button> });
    expect(container.querySelector('[data-testid="switcher"]')).toBeTruthy();
    expect(container.querySelector('.project-nav-title')).toBeNull();
  });

  it('shows only the rail when collapsed', () => {
    const onToggleCollapsed = vi.fn();
    renderSidebar({ collapsed: true, onToggleCollapsed });
    expect(container.querySelector('[data-testid="project-nav"]')).toBeNull();
    click(container.querySelector('[aria-label="Show project sidebar"]'));
    expect(onToggleCollapsed).toHaveBeenCalled();
  });
});
