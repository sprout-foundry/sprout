import { describe, it, expect, vi } from 'vitest';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import ChatMetricsStrip from './ChatMetricsStrip';

vi.mock('@sprout/ui', async () => {
  const actual = await vi.importActual<any>('@sprout/ui');
  return { ...actual, getPersonaColor: () => '#8888ff' };
});

vi.mock('../../contexts/ProviderCatalogContext', () => ({
  useProviderCatalog: () => ({ getProviderName: (id: string) => (id === 'openrouter' ? 'OpenRouter' : id) }),
}));

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  // @ts-expect-error test env flag
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
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

function render(props: Parameters<typeof ChatMetricsStrip>[0]) {
  act(() => {
    root.render(createElement(ChatMetricsStrip, props));
  });
}

describe('ChatMetricsStrip', () => {
  it('renders nothing when stats are empty and connected', () => {
    render({ stats: {}, isConnected: true });
    expect(container.querySelector('.chat-metrics-strip')).toBeNull();
  });

  it('renders the disconnected pill even without stats', () => {
    render({ stats: null, isConnected: false });
    const strip = container.querySelector('.chat-metrics-strip')!;
    expect(strip.textContent).toContain('disconnected');
    expect(strip.textContent).toContain('Link: disconnected');
  });

  it('shows persona, context, tokens, and cost segments', () => {
    render({
      stats: {
        persona: 'web_scraper',
        provider: 'openrouter',
        model: 'test-model',
        total_tokens: 1500,
        current_context_tokens: 2000,
        max_context_tokens: 8000,
        total_cost: 0.5,
        context_usage_percent: 25,
      },
      isConnected: true,
    });
    const text = container.querySelector('.chat-metrics-strip')!.textContent!;
    expect(text).toContain('Web Scraper');
    expect(text).toContain('25.0%');
    expect(text).toContain('1.5k tok');
    expect(text).toContain('$0.500');
    expect(text).toContain('Link: connected');
  });

  it('colors cost by threshold', () => {
    render({ stats: { total_cost: 7 }, isConnected: true });
    expect(container.querySelector('.chat-metrics-cost--alert')).not.toBeNull();

    act(() => {
      root.render(createElement(ChatMetricsStrip, { stats: { total_cost: 2 } }));
    });
    expect(container.querySelector('.chat-metrics-cost--warn')).not.toBeNull();
    expect(container.querySelector('.chat-metrics-cost--alert')).toBeNull();
  });

  it('model name is a button when onModelClick is given', () => {
    const onModelClick = vi.fn();
    render({
      stats: { provider: 'openrouter', model: 'm1', persona: 'coder' },
      isConnected: true,
      onModelClick,
    });
    const btn = container.querySelector('.chat-metrics-model-button') as HTMLButtonElement;
    expect(btn).not.toBeNull();
    expect(btn.textContent).toBe('m1');
    act(() => {
      btn.click();
    });
    expect(onModelClick).toHaveBeenCalledWith('openrouter');
  });

  it('separator count is segments minus one (no trailing dot)', () => {
    render({ stats: { persona: 'coder', total_tokens: 10 }, isConnected: true });
    const items = container.querySelectorAll('.chat-metrics-item').length;
    const seps = container.querySelectorAll('.chat-metrics-sep').length;
    expect(seps).toBe(items - 1);
  });
});
