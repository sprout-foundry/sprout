// @ts-nocheck
/**
 * SP-053-3d: tests for the chat-context status-bar segment that lives
 * in the right slot of the shared @sprout/ui StatusBar. Pins:
 *   - null/empty stats → renders nothing.
 *   - Full stats → provider icon, model name, ctx, cost all visible.
 *   - Missing fields → corresponding segment omitted, no orphan separators.
 *   - Cost color thresholds: below warn = plain, warn = yellow class, alert = red class.
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi } from 'vitest';
import { ChatStatusBarItems } from './ChatStatusBarItems';

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  // @ts-expect-error
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
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

describe('ChatStatusBarItems', () => {
  it('renders nothing when stats is null', () => {
    act(() => {
      root.render(createElement(ChatStatusBarItems, { stats: null }));
    });
    expect(container.children.length).toBe(0);
  });

  it('renders nothing when stats is empty', () => {
    act(() => {
      root.render(createElement(ChatStatusBarItems, { stats: {} }));
    });
    // No segments — output should be empty (just React fragment with no children).
    expect(container.querySelectorAll('.chat-statusbar-item').length).toBe(0);
  });

  it('renders model, ctx, and cost when all fields are present', () => {
    act(() => {
      root.render(createElement(ChatStatusBarItems, { stats: {
        provider: 'anthropic',
        model: 'claude-haiku-4-5',
        current_context_tokens: 14200,
        max_context_tokens: 200000,
        total_cost: 0.42,
      }}));
    });
    const items = container.querySelectorAll('.chat-statusbar-item');
    expect(items.length).toBe(3);
    expect(container.querySelector('.chat-statusbar-model-name')?.textContent).toBe('claude-haiku-4-5');
    expect(items[1]?.textContent).toContain('14.2k/200.0k ctx');
    expect(container.querySelector('.chat-statusbar-cost')?.textContent).toBe('$0.420');
  });

  it('omits segments for missing fields, with no orphan separators', () => {
    act(() => {
      root.render(createElement(ChatStatusBarItems, { stats: {
        model: 'gpt-5',
        total_cost: 0.01,
      }}));
    });
    const items = container.querySelectorAll('.chat-statusbar-item');
    expect(items.length).toBe(2); // model + cost (no ctx)
    const seps = container.querySelectorAll('.chat-statusbar-sep');
    expect(seps.length).toBe(1); // exactly one separator between the two
  });

  it('falls back to total_tokens when ctx fields are absent', () => {
    act(() => {
      root.render(createElement(ChatStatusBarItems, { stats: {
        model: 'gpt-5',
        total_tokens: 1500,
      }}));
    });
    // Find the tok segment.
    const items = Array.from(container.querySelectorAll('.chat-statusbar-item'));
    const tok = items.find((el) => el.textContent?.includes('tok'));
    expect(tok).not.toBeUndefined();
    expect(tok?.textContent).toContain('1.5k tok');
  });

  it('cost below warn threshold has no color class', () => {
    act(() => {
      root.render(createElement(ChatStatusBarItems, { stats: { total_cost: 0.50 }}));
    });
    const cost = container.querySelector('.chat-statusbar-cost') as HTMLElement | null;
    expect(cost).not.toBeNull();
    expect(cost?.classList.contains('chat-statusbar-cost--warn')).toBe(false);
    expect(cost?.classList.contains('chat-statusbar-cost--alert')).toBe(false);
  });

  it('cost above $1 gets the warn class (yellow)', () => {
    act(() => {
      root.render(createElement(ChatStatusBarItems, { stats: { total_cost: 2.00 }}));
    });
    const cost = container.querySelector('.chat-statusbar-cost');
    expect(cost?.classList.contains('chat-statusbar-cost--warn')).toBe(true);
  });

  it('cost above $5 gets the alert class (red)', () => {
    act(() => {
      root.render(createElement(ChatStatusBarItems, { stats: { total_cost: 10.00 }}));
    });
    const cost = container.querySelector('.chat-statusbar-cost');
    expect(cost?.classList.contains('chat-statusbar-cost--alert')).toBe(true);
  });

  it('renders provider icon (lucide SVG) when provider is set', () => {
    act(() => {
      root.render(createElement(ChatStatusBarItems, { stats: { provider: 'anthropic', model: 'claude-haiku-4-5' }}));
    });
    const modelItem = container.querySelector('.chat-statusbar-model');
    expect(modelItem?.querySelector('svg')).not.toBeNull();
  });

  it('renders the active persona badge when stats.persona is set (and not "orchestrator")', () => {
    act(() => {
      root.render(createElement(ChatStatusBarItems, { stats: {
        provider: 'anthropic',
        model: 'claude-haiku-4-5',
        persona: 'coder',
      }}));
    });
    const persona = container.querySelector('.chat-statusbar-persona') as HTMLElement | null;
    expect(persona).not.toBeNull();
    expect(persona?.textContent).toBe('coder');
    // getPersonaColor('coder') → #58a6ff → rgb(88, 166, 255)
    expect(persona?.style.color.replace(/\s/g, '').toLowerCase()).toBe('rgb(88,166,255)');
  });

  it('omits the persona segment when persona is "orchestrator" (primary, unmarked)', () => {
    act(() => {
      root.render(createElement(ChatStatusBarItems, { stats: {
        provider: 'anthropic',
        model: 'claude-haiku-4-5',
        persona: 'orchestrator',
      }}));
    });
    expect(container.querySelector('.chat-statusbar-persona')).toBeNull();
  });

  it('renders the model as a button when onModelClick is provided', () => {
    const onClick = vi.fn();
    act(() => {
      root.render(createElement(ChatStatusBarItems, {
        stats: { provider: 'anthropic', model: 'claude-haiku-4-5' },
        onModelClick: onClick,
      }));
    });
    const btn = container.querySelector('.chat-statusbar-model-button') as HTMLButtonElement | null;
    expect(btn).not.toBeNull();
    act(() => {
      btn?.click();
    });
    expect(onClick).toHaveBeenCalledWith('anthropic');
  });

  it('renders the model as plain text when onModelClick is not provided', () => {
    act(() => {
      root.render(createElement(ChatStatusBarItems, {
        stats: { provider: 'anthropic', model: 'claude-haiku-4-5' },
      }));
    });
    expect(container.querySelector('.chat-statusbar-model-button')).toBeNull();
    expect(container.querySelector('.chat-statusbar-model-name')?.textContent).toBe('claude-haiku-4-5');
  });
});
