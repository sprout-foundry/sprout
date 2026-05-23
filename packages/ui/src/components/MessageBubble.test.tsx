// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi } from 'vitest';
import MessageBubble from './MessageBubble';
import * as clipboard from '../utils/clipboard';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  vi.spyOn(clipboard, 'copyToClipboard').mockResolvedValue();
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('MessageBubble', () => {
  it('renders with default type (assistant)', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test message',
        children: 'Hello',
      }));
    });

    const bubble = container.querySelector('.message');
    expect(bubble).not.toBeNull();
    expect(bubble?.classList.contains('assistant')).toBe(true);
    expect(bubble?.getAttribute('role')).toBe('assistant-message');
  });

  it('renders with user type', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        type: 'user',
        ariaLabel: 'User message',
        children: 'Hi there',
      }));
    });

    const bubble = container.querySelector('.message');
    expect(bubble).not.toBeNull();
    expect(bubble?.classList.contains('user')).toBe(true);
    expect(bubble?.getAttribute('role')).toBe('user-message');
  });

  it('applies aria-label', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Custom label',
        children: 'Content',
      }));
    });

    const bubble = container.querySelector('.message');
    expect(bubble?.getAttribute('aria-label')).toBe('Custom label');
  });

  it('renders children content inside message-content', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        children: 'My content',
      }));
    });

    const content = container.querySelector('.message-content');
    expect(content).not.toBeNull();
    expect(content?.textContent).toBe('My content');
  });

  it('renders children as React elements', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        children: createElement('span', { 'data-testid': 'child-span' }, 'Nested'),
      }));
    });

    expect(container.querySelector('[data-testid="child-span"]')).not.toBeNull();
  });

  it('shows copy button when copyText is provided', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        copyText: 'Text to copy',
        children: 'Content',
      }));
    });

    const copyBtn = container.querySelector('.copy-button');
    expect(copyBtn).not.toBeNull();
    expect(copyBtn?.getAttribute('title')).toBe('Copy message');
    expect(copyBtn?.getAttribute('aria-label')).toBe('Copy message');
  });

  it('hides copy button when copyText is not provided', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        children: 'Content',
      }));
    });

    expect(container.querySelector('.copy-button')).toBeNull();
  });

  it('hides copy button when copyText is empty string', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        copyText: '',
        children: 'Content',
      }));
    });

    expect(container.querySelector('.copy-button')).toBeNull();
  });

  it('calls copyToClipboard with copyText on copy button click', async () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        copyText: 'Copied content',
        children: 'Content',
      }));
    });

    const copyBtn = container.querySelector('.copy-button');
    await act(async () => {
      copyBtn?.click();
    });

    expect(clipboard.copyToClipboard).toHaveBeenCalledWith('Copied content');
  });

  it('shows timestamp when provided', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        timestamp: '2024-01-15 10:30:00',
        children: 'Content',
      }));
    });

    const ts = container.querySelector('.message-timestamp');
    expect(ts).not.toBeNull();
    expect(ts?.textContent).toBe('2024-01-15 10:30:00');
  });

  it('hides timestamp when not provided', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        children: 'Content',
      }));
    });

    expect(container.querySelector('.message-timestamp')).toBeNull();
  });

  it('sets data-message-content attribute on bubble when copyText is provided', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        copyText: 'Copiable text',
        children: 'Content',
      }));
    });

    const bubbleInner = container.querySelector('[data-message-content]');
    expect(bubbleInner).not.toBeNull();
    expect(bubbleInner?.getAttribute('data-message-content')).toBe('Copiable text');
  });

  it('sets data-message-content to empty string when no copyText', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        children: 'Content',
      }));
    });

    const bubbleInner = container.querySelector('[data-message-content]');
    expect(bubbleInner).not.toBeNull();
    expect(bubbleInner?.getAttribute('data-message-content')).toBe('');
  });

  it('renders copy button with SVG icon', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Test',
        copyText: 'Text',
        children: 'Content',
      }));
    });

    const copyBtn = container.querySelector('.copy-button');
    expect(copyBtn?.querySelector('svg')).not.toBeNull();
  });

  it('combines all features: user type, copyText, timestamp', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        type: 'user',
        ariaLabel: 'User copyable message',
        copyText: 'Copiable',
        timestamp: 'Now',
        children: 'Full example',
      }));
    });

    const bubble = container.querySelector('.message');
    expect(bubble?.classList.contains('user')).toBe(true);
    expect(bubble?.getAttribute('role')).toBe('user-message');
    expect(container.querySelector('.copy-button')).not.toBeNull();
    expect(container.querySelector('.message-timestamp')?.textContent).toBe('Now');
    expect(container.querySelector('.message-content')?.textContent).toBe('Full example');
  });

  // ─────────────────────────────────────────────────────────────────────
  // SP-053-1d: persona badge + depth indent
  // ─────────────────────────────────────────────────────────────────────

  // Backwards-compat pin: a MessageBubble called the old way (no persona,
  // no depth) must produce no badge, no inline marginLeft, and no
  // data-subagent-depth attr — so sprout-foundry and other consumers that
  // haven't upgraded see byte-identical layout.
  it('SP-053: omits badge and indent when persona/depth absent (backwards-compat)', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Primary agent message',
        children: 'Hello',
      }));
    });

    const bubble = container.querySelector('.message') as HTMLElement | null;
    expect(bubble).not.toBeNull();
    expect(container.querySelector('.message-persona-badge')).toBeNull();
    // No inline marginLeft for primary-agent bubbles.
    expect(bubble?.style.marginLeft).toBe('');
    expect(bubble?.getAttribute('data-subagent-depth')).toBeNull();
  });

  it('renders the persona name (as a chip) when persona is set', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Coder subagent message',
        persona: 'coder',
        children: 'Coder said',
      }));
    });

    const badge = container.querySelector('.message-persona-badge') as HTMLElement | null;
    expect(badge).not.toBeNull();
    expect(badge?.textContent).toBe('coder');
    // The persona color now flows in via the --persona-color CSS custom
    // property set on the outer .message wrapper (so the depth rail and
    // the chip both pick it up). The chip text-color is `var(--persona-color)`
    // in CSS — JSDOM doesn't resolve custom properties, so we only assert
    // that the variable is set on the wrapper.
    const wrapper = container.querySelector('.message') as HTMLElement | null;
    expect(wrapper?.style.getPropertyValue('--persona-color')).toBe('#58a6ff');
  });

  it('SP-053: applies depth indent when depth > 0', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Depth 2 message',
        depth: 2,
        children: 'Deep',
      }));
    });

    const bubble = container.querySelector('.message') as HTMLElement | null;
    expect(bubble?.style.marginLeft).toBe('24px'); // 2 * 12px
    expect(bubble?.getAttribute('data-subagent-depth')).toBe('2');
  });

  it('SP-053: depth 0 with persona renders badge but no indent', () => {
    // A primary-agent message could still have a persona (e.g.
    // orchestrator at depth 0). Badge shows; no indent applied.
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Orchestrator message',
        persona: 'orchestrator',
        depth: 0,
        children: 'Plan',
      }));
    });

    const bubble = container.querySelector('.message') as HTMLElement | null;
    expect(container.querySelector('.message-persona-badge')).not.toBeNull();
    expect(bubble?.style.marginLeft).toBe('');
    expect(bubble?.getAttribute('data-subagent-depth')).toBeNull();
  });

  it('unknown persona falls back to the neutral mid-gray color via the custom property', () => {
    act(() => {
      root.render(createElement(MessageBubble, {
        ariaLabel: 'Unknown',
        persona: 'made_up_persona',
        children: 'x',
      }));
    });

    const badge = container.querySelector('.message-persona-badge') as HTMLElement | null;
    expect(badge?.textContent).toBe('made_up_persona');
    const wrapper = container.querySelector('.message') as HTMLElement | null;
    // Fallback color #6e7681 — neutral mid-gray, readable on both themes.
    expect(wrapper?.style.getPropertyValue('--persona-color')).toBe('#6e7681');
  });
});
