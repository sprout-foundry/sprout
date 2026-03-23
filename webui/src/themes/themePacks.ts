export type ThemeMode = 'dark' | 'light';

export type EditorSyntaxStyle = 'one-dark' | 'default';

export interface ThemePack {
  id: string;
  name: string;
  mode: ThemeMode;
  description?: string;
  editorSyntaxStyle: EditorSyntaxStyle;
  variables: Record<string, string>;
}

const atomOneDark: ThemePack = {
  id: 'atom-one-dark',
  name: 'Atom One Dark',
  mode: 'dark',
  description: 'Classic Atom One Dark palette',
  editorSyntaxStyle: 'one-dark',
  variables: {
    '--bg-primary': '#282c34',
    '--bg-secondary': '#21252b',
    '--bg-tertiary': '#2c313a',
    '--bg-elevated': '#303742',
    '--bg-surface': '#3a404b',

    '--border-subtle': 'rgba(171, 178, 191, 0.14)',
    '--border-default': 'rgba(171, 178, 191, 0.22)',
    '--border-strong': 'rgba(171, 178, 191, 0.34)',
    '--border-focus': 'rgba(97, 175, 239, 0.75)',

    '--text-primary': '#abb2bf',
    '--text-secondary': '#9aa3b2',
    '--text-tertiary': '#7f8794',
    '--text-muted': '#5c6370',

    '--accent-primary': '#61afef',
    '--accent-secondary': '#56b6c2',
    '--accent-success': '#98c379',
    '--accent-warning': '#e5c07b',
    '--accent-error': '#e06c75',
    '--accent-cyan': '#56b6c2',
    '--accent-primary-alpha': 'rgba(97, 175, 239, 0.2)',

    '--color-success-bg': 'rgba(152, 195, 121, 0.12)',
    '--color-warning-bg': 'rgba(229, 192, 123, 0.12)',
    '--color-error-bg': 'rgba(224, 108, 117, 0.12)',
    '--color-info-bg': 'rgba(97, 175, 239, 0.12)',

    '--editor-pane-bg': 'linear-gradient(135deg, #282c34 0%, #2b3038 100%)',
    '--editor-pane-footer-bg': 'rgba(0, 0, 0, 0.16)',
    '--editor-pane-footer-fg': 'rgba(171, 178, 191, 0.75)',
    '--editor-empty-fg': 'rgba(171, 178, 191, 0.56)',
    '--editor-loading-bg': 'rgba(97, 175, 239, 0.2)',
    '--editor-loading-fg': '#d6e9ff',
    '--editor-error-bg': 'rgba(224, 108, 117, 0.2)',
    '--editor-error-fg': '#ffd7dc',

    '--toolbar-bg': '#2b313b',
    '--toolbar-fg': '#abb2bf',
    '--toolbar-hover-bg': '#373d48',
    '--toolbar-hover-fg': '#ffffff',
    '--accent-bg': '#61afef',
    '--accent-fg': '#0f1115',
    '--border-color': 'rgba(171, 178, 191, 0.24)',
    '--input-bg': '#242932',
    '--input-fg': '#d7dce4',

    '--cm-bg': '#282c34',
    '--cm-fg': '#abb2bf',
    '--cm-gutter-bg': '#21252b',
    '--cm-gutter-fg': '#636d83',
    '--cm-gutter-fg-active': '#abb2bf',
    '--cm-cursor': '#528bff',
    '--cm-selection': 'rgba(62, 68, 81, 0.95)',
    '--cm-active-line': 'rgba(44, 49, 60, 0.8)',
    '--cm-active-line-gutter': 'rgba(44, 49, 60, 0.8)',
    '--editor-pane-inactive-overlay': 'rgba(0, 0, 0, 0.28)',
    '--editor-pane-inactive-overlay-hover': 'rgba(0, 0, 0, 0.18)',

    '--app-shell-bg': 'linear-gradient(180deg, #262b33, #282c34)',
    '--app-main-bg': '#282c34',
    '--sidebar-bg': 'linear-gradient(180deg, #21252b, #1f2329)',
    '--sidebar-header-bg': 'rgba(33, 37, 43, 0.95)',
    '--sidebar-nav-btn-bg': '#2c313a',
    '--sidebar-nav-btn-hover-bg': '#353b45',
    '--sidebar-nav-btn-active-bg': 'linear-gradient(180deg, #3a404b, #343a45)',
    '--sidebar-select-bg': '#242932',

    '--gradient-subtle': 'radial-gradient(1100px 420px at -5% -10%, rgba(97, 175, 239, 0.08), transparent 55%), radial-gradient(1100px 420px at 105% -10%, rgba(86, 182, 194, 0.08), transparent 58%)',
    '--gradient-elevated': 'linear-gradient(145deg, rgba(97, 175, 239, 0.16), rgba(86, 182, 194, 0.12))',
  },
};

const atomOneLight: ThemePack = {
  id: 'atom-one-light',
  name: 'Atom One Light',
  mode: 'light',
  description: 'Light companion to Atom One Dark',
  editorSyntaxStyle: 'default',
  variables: {
    '--bg-primary': '#fafafa',
    '--bg-secondary': '#f2f2f2',
    '--bg-tertiary': '#eaeaeb',
    '--bg-elevated': '#e3e4e6',
    '--bg-surface': '#d8dadd',

    '--border-subtle': 'rgba(56, 60, 67, 0.12)',
    '--border-default': 'rgba(56, 60, 67, 0.2)',
    '--border-strong': 'rgba(56, 60, 67, 0.3)',
    '--border-focus': 'rgba(64, 120, 242, 0.65)',

    '--text-primary': '#383a42',
    '--text-secondary': '#4f535f',
    '--text-tertiary': '#696f7a',
    '--text-muted': '#8a9099',

    '--accent-primary': '#4078f2',
    '--accent-secondary': '#0184bc',
    '--accent-success': '#50a14f',
    '--accent-warning': '#c18401',
    '--accent-error': '#e45649',
    '--accent-cyan': '#0184bc',
    '--accent-primary-alpha': 'rgba(64, 120, 242, 0.16)',

    '--editor-pane-bg': 'linear-gradient(135deg, #fafafa 0%, #f3f4f6 100%)',
    '--editor-pane-footer-bg': 'rgba(56, 58, 66, 0.05)',
    '--editor-pane-footer-fg': 'rgba(56, 58, 66, 0.72)',
    '--editor-empty-fg': 'rgba(56, 58, 66, 0.54)',
    '--editor-loading-bg': 'rgba(64, 120, 242, 0.14)',
    '--editor-loading-fg': '#1f3e83',
    '--editor-error-bg': 'rgba(228, 86, 73, 0.14)',
    '--editor-error-fg': '#7f2c23',

    '--toolbar-bg': '#eceef1',
    '--toolbar-fg': '#4f535f',
    '--toolbar-hover-bg': '#dfe3e8',
    '--toolbar-hover-fg': '#2f3137',
    '--accent-bg': '#4078f2',
    '--accent-fg': '#ffffff',
    '--border-color': 'rgba(56, 60, 67, 0.22)',
    '--input-bg': '#ffffff',
    '--input-fg': '#383a42',

    '--cm-bg': '#fafafa',
    '--cm-fg': '#383a42',
    '--cm-gutter-bg': '#f2f2f2',
    '--cm-gutter-fg': '#8a9099',
    '--cm-gutter-fg-active': '#383a42',
    '--cm-cursor': '#526fff',
    '--cm-selection': 'rgba(196, 211, 255, 0.75)',
    '--cm-active-line': 'rgba(0, 0, 0, 0.035)',
    '--cm-active-line-gutter': 'rgba(0, 0, 0, 0.035)',
    '--editor-pane-inactive-overlay': 'rgba(56, 58, 66, 0.1)',
    '--editor-pane-inactive-overlay-hover': 'rgba(56, 58, 66, 0.06)',

    '--app-shell-bg': 'linear-gradient(180deg, #f6f6f6, #fafafa)',
    '--app-main-bg': '#fafafa',
    '--sidebar-bg': 'linear-gradient(180deg, #f1f2f4, #eceef1)',
    '--sidebar-header-bg': 'rgba(236, 238, 241, 0.95)',
    '--sidebar-nav-btn-bg': '#e4e7ec',
    '--sidebar-nav-btn-hover-bg': '#d9dee5',
    '--sidebar-nav-btn-active-bg': 'linear-gradient(180deg, #4078f2, #315ec4)',
    '--sidebar-select-bg': '#ffffff',

    '--gradient-subtle': 'radial-gradient(1200px 420px at -5% -10%, rgba(64, 120, 242, 0.08), transparent 55%), radial-gradient(1100px 420px at 105% -10%, rgba(1, 132, 188, 0.08), transparent 58%)',
    '--gradient-elevated': 'linear-gradient(145deg, rgba(64, 120, 242, 0.12), rgba(1, 132, 188, 0.1))',
  },
};

const dracula: ThemePack = {
  id: 'dracula',
  name: 'Dracula',
  mode: 'dark',
  description: 'Popular dark theme pack',
  editorSyntaxStyle: 'one-dark',
  variables: {
    '--bg-primary': '#282a36',
    '--bg-secondary': '#21222c',
    '--bg-tertiary': '#303442',
    '--bg-elevated': '#353949',
    '--bg-surface': '#44475a',

    '--border-subtle': 'rgba(248, 248, 242, 0.14)',
    '--border-default': 'rgba(248, 248, 242, 0.22)',
    '--border-strong': 'rgba(248, 248, 242, 0.34)',
    '--border-focus': 'rgba(139, 233, 253, 0.8)',

    '--text-primary': '#f8f8f2',
    '--text-secondary': '#bdc0cc',
    '--text-tertiary': '#999cb0',
    '--text-muted': '#6272a4',

    '--accent-primary': '#8be9fd',
    '--accent-secondary': '#50fa7b',
    '--accent-success': '#50fa7b',
    '--accent-warning': '#f1fa8c',
    '--accent-error': '#ff5555',
    '--accent-cyan': '#8be9fd',
    '--accent-primary-alpha': 'rgba(139, 233, 253, 0.2)',
  },
};

export const THEME_PACKS: ThemePack[] = [atomOneDark, atomOneLight, dracula];

export const DEFAULT_THEME_PACK_ID = 'atom-one-dark';

export const THEME_VARIABLE_KEYS = Array.from(
  new Set(THEME_PACKS.flatMap((pack) => Object.keys(pack.variables)))
);

export function getThemePackByID(themePackID: string): ThemePack {
  return THEME_PACKS.find((pack) => pack.id === themePackID) ||
    THEME_PACKS.find((pack) => pack.id === DEFAULT_THEME_PACK_ID) ||
    THEME_PACKS[0];
}

export function getThemePackForMode(mode: ThemeMode): ThemePack {
  return THEME_PACKS.find((pack) => pack.mode === mode) || getThemePackByID(DEFAULT_THEME_PACK_ID);
}
