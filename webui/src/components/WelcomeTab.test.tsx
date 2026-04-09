// @ts-nocheck
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import WelcomeTab from './WelcomeTab';

// ---------------------------------------------------------------------------
// Mocks & Setup
// ---------------------------------------------------------------------------

let rafId = 0;
beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  // Mock requestAnimationFrame for any effects that use it
  global.requestAnimationFrame = ((cb) => {
    rafId += 1;
    cb(Date.now());
    return rafId;
  }) as typeof requestAnimationFrame;
  global.cancelAnimationFrame = jest.fn();
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const defaultCallbacks = {
  onDismiss: jest.fn(),
  onOpenCommandPalette: jest.fn(),
  onOpenTerminal: jest.fn(),
  onViewGit: jest.fn(),
  onStartChat: jest.fn(),
};

let container: HTMLDivElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;

beforeEach(() => {
  jest.clearAllMocks();
  jest.spyOn(window, 'open').mockImplementation(() => null);
  container = document.createElement('div');
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    if (root) {
      root.unmount();
      root = null;
    }
  });
  if (container) {
    container.remove();
    container = null;
  }
  jest.restoreAllMocks();
});

/** Render WelcomeTab with all callbacks (allows overriding individual ones). */
function renderWithCallbacks(overrides: Partial<typeof defaultCallbacks> = {}) {
  const callbacks = { ...defaultCallbacks, ...overrides };
  act(() => {
    root = createRoot(container!);
    root.render(<WelcomeTab {...callbacks} />);
  });
  return callbacks;
}

/** Render WelcomeTab with no props. */
function renderNoProps() {
  act(() => {
    root = createRoot(container!);
    root.render(<WelcomeTab />);
  });
}

/** Helper: find a button whose text content matches the given regex. */
function findButtonByText(pattern: RegExp): HTMLButtonElement | null {
  const buttons = container!.querySelectorAll('button');
  for (const btn of buttons) {
    if (pattern.test(btn.textContent ?? '')) return btn as HTMLButtonElement;
  }
  return null;
}

/** Helper: find a resource card button whose text includes the given string. */
function findResourceCardByText(text: string): HTMLButtonElement | null {
  const cards = container!.querySelectorAll('button.resource-card');
  for (const card of cards) {
    if (card.textContent?.includes(text)) return card as HTMLButtonElement;
  }
  return null;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('WelcomeTab', () => {
  // -------------------------------------------------------------------------
  // Header rendering
  // -------------------------------------------------------------------------
  describe('Header rendering', () => {
    it('renders welcome heading and subtitle', () => {
      renderWithCallbacks();

      const heading = container!.querySelector('h1, h2, h3, [role="heading"]');
      expect(heading).not.toBeNull();
      expect(heading!.textContent).toMatch(/welcome to ledit/i);

      const allText = container!.textContent ?? '';
      expect(allText).toContain('Your AI-powered code editor');
    });

    it('renders dismiss button when onDismiss is provided', () => {
      renderWithCallbacks();

      const dismissBtn = container!.querySelector('[title="Dismiss welcome tab"]');
      expect(dismissBtn).toBeTruthy();
    });

    it('does NOT render dismiss button when onDismiss is not provided', () => {
      renderNoProps();

      const dismissBtn = container!.querySelector('[title="Dismiss welcome tab"]');
      expect(dismissBtn).toBeNull();
    });

    it('calls onDismiss when dismiss button is clicked', () => {
      const callbacks = renderWithCallbacks();

      const dismissBtn = container!.querySelector('[title="Dismiss welcome tab"]') as HTMLElement;
      act(() => { dismissBtn.click(); });

      expect(callbacks.onDismiss).toHaveBeenCalledTimes(1);
    });
  });

  // -------------------------------------------------------------------------
  // Quick Actions section
  // -------------------------------------------------------------------------
  describe('Quick Actions', () => {
    it('renders all four quick action buttons', () => {
      renderWithCallbacks();

      expect(findButtonByText(/open command palette/i)).toBeTruthy();
      expect(findButtonByText(/open terminal/i)).toBeTruthy();
      expect(findButtonByText(/view git history/i)).toBeTruthy();
      expect(findButtonByText(/start chat/i)).toBeTruthy();
    });

    it('shows the Quick Actions section heading', () => {
      renderWithCallbacks();

      const headings = container!.querySelectorAll('h1, h2, h3');
      const found = Array.from(headings).some((h) => /Quick Actions/i.test(h.textContent ?? ''));
      expect(found).toBe(true);
    });

    it('calls onOpenCommandPalette when clicking "Open Command Palette"', () => {
      const callbacks = renderWithCallbacks();

      act(() => { findButtonByText(/open command palette/i)!.click(); });
      expect(callbacks.onOpenCommandPalette).toHaveBeenCalledTimes(1);
    });

    it('calls onOpenTerminal when clicking "Open Terminal"', () => {
      const callbacks = renderWithCallbacks();

      act(() => { findButtonByText(/open terminal/i)!.click(); });
      expect(callbacks.onOpenTerminal).toHaveBeenCalledTimes(1);
    });

    it('calls onViewGit when clicking "View Git History"', () => {
      const callbacks = renderWithCallbacks();

      act(() => { findButtonByText(/view git history/i)!.click(); });
      expect(callbacks.onViewGit).toHaveBeenCalledTimes(1);
    });

    it('calls onStartChat when clicking "Start Chat"', () => {
      const callbacks = renderWithCallbacks();

      act(() => { findButtonByText(/start chat/i)!.click(); });
      expect(callbacks.onStartChat).toHaveBeenCalledTimes(1);
    });

    it('quick action buttons do not throw when callbacks are undefined', () => {
      renderNoProps();

      const buttons = [
        findButtonByText(/open command palette/i),
        findButtonByText(/open terminal/i),
        findButtonByText(/view git history/i),
        findButtonByText(/start chat/i),
      ];

      buttons.forEach((btn) => {
        expect(() => { if (btn) btn.click(); }).not.toThrow();
      });
    });
  });

  // -------------------------------------------------------------------------
  // Getting Started section
  // -------------------------------------------------------------------------
  describe('Getting Started section', () => {
    it('renders the Getting Started section heading', () => {
      renderWithCallbacks();

      const headings = container!.querySelectorAll('h1, h2, h3');
      const found = Array.from(headings).some((h) => /Get Started/i.test(h.textContent ?? ''));
      expect(found).toBe(true);
    });

    it('renders all six getting started cards', () => {
      renderWithCallbacks();

      const headings = [
        'Open a File',
        'Navigate Projects',
        'Run Commands',
        'Version Control',
        'AI Assistance',
        'Command Palette',
      ];

      const allText = container!.textContent ?? '';
      headings.forEach((title) => {
        expect(allText).toContain(title);
      });
    });

    it('renders descriptive text for each getting started card', () => {
      renderWithCallbacks();

      const allText = container!.textContent ?? '';
      expect(allText).toMatch(/Select a file from the file tree or use Ctrl\+P to search/);
      expect(allText).toMatch(/Use the file tree to browse your workspace/);
      expect(allText).toMatch(/Use the integrated terminal for shell commands/);
      expect(allText).toMatch(/View and manage git history and changes/);
      expect(allText).toMatch(/Chat with AI to get code help and analysis/);
      expect(allText).toMatch(/Access all commands with Ctrl\+P/);
    });
  });

  // -------------------------------------------------------------------------
  // Resources section
  // -------------------------------------------------------------------------
  describe('Resources section', () => {
    it('renders the Resources section heading', () => {
      renderWithCallbacks();

      const headings = container!.querySelectorAll('h1, h2, h3');
      const found = Array.from(headings).some((h) => /Resources/i.test(h.textContent ?? ''));
      expect(found).toBe(true);
    });

    it('renders all three resource cards', () => {
      renderWithCallbacks();

      const resourceButtons = container!.querySelectorAll('button.resource-card');
      expect(resourceButtons).toHaveLength(3);
    });

    it('renders Documentation, Settings, and Keyboard Shortcuts links', () => {
      renderWithCallbacks();

      const allText = container!.textContent ?? '';
      expect(allText).toContain('Documentation');
      expect(allText).toContain('Settings');
      expect(allText).toContain('Keyboard Shortcuts');
    });

    it('"Documentation" button opens external URL in new tab', () => {
      renderWithCallbacks();

      const docButton = findResourceCardByText('Documentation');
      expect(docButton).not.toBeNull();

      act(() => { docButton!.click(); });
      expect(window.open).toHaveBeenCalledWith('https://ledit.dev/docs', '_blank');
    });

    it('"Settings" button calls onOpenCommandPalette when provided', () => {
      const callbacks = renderWithCallbacks();

      const settingsButton = findResourceCardByText('Settings');
      expect(settingsButton).not.toBeNull();

      act(() => { settingsButton!.click(); });
      expect(callbacks.onOpenCommandPalette).toHaveBeenCalledTimes(1);
    });

    it('"Keyboard Shortcuts" button calls onOpenCommandPalette when provided', () => {
      const callbacks = renderWithCallbacks();

      const shortcutsButton = findResourceCardByText('Keyboard Shortcuts');
      expect(shortcutsButton).not.toBeNull();

      act(() => { shortcutsButton!.click(); });
      expect(callbacks.onOpenCommandPalette).toHaveBeenCalledTimes(1);
    });

    it('resource buttons with onOpenCommandPalette do not throw when callback is undefined', () => {
      renderNoProps();

      const resourceButtons = container!.querySelectorAll('button.resource-card');
      resourceButtons.forEach((btn) => {
        expect(() => { btn.click(); }).not.toThrow();
      });
    });
  });

  // -------------------------------------------------------------------------
  // Footer rendering
  // -------------------------------------------------------------------------
  describe('Footer rendering', () => {
    it('renders the pro tip text', () => {
      renderWithCallbacks();

      const footerEl = container!.querySelector('.welcome-footer');
      expect(footerEl).not.toBeNull();
      expect(footerEl!.textContent).toContain('Pro tip');
    });

    it('renders the Ctrl+P keyboard shortcut kbd element', () => {
      renderWithCallbacks();

      const kbd = container!.querySelector('kbd');
      expect(kbd).not.toBeNull();
      expect(kbd!.textContent).toBe('Ctrl+P');
    });

    it('renders the complete footer hint text with kbd element', () => {
      renderWithCallbacks();

      const footerEl = container!.querySelector('.welcome-footer');
      expect(footerEl).not.toBeNull();
      expect(footerEl!.textContent).toContain('Pro tip');
      expect(footerEl!.textContent).toContain('Ctrl+P');
      expect(footerEl!.textContent).toContain('command palette');
      expect(footerEl!.textContent).toContain('search for any command or file');

      const kbd = footerEl!.querySelector('kbd');
      expect(kbd).not.toBeNull();
      expect(kbd!.textContent).toBe('Ctrl+P');
    });
  });

  // -------------------------------------------------------------------------
  // No callbacks provided (graceful degradation)
  // -------------------------------------------------------------------------
  describe('rendering without callbacks', () => {
    it('renders without crashing when no props are provided', () => {
      expect(() => renderNoProps()).not.toThrow();
    });

    it('renders all sections even without callbacks', () => {
      renderNoProps();

      const allText = container!.textContent ?? '';
      expect(allText).toMatch(/welcome to ledit/i);
      expect(allText).toMatch(/Quick Actions/i);
      expect(allText).toMatch(/Get Started/i);
      expect(allText).toMatch(/Resources/i);
      expect(allText).toMatch(/Pro tip/);
    });
  });
});
