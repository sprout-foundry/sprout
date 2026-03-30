// @ts-nocheck

import React from 'react';
import { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { buildKeyString, HotkeyProvider } from './HotkeyContext';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Mock the ApiService module so the provider can mount without real API calls.
// The loadHotkeys callback will fail gracefully (sets isLoaded=true in finally).
jest.mock('../services/api', () => {
  class MockApiService {
    private static instance: MockApiService;
    static getInstance() {
      if (!MockApiService.instance) MockApiService.instance = new MockApiService();
      return MockApiService.instance;
    }
    async getHotkeys() {
      throw new Error('API not available in tests');
    }
    async applyHotkeyPreset() {
      throw new Error('API not available in tests');
    }
  }
  return {
    ApiService: MockApiService,
  };
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  jest.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

const flushPromises = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

/** Create a mock KeyboardEvent with the given key and modifier flags. */
function makeKeyEvent(opts: Partial<KeyboardEvent> & { key: string }): KeyboardEvent {
  return new KeyboardEvent('keydown', {
    key: opts.key,
    metaKey: opts.metaKey ?? false,
    ctrlKey: opts.ctrlKey ?? false,
    altKey: opts.altKey ?? false,
    shiftKey: opts.shiftKey ?? false,
    bubbles: true,
    cancelable: true,
  });
}

// ---------------------------------------------------------------------------
// Tests: buildKeyString
// ---------------------------------------------------------------------------

describe('buildKeyString', () => {
  it('returns key name alone when no modifiers', () => {
    const ev = makeKeyEvent({ key: 'Escape' });
    expect(buildKeyString(ev)).toBe('Escape');
  });

  it('includes Cmd for metaKey', () => {
    const ev = makeKeyEvent({ key: 's', metaKey: true });
    expect(buildKeyString(ev)).toBe('Cmd+s');
  });

  it('includes Ctrl for ctrlKey', () => {
    const ev = makeKeyEvent({ key: 's', ctrlKey: true });
    expect(buildKeyString(ev)).toBe('Ctrl+s');
  });

  it('includes Alt modifier', () => {
    const ev = makeKeyEvent({ key: 'f', altKey: true });
    expect(buildKeyString(ev)).toBe('Alt+f');
  });

  it('includes Shift modifier', () => {
    const ev = makeKeyEvent({ key: 'A', shiftKey: true });
    expect(buildKeyString(ev)).toBe('Shift+A');
  });

  it('includes multiple modifiers in canonical order (Cmd, Ctrl, Alt, Shift)', () => {
    const ev = makeKeyEvent({ key: 's', ctrlKey: true, shiftKey: true });
    expect(buildKeyString(ev)).toBe('Ctrl+Shift+s');
  });

  it('includes Cmd+Shift+S in canonical order', () => {
    const ev = makeKeyEvent({ key: 'S', metaKey: true, shiftKey: true });
    expect(buildKeyString(ev)).toBe('Cmd+Shift+S');
  });

  it('includes Alt+ArrowUp', () => {
    const ev = makeKeyEvent({ key: 'ArrowUp', altKey: true });
    expect(buildKeyString(ev)).toBe('Alt+Up');
  });

  it('handles Tab key', () => {
    const ev = makeKeyEvent({ key: 'Tab' });
    expect(buildKeyString(ev)).toBe('Tab');
  });

  it('handles Ctrl+Tab', () => {
    const ev = makeKeyEvent({ key: 'Tab', ctrlKey: true });
    expect(buildKeyString(ev)).toBe('Ctrl+Tab');
  });

  it('handles Ctrl+Shift+Tab', () => {
    const ev = makeKeyEvent({ key: 'Tab', ctrlKey: true, shiftKey: true });
    expect(buildKeyString(ev)).toBe('Ctrl+Shift+Tab');
  });

  it('digit keys work (e.g. Ctrl+5)', () => {
    const ev = makeKeyEvent({ key: '5', ctrlKey: true });
    expect(buildKeyString(ev)).toBe('Ctrl+5');
  });

  it('handles Ctrl+W', () => {
    const ev = makeKeyEvent({ key: 'w', ctrlKey: true });
    expect(buildKeyString(ev)).toBe('Ctrl+w');
  });

  it('backtick remains as backtick (keyMap overrides the initial remap)', () => {
    // Note: buildKeyString first remaps '`' → 'Backquote', but then
    // keyMap['Backquote'] = '`' undoes it. This is the current behavior.
    const ev = makeKeyEvent({ key: '`' });
    expect(buildKeyString(ev)).toBe('`');
  });

  it('Enter key maps correctly', () => {
    const ev = makeKeyEvent({ key: 'Enter' });
    expect(buildKeyString(ev)).toBe('Enter');
  });

  it('Space key stays as space character (keyMap["Space"]=" ") does not apply for event.key=" ")', () => {
    // event.key for Space is ' '. keyMap has key 'Space' not ' '.
    const ev = makeKeyEvent({ key: ' ' });
    expect(buildKeyString(ev)).toBe(' ');
  });

  it('ArrowDown maps to Down', () => {
    const ev = makeKeyEvent({ key: 'ArrowDown' });
    expect(buildKeyString(ev)).toBe('Down');
  });

  it('ArrowLeft maps to Left', () => {
    const ev = makeKeyEvent({ key: 'ArrowLeft' });
    expect(buildKeyString(ev)).toBe('Left');
  });

  it('ArrowRight maps to Right', () => {
    const ev = makeKeyEvent({ key: 'ArrowRight' });
    expect(buildKeyString(ev)).toBe('Right');
  });

  it('Delete key maps correctly', () => {
    const ev = makeKeyEvent({ key: 'Delete' });
    expect(buildKeyString(ev)).toBe('Delete');
  });

  it('Backspace key maps correctly', () => {
    const ev = makeKeyEvent({ key: 'Backspace' });
    expect(buildKeyString(ev)).toBe('Backspace');
  });

  it('handles Ctrl+Space (space character, not "Space" string)', () => {
    const ev = makeKeyEvent({ key: ' ', ctrlKey: true });
    expect(buildKeyString(ev)).toBe('Ctrl+ ');
  });

  it('handles a simple letter key without modifiers', () => {
    const ev = makeKeyEvent({ key: 'a' });
    expect(buildKeyString(ev)).toBe('a');
  });

  it('handles Escape without modifiers', () => {
    const ev = makeKeyEvent({ key: 'Escape' });
    expect(buildKeyString(ev)).toBe('Escape');
  });

  it('handles Ctrl+1 for tab focus', () => {
    const ev = makeKeyEvent({ key: '1', ctrlKey: true });
    expect(buildKeyString(ev)).toBe('Ctrl+1');
  });

  it('handles Cmd+1 for tab focus (Mac)', () => {
    const ev = makeKeyEvent({ key: '1', metaKey: true });
    expect(buildKeyString(ev)).toBe('Cmd+1');
  });
});

// ---------------------------------------------------------------------------
// Tests: HotkeyProvider integration
// ---------------------------------------------------------------------------

describe('HotkeyProvider', () => {
  it('renders without crashing (smoke test)', () => {
    act(() => {
      root.render(React.createElement(HotkeyProvider, null, React.createElement('div')));
    });
    // If we get here, the provider mounted successfully
    expect(container.querySelector('div')).not.toBeNull();
  });
});

describe('fallback hotkeys are wired to hotkey commands', () => {
  it('dispatches ledit:hotkey when a fallback hotkey matches', async () => {
    act(() => {
      root.render(
        React.createElement(HotkeyProvider, null, React.createElement('div')),
      );
    });

    // Wait for the provider's async initialization to settle
    await flushPromises();

    const handler = jest.fn();
    window.addEventListener('ledit:hotkey', handler);

    const ev = new KeyboardEvent('keydown', { key: 's', ctrlKey: true, bubbles: true });
    window.dispatchEvent(ev);

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail.commandId).toBe('save_file');

    window.removeEventListener('ledit:hotkey', handler);
  });

  it('does not dispatch hotkey when input is focused and hotkey is not global', async () => {
    act(() => {
      root.render(
        React.createElement(HotkeyProvider, null, React.createElement('div')),
      );
    });

    await flushPromises();

    const handler = jest.fn();
    window.addEventListener('ledit:hotkey', handler);

    // Create and focus an input element
    const input = document.createElement('input');
    container.appendChild(input);
    input.focus();

    // Ctrl+Tab has global=false, should NOT fire when input is focused.
    // Dispatch from the input so event.target is the input element.
    const ev = new KeyboardEvent('keydown', { key: 'Tab', ctrlKey: true, bubbles: true });
    input.dispatchEvent(ev);

    expect(handler).not.toHaveBeenCalled();

    input.remove();
    window.removeEventListener('ledit:hotkey', handler);
  });

  it('dispatches global hotkey even when input is focused', async () => {
    act(() => {
      root.render(
        React.createElement(HotkeyProvider, null, React.createElement('div')),
      );
    });

    await flushPromises();

    const handler = jest.fn();
    window.addEventListener('ledit:hotkey', handler);

    // Create and focus an input element
    const input = document.createElement('input');
    container.appendChild(input);
    input.focus();

    // Ctrl+S has global=true, should fire even with input focused.
    const ev = new KeyboardEvent('keydown', { key: 's', ctrlKey: true, bubbles: true });
    input.dispatchEvent(ev);

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail.commandId).toBe('save_file');

    input.remove();
    window.removeEventListener('ledit:hotkey', handler);
  });

  it('dispatches event with correct detail (commandId and key)', async () => {
    act(() => {
      root.render(
        React.createElement(HotkeyProvider, null, React.createElement('div')),
      );
    });

    await flushPromises();

    const handler = jest.fn();
    window.addEventListener('ledit:hotkey', handler);

    const ev = new KeyboardEvent('keydown', { key: 's', ctrlKey: true, bubbles: true });
    window.dispatchEvent(ev);

    expect(handler).toHaveBeenCalledTimes(1);
    const detail = handler.mock.calls[0][0].detail;
    expect(detail.commandId).toBe('save_file');
    expect(detail.key).toBe('Ctrl+S');

    window.removeEventListener('ledit:hotkey', handler);
  });

  it('does not dispatch event for unrecognized key combinations', async () => {
    act(() => {
      root.render(
        React.createElement(HotkeyProvider, null, React.createElement('div')),
      );
    });

    await flushPromises();

    const handler = jest.fn();
    window.addEventListener('ledit:hotkey', handler);

    // Ctrl+Z is not in the fallback hotkeys
    const ev = new KeyboardEvent('keydown', { key: 'z', ctrlKey: true, bubbles: true });
    window.dispatchEvent(ev);

    expect(handler).not.toHaveBeenCalled();

    window.removeEventListener('ledit:hotkey', handler);
  });
});
