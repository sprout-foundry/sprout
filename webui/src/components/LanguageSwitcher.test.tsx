import { createRoot } from 'react-dom/client';
import { act } from 'react';
import LanguageSwitcher from './LanguageSwitcher';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

jest.mock('../extensions/languageRegistry', () => {
  const mockEntries: Array<{ id: string; name: string; extensions: string[] }> = [
    { id: 'javascript', name: 'JavaScript', extensions: ['js', 'mjs', 'cjs'] },
    { id: 'typescript', name: 'TypeScript', extensions: ['ts'] },
    { id: 'python', name: 'Python', extensions: ['py'] },
    { id: 'go', name: 'Go', extensions: ['go'] },
    { id: 'html', name: 'HTML', extensions: ['html', 'htm'] },
    { id: 'css', name: 'CSS', extensions: ['css'] },
    { id: 'json', name: 'JSON', extensions: ['json'] },
    { id: 'plaintext', name: 'Plain Text', extensions: ['txt'] },
  ];
  return {
    allLanguageEntries: mockEntries,
    getLanguageExtensions: () => [],
    resolveLanguageId: jest.fn(),
    detectLanguage: jest.fn(),
  };
});

jest.mock('lucide-react', () => ({
  Check: () => <span data-testid="check-icon" />,
  FileCode: () => <span data-testid="filecode-icon" />,
}));

// Mock requestAnimationFrame so effects (outside-click listener,
// scroll-into-view, popup positioning) fire synchronously.
let rafId = 0;
beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  global.requestAnimationFrame = ((cb: FrameRequestCallback) => {
    rafId += 1;
    cb(Date.now());
    return rafId;
  }) as typeof requestAnimationFrame;
  global.cancelAnimationFrame = jest.fn() as jest.Mock;
  // jsdom does not implement scrollIntoView
  Element.prototype.scrollIntoView = jest.fn() as jest.Mock;
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let mountPoint: HTMLDivElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;

beforeEach(() => {
  jest.clearAllMocks();
  mountPoint = document.createElement('div');
  document.body.appendChild(mountPoint);
});

afterEach(() => {
  act(() => {
    if (root) {
      root.unmount();
      root = null;
    }
  });
  if (mountPoint) {
    document.body.removeChild(mountPoint);
    mountPoint = null;
  }
  // Clean up any portals left on body
  document.querySelectorAll('.language-switcher-popup').forEach((el) => el.remove());
});

interface RenderOptions {
  currentLanguageId?: string | null;
  isAutoDetected?: boolean;
  onLanguageChange?: jest.Mock;
}

function renderSwitcher(opts: RenderOptions = {}) {
  const { currentLanguageId = null, isAutoDetected = true, onLanguageChange = jest.fn() } = opts;

  // eslint-disable-next-line testing-library/no-unnecessary-act
  act(() => {
    root = createRoot(mountPoint!);
    root.render(
      <LanguageSwitcher
        currentLanguageId={currentLanguageId}
        isAutoDetected={isAutoDetected}
        onLanguageChange={onLanguageChange}
      />,
    );
  });

  return { onLanguageChange };
}

/**
 * Simulate typing into the search input of the open popup.
 */
function typeInSearch(text: string) {
  const input = document.querySelector('.language-switcher-search-input') as HTMLInputElement;
  expect(input).not.toBeNull();
  act(() => {
    // Use native input value setter then dispatch an input event
    // so React picks it up via the onChange handler (React's onChange
    // is bound to the native 'input' event, not 'change').
    const desc = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value');
    expect(desc?.set).toBeDefined();
    (desc as PropertyDescriptor).set!.call(input, text);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

/**
 * Fire a keydown event on the search input (where handleKeyDown is attached).
 */
function fireSearchKeyDown(key: string) {
  const input = document.querySelector('.language-switcher-search-input') as HTMLElement;
  expect(input).not.toBeNull();
  act(() => {
    input.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }));
  });
}

/**
 * Open the popup by clicking the button. Does NOT flush RAF since the mock
 * fires the callback synchronously.
 */
function openPopup() {
  const btn = document.querySelector('[data-testid="language-switcher-button"]') as HTMLElement;
  expect(btn).not.toBeNull();
  act(() => {
    btn.click();
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('LanguageSwitcher', () => {
  // ---- 1. Display name: auto-detected ----
  test('renders display name as "Auto (JavaScript)" when isAutoDetected=true and currentLanguageId is javascript', () => {
    renderSwitcher({ currentLanguageId: 'javascript', isAutoDetected: true });
    const btn = document.querySelector('[data-testid="language-switcher-button"]')!;
    const label = btn.querySelector('.language-switcher-label')!;
    expect(label.textContent).toBe('Auto (JavaScript)');
    expect(btn.getAttribute('data-language-id')).toBe('javascript');
    expect(btn.getAttribute('data-auto-detected')).toBe('true');
  });

  // ---- 2. Display name: user-override ----
  test('renders display name as "Python" when isAutoDetected=false and currentLanguageId is python', () => {
    renderSwitcher({ currentLanguageId: 'python', isAutoDetected: false });
    const btn = document.querySelector('[data-testid="language-switcher-button"]')!;
    const label = btn.querySelector('.language-switcher-label')!;
    expect(label.textContent).toBe('Python');
    expect(btn.getAttribute('data-auto-detected')).toBe('false');
  });

  // ---- 3. Display name: no language (null) ----
  test('renders "Auto" when currentLanguageId is null', () => {
    renderSwitcher({ currentLanguageId: null, isAutoDetected: false });
    const btn = document.querySelector('[data-testid="language-switcher-button"]')!;
    const label = btn.querySelector('.language-switcher-label')!;
    expect(label.textContent).toBe('Auto');
    expect(btn.getAttribute('data-language-id')).toBe('auto');
  });

  // ---- 4. Popup opens on button click ----
  test('opens popup on button click', () => {
    renderSwitcher();
    const btn = document.querySelector('[data-testid="language-switcher-button"]') as HTMLElement;
    expect(document.querySelector('[data-testid="language-switcher-popup"]')).toBeNull();

    act(() => {
      btn.click();
    });

    const popup = document.querySelector('[data-testid="language-switcher-popup"]');
    expect(popup).not.toBeNull();
  });

  // ---- 5. Auto-detect option with check when auto-detected ----
  test('shows auto-detect option with check mark when auto-detected', () => {
    renderSwitcher({ currentLanguageId: 'typescript', isAutoDetected: true });
    openPopup();

    const items = document.querySelectorAll('.language-switcher-item');
    // First item should be "Auto-detect"
    expect(items[0]!.querySelector('.language-switcher-item-name')!.textContent).toBe('Auto-detect');
    // Should have the check icon because isAutoDetected is true
    expect(items[0]!.querySelector('[data-testid="check-icon"]')).not.toBeNull();
  });

  // ---- 6. Clicking Auto-detect calls onLanguageChange(null) and closes popup ----
  test('calls onLanguageChange(null) and closes popup when clicking Auto-detect', () => {
    const { onLanguageChange } = renderSwitcher({
      currentLanguageId: 'javascript',
      isAutoDetected: true,
    });
    openPopup();

    const autoItem = document.querySelectorAll('.language-switcher-item')[0]!;
    act(() => {
      autoItem.click();
    });

    expect(onLanguageChange).toHaveBeenCalledTimes(1);
    expect(onLanguageChange).toHaveBeenCalledWith(null);
    // Popup should be closed
    expect(document.querySelector('[data-testid="language-switcher-popup"]')).toBeNull();
  });

  // ---- 7. Clicking a language entry calls onLanguageChange(id) and closes popup ----
  test('calls onLanguageChange with language id when clicking a language entry', () => {
    const { onLanguageChange } = renderSwitcher({
      currentLanguageId: 'javascript',
      isAutoDetected: true,
    });
    openPopup();

    // Find the "Go" item (index 4 in allLanguageEntries, rendered at list index 5 = items[4])
    const languageItems = Array.from(document.querySelectorAll('.language-switcher-item'));
    const goItem = languageItems.find((el) => el.querySelector('.language-switcher-item-name')!.textContent === 'Go');
    expect(goItem).not.toBeNull();

    act(() => {
      goItem!.click();
    });

    expect(onLanguageChange).toHaveBeenCalledTimes(1);
    expect(onLanguageChange).toHaveBeenCalledWith('go');
    expect(document.querySelector('[data-testid="language-switcher-popup"]')).toBeNull();
  });

  // ---- 8. Check mark for current language when user-override (not auto-detected) ----
  test('shows check mark for current language when user-override (not auto-detected)', () => {
    renderSwitcher({ currentLanguageId: 'python', isAutoDetected: false });
    openPopup();

    const languageItems = document.querySelectorAll('.language-switcher-item');
    // auto-detect (index 0) should NOT have a check because isAutoDetected=false AND currentLanguageId is not null
    expect(languageItems[0]!.querySelector('[data-testid="check-icon"]')).toBeNull();

    // Find the "Python" item — it should have the check icon (it's the user override)
    const pythonItem = Array.from(languageItems).find(
      (el) => el.querySelector('.language-switcher-item-name')!.textContent === 'Python',
    )!;
    expect(pythonItem.querySelector('[data-testid="check-icon"]')).not.toBeNull();
  });

  // ---- 9. Search filters language list ----
  test('filters language list by search query', () => {
    renderSwitcher({ currentLanguageId: null, isAutoDetected: false });
    openPopup();

    // All items before filtering: 1 (auto-detect) + 8 languages = 9
    expect(document.querySelectorAll('.language-switcher-item').length).toBe(9);

    typeInSearch('java');

    // Should match "JavaScript" only — so 2 items: auto-detect + JavaScript
    const items = document.querySelectorAll('.language-switcher-item');
    expect(items.length).toBe(2);
    const names = Array.from(items).map((el) => el.querySelector('.language-switcher-item-name')!.textContent);
    expect(names).toContain('Auto-detect');
    expect(names).toContain('JavaScript');
  });

  // ---- 10. "No matching languages" when filter matches nothing ----
  test('shows "No matching languages" message when filter matches nothing', () => {
    renderSwitcher({ currentLanguageId: null, isAutoDetected: false });
    openPopup();

    typeInSearch('zzzzznothing');

    const noResults = document.querySelector('.language-switcher-no-results');
    expect(noResults).not.toBeNull();
    expect(noResults!.textContent).toBe('No matching languages');
  });

  // ---- 11. Keyboard navigation: ArrowDown + Enter ----
  test('keyboard navigation: ArrowDown moves selection, Enter selects', () => {
    const { onLanguageChange } = renderSwitcher({
      currentLanguageId: null,
      isAutoDetected: false,
    });
    openPopup();

    // Initially selectedIndex should be 0 (Auto-detect)
    let items = document.querySelectorAll('.language-switcher-item');
    expect(items[0]!.classList.contains('selected')).toBe(true);

    // ArrowDown three times → selectedIndex becomes 3 → "Python" (3rd language entry)
    // Index mapping: 0=Auto-detect, 1=javascript, 2=typescript, 3=python
    fireSearchKeyDown('ArrowDown');
    fireSearchKeyDown('ArrowDown');
    fireSearchKeyDown('ArrowDown');

    items = document.querySelectorAll('.language-switcher-item');
    expect(items[3]!.classList.contains('selected')).toBe(true);

    // Press Enter to select Python
    fireSearchKeyDown('Enter');

    expect(onLanguageChange).toHaveBeenCalledTimes(1);
    expect(onLanguageChange).toHaveBeenCalledWith('python');
    expect(document.querySelector('[data-testid="language-switcher-popup"]')).toBeNull();
  });

  // ---- 12. Clicking outside closes popup ----
  test('closes popup when clicking outside', () => {
    renderSwitcher();
    openPopup();

    expect(document.querySelector('[data-testid="language-switcher-popup"]')).not.toBeNull();

    // Create an element outside the popup and dispatch mousedown on it
    const outsideEl = document.createElement('div');
    document.body.appendChild(outsideEl);

    act(() => {
      outsideEl.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });

    expect(document.querySelector('[data-testid="language-switcher-popup"]')).toBeNull();
    document.body.removeChild(outsideEl);
  });

  // ---- 13. Escape closes popup ----
  test('closes popup on Escape key', () => {
    renderSwitcher();
    openPopup();

    expect(document.querySelector('[data-testid="language-switcher-popup"]')).not.toBeNull();

    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });

    expect(document.querySelector('[data-testid="language-switcher-popup"]')).toBeNull();
  });

  // ---- 14. Home / End keyboard navigation ----
  test('keyboard navigation: Home jumps to first item, End jumps to last item', () => {
    renderSwitcher({ currentLanguageId: null, isAutoDetected: false });
    openPopup();

    // Press End to go to last item (index 8 = Plain Text)
    fireSearchKeyDown('End');
    let items = document.querySelectorAll('.language-switcher-item');
    // There are 9 items (1 auto + 8 languages), last index = 8
    expect(items[8]!.classList.contains('selected')).toBe(true);
    expect(items[8]!.querySelector('.language-switcher-item-name')!.textContent).toBe('Plain Text');

    // Press Home to go back to first item (auto-detect, index 0)
    fireSearchKeyDown('Home');
    items = document.querySelectorAll('.language-switcher-item');
    expect(items[0]!.classList.contains('selected')).toBe(true);
  });

  // ---- 15. Footer with keyboard hints ----
  test('shows footer with keyboard shortcut hints', () => {
    renderSwitcher();
    openPopup();

    const footer = document.querySelector('.language-switcher-footer');
    expect(footer).not.toBeNull();
    expect(footer!.textContent).toContain('Navigate');
    expect(footer!.textContent).toContain('Select');
    expect(footer!.textContent).toContain('Close');
  });

  // ---- 16. Closes popup when button is clicked again ----
  test('closes popup when button is clicked again', () => {
    renderSwitcher();
    const btn = document.querySelector('[data-testid="language-switcher-button"]') as HTMLElement;

    // Open
    act(() => {
      btn.click();
    });
    expect(document.querySelector('[data-testid="language-switcher-popup"]')).not.toBeNull();

    // Close by clicking the same button again
    act(() => {
      btn.click();
    });
    expect(document.querySelector('[data-testid="language-switcher-popup"]')).toBeNull();
  });

  // ---- 17. ArrowUp at index 0 stays at 0 ----
  test('keyboard navigation: ArrowUp at index 0 stays at 0', () => {
    renderSwitcher({ currentLanguageId: null, isAutoDetected: false });
    openPopup();

    // selectedIndex starts at 0 (Auto-detect)
    fireSearchKeyDown('ArrowUp');

    const items = document.querySelectorAll('.language-switcher-item');
    expect(items[0]!.classList.contains('selected')).toBe(true);
    // Nothing else should be selected
    expect(items[1]!.classList.contains('selected')).toBe(false);
  });

  // ---- 18. Unknown language ID renders "Auto" ----
  test('renders "Auto" when currentLanguageId is an unknown value', () => {
    renderSwitcher({ currentLanguageId: 'nonexistent-lang', isAutoDetected: false });
    const btn = document.querySelector('[data-testid="language-switcher-button"]')!;
    const label = btn.querySelector('.language-switcher-label')!;
    expect(label.textContent).toBe('Auto');
  });
});
