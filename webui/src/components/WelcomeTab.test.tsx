import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import WelcomeTab from './WelcomeTab';

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

/** Render WelcomeTab with all callbacks (allows overriding individual ones). */
function renderWithCallbacks(overrides: Partial<typeof defaultCallbacks> = {}) {
  const callbacks = { ...defaultCallbacks, ...overrides };
  return {
    ...render(<WelcomeTab {...callbacks} />),
    callbacks,
  };
}

beforeEach(() => {
  jest.clearAllMocks();
  // Spy on window.open so we can verify external link behavior
  jest.spyOn(window, 'open').mockImplementation(() => null);
});

afterEach(() => {
  jest.restoreAllMocks();
});

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

      expect(screen.getByRole('heading', { name: /welcome to ledit/i })).toBeInTheDocument();
      expect(screen.getByText('Your AI-powered code editor')).toBeInTheDocument();
    });

    it('renders dismiss button when onDismiss is provided', () => {
      renderWithCallbacks();

      const dismissBtn = screen.getByTitle('Dismiss welcome tab');
      expect(dismissBtn).toBeInTheDocument();
    });

    it('does NOT render dismiss button when onDismiss is not provided', () => {
      render(<WelcomeTab />);

      expect(screen.queryByTitle('Dismiss welcome tab')).not.toBeInTheDocument();
    });

    it('calls onDismiss when dismiss button is clicked', () => {
      const { callbacks } = renderWithCallbacks();

      const dismissBtn = screen.getByTitle('Dismiss welcome tab');
      fireEvent.click(dismissBtn);

      expect(callbacks.onDismiss).toHaveBeenCalledTimes(1);
    });
  });

  // -------------------------------------------------------------------------
  // Quick Actions section
  // -------------------------------------------------------------------------
  describe('Quick Actions', () => {
    it('renders all four quick action buttons', () => {
      renderWithCallbacks();

      expect(screen.getByRole('button', { name: /open command palette/i })).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /open terminal/i })).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /view git history/i })).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /start chat/i })).toBeInTheDocument();
    });

    it('shows the Quick Actions section heading', () => {
      renderWithCallbacks();

      // The Quick Actions h2
      const quickActionsHeading = screen.getByRole('heading', { name: /Quick Actions/i });
      expect(quickActionsHeading).toBeInTheDocument();
    });

    it('calls onOpenCommandPalette when clicking "Open Command Palette"', () => {
      const { callbacks } = renderWithCallbacks();

      fireEvent.click(screen.getByRole('button', { name: /open command palette/i }));
      expect(callbacks.onOpenCommandPalette).toHaveBeenCalledTimes(1);
    });

    it('calls onOpenTerminal when clicking "Open Terminal"', () => {
      const { callbacks } = renderWithCallbacks();

      fireEvent.click(screen.getByRole('button', { name: /open terminal/i }));
      expect(callbacks.onOpenTerminal).toHaveBeenCalledTimes(1);
    });

    it('calls onViewGit when clicking "View Git History"', () => {
      const { callbacks } = renderWithCallbacks();

      fireEvent.click(screen.getByRole('button', { name: /view git history/i }));
      expect(callbacks.onViewGit).toHaveBeenCalledTimes(1);
    });

    it('calls onStartChat when clicking "Start Chat"', () => {
      const { callbacks } = renderWithCallbacks();

      fireEvent.click(screen.getByRole('button', { name: /start chat/i }));
      expect(callbacks.onStartChat).toHaveBeenCalledTimes(1);
    });

    it('quick action buttons do not throw when callbacks are undefined', () => {
      render(<WelcomeTab />);

      // All four buttons should be present even without callbacks
      const buttons = [
        screen.getByRole('button', { name: /open command palette/i }),
        screen.getByRole('button', { name: /open terminal/i }),
        screen.getByRole('button', { name: /view git history/i }),
        screen.getByRole('button', { name: /start chat/i }),
      ];

      // Clicking each should not throw
      buttons.forEach((btn) => {
        expect(() => fireEvent.click(btn)).not.toThrow();
      });
    });
  });

  // -------------------------------------------------------------------------
  // Getting Started section
  // -------------------------------------------------------------------------
  describe('Getting Started section', () => {
    it('renders the Getting Started section heading', () => {
      renderWithCallbacks();

      expect(screen.getByRole('heading', { name: /Get Started/i })).toBeInTheDocument();
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

      headings.forEach((title) => {
        expect(screen.getByRole('heading', { name: title })).toBeInTheDocument();
      });
    });

    it('renders descriptive text for each getting started card', () => {
      renderWithCallbacks();

      expect(screen.getByText(/Select a file from the file tree or use Ctrl\+P to search/)).toBeInTheDocument();
      expect(screen.getByText(/Use the file tree to browse your workspace/)).toBeInTheDocument();
      expect(screen.getByText(/Use the integrated terminal for shell commands/)).toBeInTheDocument();
      expect(screen.getByText(/View and manage git history and changes/)).toBeInTheDocument();
      expect(screen.getByText(/Chat with AI to get code help and analysis/)).toBeInTheDocument();
      expect(screen.getByText(/Access all commands with Ctrl\+P/)).toBeInTheDocument();
    });
  });

  // -------------------------------------------------------------------------
  // Resources section
  // -------------------------------------------------------------------------
  describe('Resources section', () => {
    it('renders the Resources section heading', () => {
      renderWithCallbacks();

      expect(screen.getByRole('heading', { name: /Resources/i })).toBeInTheDocument();
    });

    it('renders all three resource cards', () => {
      renderWithCallbacks();

      const resourceButtons = screen.getAllByRole('button').filter((btn) => btn.classList.contains('resource-card'));
      expect(resourceButtons).toHaveLength(3);
    });

    it('renders Documentation, Settings, and Keyboard Shortcuts links', () => {
      renderWithCallbacks();

      expect(screen.getByText('Documentation')).toBeInTheDocument();
      expect(screen.getByText('Settings')).toBeInTheDocument();
      expect(screen.getByText('Keyboard Shortcuts')).toBeInTheDocument();
    });

    it('"Documentation" button opens external URL in new tab', () => {
      renderWithCallbacks();

      // The Documentation button — find by its title text within a resource card
      const docButton = screen
        .getAllByRole('button')
        .find((btn) => btn.classList.contains('resource-card') && btn.textContent?.includes('Documentation'));
      expect(docButton).toBeDefined();

      fireEvent.click(docButton!);
      expect(window.open).toHaveBeenCalledWith('https://ledit.dev/docs', '_blank');
    });

    it('"Settings" button calls onOpenCommandPalette when provided', () => {
      const { callbacks } = renderWithCallbacks();

      const settingsButton = screen
        .getAllByRole('button')
        .find((btn) => btn.classList.contains('resource-card') && btn.textContent?.includes('Settings'));
      expect(settingsButton).toBeDefined();

      fireEvent.click(settingsButton!);
      expect(callbacks.onOpenCommandPalette).toHaveBeenCalledTimes(1);
    });

    it('"Keyboard Shortcuts" button calls onOpenCommandPalette when provided', () => {
      const { callbacks } = renderWithCallbacks();

      const shortcutsButton = screen
        .getAllByRole('button')
        .find((btn) => btn.classList.contains('resource-card') && btn.textContent?.includes('Keyboard Shortcuts'));
      expect(shortcutsButton).toBeDefined();

      fireEvent.click(shortcutsButton!);
      expect(callbacks.onOpenCommandPalette).toHaveBeenCalledTimes(1);
    });

    it('resource buttons with onOpenCommandPalette do not throw when callback is undefined', () => {
      render(<WelcomeTab />);

      // Settings and Keyboard Shortcuts buttons both use onOpenCommandPalette
      const resourceButtons = screen.getAllByRole('button').filter((btn) => btn.classList.contains('resource-card'));

      resourceButtons.forEach((btn) => {
        expect(() => fireEvent.click(btn)).not.toThrow();
      });
    });
  });

  // -------------------------------------------------------------------------
  // Footer rendering
  // -------------------------------------------------------------------------
  describe('Footer rendering', () => {
    it('renders the pro tip text', () => {
      renderWithCallbacks();

      expect(screen.getByText(/Pro tip:/)).toBeInTheDocument();
    });

    it('renders the Ctrl+P keyboard shortcut kbd element', () => {
      renderWithCallbacks();

      const kbd = screen.getByText('Ctrl+P');
      expect(kbd.tagName).toBe('KBD');
      expect(kbd).toBeInTheDocument();
    });

    it('renders the complete footer hint text with kbd element', () => {
      renderWithCallbacks();

      const footerEl = document.querySelector('.welcome-footer');
      expect(footerEl).not.toBeNull();
      expect(footerEl!.textContent).toContain('Pro tip');
      expect(footerEl!.textContent).toContain('Ctrl+P');
      expect(footerEl!.textContent).toContain('command palette');
      expect(footerEl!.textContent).toContain('search for any command or file');

      // Verify the kbd element is present within the footer
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
      expect(() => render(<WelcomeTab />)).not.toThrow();
    });

    it('renders all sections even without callbacks', () => {
      render(<WelcomeTab />);

      // Header
      expect(screen.getByRole('heading', { name: /welcome to ledit/i })).toBeInTheDocument();
      // Quick Actions
      expect(screen.getByRole('heading', { name: /Quick Actions/i })).toBeInTheDocument();
      // Getting Started
      expect(screen.getByRole('heading', { name: /Get Started/i })).toBeInTheDocument();
      // Resources
      expect(screen.getByRole('heading', { name: /Resources/i })).toBeInTheDocument();
      // Footer
      expect(screen.getByText(/Pro tip:/)).toBeInTheDocument();
    });
  });
});
