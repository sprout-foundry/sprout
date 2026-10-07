import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  COPY_INSTALLED_EVENT,
  COPY_KEYS,
  DEFAULT_COPY,
  copy,
  formatCopy,
  installCopy,
  resetCopyForTests,
} from './copy';

afterEach(() => {
  resetCopyForTests();
  vi.restoreAllMocks();
});

describe('copy defaults', () => {
  // The registry contract: each key resolves to today's literal English text.
  // Asserting the literal (not DEFAULT_COPY itself) is the point — a future
  // edit to a default fails here instead of silently changing the UI.
  const EXPECTED: Record<string, string> = {
    'app.name': 'sprout',
    'modeSwitcher.label': 'Switch mode',
    'nav.git': 'Git',
    'nav.files': 'Files',
    'nav.search': 'Search',
    'nav.automations': 'Automations',
    'nav.design': 'Design',
    'nav.settings': 'Settings',
    'nav.logs': 'Logs',
    'home.title': 'Home',
    'home.work': 'Work',
    'home.account': 'Account',
    'home.backToProject': 'Back to {project}',
    'home.dashboard': 'Dashboard',
    'home.tasks': 'Tasks',
    'home.workspaces': 'Workspaces',
    'home.billing': 'Usage & billing',
    'home.team': 'Team',
    'home.runners': 'Runners',
    'home.settings': 'Settings',
    'home.admin': 'Admin',
    'welcome.title': 'Welcome to sprout',
    'welcome.subtitle': 'Your AI-powered code editor',
    'welcome.pickerTitle': 'Welcome to Sprout!',
    'welcome.pickerSubtitle': 'Ask the AI to create files, or use the Files tab to get started.',
    'chat.welcome': "Welcome to sprout! I'm ready to help you with code analysis, editing, and more.",
    'chat.welcomeHint': 'Try asking: "Show me the project structure" or "Find the main function"',
    'chat.offlineTitle': 'No Server Connection',
    'chat.offlineDescription':
      'Chat requires a connection to your Sprout server. Your editor and terminal remain available while offline.',
    'chat.noProvider': 'No AI provider configured',
    'chat.noProviderHint':
      'AI features require a provider to be set up. The editor, terminal, file tree, and git panels are fully functional without one.',
    'chat.openRepo': 'Open a repository to get started',
    'chat.openRepoHint':
      'The agent works on the repository you open. Public GitHub repositories work without connecting an account.',
    'workspace.none': 'No repository open',
    'action.retryConnection': 'Retry Connection',
    'action.configureProvider': 'Configure Provider',
    'action.open': 'Open',
    'action.newProject': 'Start a new project',
    'action.chooseRepo': 'Choose from your repositories',
  };

  it('covers every declared key', () => {
    expect([...COPY_KEYS].sort()).toEqual(Object.keys(EXPECTED).sort());
  });

  it.each(Object.entries(EXPECTED))('%s resolves to its literal default', (key, text) => {
    expect(copy(key as keyof typeof DEFAULT_COPY)).toBe(text);
    expect(DEFAULT_COPY[key as keyof typeof DEFAULT_COPY]).toBe(text);
  });
});

describe('installCopy', () => {
  it('changes the resolved string for the overridden key only', () => {
    installCopy({ 'app.name': 'acme' });
    expect(copy('app.name')).toBe('acme');
    expect(copy('nav.files')).toBe('Files');
    expect(copy('welcome.title')).toBe('Welcome to sprout');
  });

  it('merges successive installs without dropping earlier overrides', () => {
    installCopy({ 'app.name': 'acme' });
    installCopy({ 'nav.files': 'Documents' });
    expect(copy('app.name')).toBe('acme');
    expect(copy('nav.files')).toBe('Documents');
  });

  it('treats an empty-string override as a real override', () => {
    installCopy({ 'welcome.subtitle': '' });
    expect(copy('welcome.subtitle')).toBe('');
  });

  it('ignores unknown keys', () => {
    installCopy({ 'not.a.key': 'nope' } as never);
    expect(copy('app.name')).toBe('sprout');
  });

  it('fires the installed event so late readers can re-render', () => {
    const listener = vi.fn();
    window.addEventListener(COPY_INSTALLED_EVENT, listener);
    installCopy({ 'app.name': 'acme' });
    window.removeEventListener(COPY_INSTALLED_EVENT, listener);
    expect(listener).toHaveBeenCalledTimes(1);
  });
});

describe('resetCopyForTests', () => {
  it('returns every key to its default', () => {
    installCopy({ 'app.name': 'acme', 'nav.files': 'Documents' });
    resetCopyForTests();
    expect(copy('app.name')).toBe('sprout');
    expect(copy('nav.files')).toBe('Files');
  });
});

describe('formatCopy', () => {
  it('substitutes named placeholders', () => {
    expect(formatCopy('home.backToProject', { project: 'sprout-foundry' })).toBe('Back to sprout-foundry');
  });

  it('honors an override carrying the same placeholder', () => {
    installCopy({ 'home.backToProject': 'Return to {project}' });
    expect(formatCopy('home.backToProject', { project: 'acme' })).toBe('Return to acme');
  });

  it('leaves an unknown placeholder intact', () => {
    expect(formatCopy('home.backToProject', {})).toBe('Back to {project}');
  });
});

describe('unknown key fallback', () => {
  it('resolves an absent key to the key text instead of throwing', () => {
    expect(copy('does.not.exist' as never)).toBe('does.not.exist');
  });
});
