/**
 * Copy keys.
 *
 * Primary shell strings (product name, mode-switcher and navigation labels,
 * empty-state headlines, primary action buttons) resolve through here instead
 * of being literals in the components. Each key carries today's exact English
 * text as its default, so the default build renders byte-for-byte what it did
 * before — only an embedding that installs overrides sees different wording.
 *
 * Only the *primary chrome* strings live here; incidental labels stay in their
 * components. A key is added when an embedding plausibly needs to reword it,
 * not for coverage.
 *
 * Resolution is a function call (`copy('nav.files')`) rather than an exported
 * constant on purpose: the value is read at render time, so an install that
 * lands after this module loads (the same asynchronous window the API adapter
 * lives in) is still observed by mount/use-time readers. Don't snapshot a key
 * into a module-scope constant — that freezes it.
 */

/** Every copy key. Complete union, so a typo is a compile error. */
export type CopyKey =
  | 'app.name'
  | 'modeSwitcher.label'
  | 'nav.git'
  | 'nav.files'
  | 'nav.search'
  | 'nav.automations'
  | 'nav.design'
  | 'nav.settings'
  | 'nav.logs'
  | 'home.title'
  | 'home.work'
  | 'home.account'
  | 'home.backToProject'
  | 'home.dashboard'
  | 'home.tasks'
  | 'home.workspaces'
  | 'home.billing'
  | 'home.team'
  | 'home.runners'
  | 'home.settings'
  | 'home.admin'
  | 'welcome.title'
  | 'welcome.subtitle'
  | 'welcome.pickerTitle'
  | 'welcome.pickerSubtitle'
  | 'chat.welcome'
  | 'chat.welcomeHint'
  | 'chat.offlineTitle'
  | 'chat.offlineDescription'
  | 'chat.noProvider'
  | 'chat.noProviderHint'
  | 'chat.openRepo'
  | 'chat.openRepoHint'
  | 'workspace.none'
  | 'action.retryConnection'
  | 'action.configureProvider'
  | 'action.open'
  | 'action.newProject'
  | 'action.chooseRepo';

/** Replacements an embedding may install. Partial: install only what you reword. */
export type CopyOverrides = Partial<Record<CopyKey, string>>;

/**
 * The default English text. This is the contract the UI ships with: every key's
 * value here is asserted in copy.test.ts so a future edit cannot silently
 * change the default UI.
 */
export const DEFAULT_COPY: Readonly<Record<CopyKey, string>> = Object.freeze({
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
});

/** All keys, in declaration order. */
export const COPY_KEYS: readonly CopyKey[] = Object.freeze(Object.keys(DEFAULT_COPY) as CopyKey[]);

/** Fired after installCopy() merges a new override set. */
export const COPY_INSTALLED_EVENT = 'sprout:copy-installed';

let overrides: CopyOverrides = {};

/**
 * Resolve a copy key: the installed override when one exists, otherwise the
 * default text. A key the build does not know (an extension calling with a
 * mistyped or forward-looking key) falls back to the key itself rather than
 * throwing — a missing word must not blank the chrome or take a render down.
 */
export function copy(key: CopyKey): string {
  const override = overrides[key];
  if (typeof override === 'string') return override;
  const fallback = (DEFAULT_COPY as Record<string, string | undefined>)[key as string];
  return typeof fallback === 'string' ? fallback : String(key);
}

/**
 * Resolve a copy key with `{name}` placeholders substituted. Unknown
 * placeholders are left intact so a missing parameter shows through instead of
 * silently dropping text.
 */
export function formatCopy(key: CopyKey, params: Record<string, string | number>): string {
  return copy(key).replace(/\{(\w+)\}/g, (match, name: string) =>
    Object.prototype.hasOwnProperty.call(params, name) ? String(params[name]) : match,
  );
}

/**
 * Install an embedding's wording. Merge semantics: keys not supplied keep their
 * current value (default, or a previous override), and unknown keys are ignored
 * so a stray entry cannot pollute the registry. An empty-string value is a
 * legitimate override (hiding a string).
 */
export function installCopy(next: CopyOverrides): void {
  const known: CopyOverrides = {};
  for (const key of Object.keys(next)) {
    if (key in DEFAULT_COPY) known[key as CopyKey] = next[key as CopyKey];
  }
  overrides = { ...overrides, ...known };
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event(COPY_INSTALLED_EVENT));
  }
}

/** Test-only: drop every override and return to the shipped defaults. */
export function resetCopyForTests(): void {
  overrides = {};
}
