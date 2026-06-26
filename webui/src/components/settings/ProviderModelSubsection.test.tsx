import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi } from 'vitest';
import ProviderModelSubsection from './ProviderModelSubsection';

import type { ProviderOption } from '../../services/api';

vi.mock('@sprout/ui', () => ({
  SkeletonText: () => createElement('div', { 'data-testid': 'skeleton' }),
}));

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

const providers: ProviderOption[] = [
  { id: 'anthropic', name: 'Anthropic', models: ['claude-haiku-4-5', 'claude-sonnet-4-6'] },
  { id: 'openai', name: 'OpenAI', models: ['gpt-5', 'gpt-5-mini'] },
];

function createBaseProps(overrides: Record<string, unknown> = {}) {
  return {
    label: 'Provider & Model',
    provider: '',
    model: '',
    providers,
    models: ['claude-haiku-4-5', 'claude-sonnet-4-6'],
    onProviderChange: vi.fn(),
    onModelChange: vi.fn(),
    scope: 'session' as const,
    ...overrides,
  };
}

// ─── Inherited display mode ───

describe('Inherited display mode', () => {
  test('renders inherited value when inheritedValue is set and provider/model are empty', () => {
    const props = createBaseProps({
      inheritedValue: 'Inherited from global: anthropic/claude-3',
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    const inheritedDisplay = container.querySelector('.inherited-display');
    expect(inheritedDisplay).not.toBeNull();

    const inheritedValue = container.querySelector('.inherited-value');
    expect(inheritedValue?.textContent).toBe('Inherited from global: anthropic/claude-3');

    const overrideBtn = container.querySelector('button');
    expect(overrideBtn?.textContent).toBe('Override');

    // Dropdowns should NOT be visible
    expect(container.querySelectorAll('select').length).toBe(0);
  });

  test('does NOT show inherited display when provider is set', () => {
    const props = createBaseProps({
      provider: 'anthropic',
      inheritedValue: 'Inherited from global: anthropic/claude-3',
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    expect(container.querySelector('.inherited-display')).toBeNull();
    expect(container.querySelectorAll('select').length).toBe(2);
  });

  test('does NOT show inherited display when model is set', () => {
    const props = createBaseProps({
      model: 'claude-haiku-4-5',
      inheritedValue: 'Inherited from global: anthropic/claude-3',
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    expect(container.querySelector('.inherited-display')).toBeNull();
    expect(container.querySelectorAll('select').length).toBe(2);
  });
});

// ─── Override flow ───

describe('Override flow', () => {
  test('clicking Override button reveals provider and model dropdowns', () => {
    const props = createBaseProps({
      inheritedValue: 'Inherited from global: anthropic/claude-3',
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    expect(container.querySelector('.inherited-display')).not.toBeNull();
    expect(container.querySelectorAll('select').length).toBe(0);

    act(() => {
      const overrideBtn = container.querySelector('button');
      overrideBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(container.querySelector('.inherited-display')).toBeNull();
    expect(container.querySelectorAll('select').length).toBe(2);
  });

  test('clear override button returns to inherited state', () => {
    const onProviderChange = vi.fn();
    const onModelChange = vi.fn();
    const props = createBaseProps({
      provider: 'anthropic',
      model: 'claude-haiku-4-5',
      inheritedValue: 'Inherited from global: anthropic/claude-3',
      onProviderChange,
      onModelChange,
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    act(() => {
      const clearBtn = container.querySelector('button.form-btn.cancel');
      clearBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(onProviderChange).toHaveBeenCalledWith('');
    expect(onModelChange).toHaveBeenCalledWith('');

    // Re-render with cleared props to simulate parent responding to the callbacks
    const clearedProps = createBaseProps({
      provider: '',
      model: '',
      inheritedValue: 'Inherited from global: anthropic/claude-3',
      onProviderChange,
      onModelChange,
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, clearedProps));
    });

    const inheritedDisplay = container.querySelector('.inherited-display');
    expect(inheritedDisplay).not.toBeNull();
  });

  test('clear override button only shows when inheritedValue exists', () => {
    const props = createBaseProps({
      provider: 'anthropic',
      model: 'claude-haiku-4-5',
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    const clearBtn = container.querySelector('button.form-btn.cancel');
    expect(clearBtn).toBeNull();
  });
});

// ─── Provider/Model dropdown behavior ───

describe('Provider/Model dropdown behavior', () => {
  test('provider dropdown lists all providers', () => {
    const props = createBaseProps({
      provider: 'anthropic',
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    const providerSelect = container.querySelector('select#provider-select-session');
    expect(providerSelect).not.toBeNull();

    const options = providerSelect?.querySelectorAll('option');
    const optionValues = Array.from(options ?? []).map((o) => o.value);
    expect(optionValues).toContain('anthropic');
    expect(optionValues).toContain('openai');
  });

  test('model dropdown lists models for current provider', () => {
    const props = createBaseProps({
      provider: 'anthropic',
      model: 'claude-haiku-4-5',
      models: ['claude-haiku-4-5', 'claude-sonnet-4-6'],
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    const modelSelect = container.querySelector('select#model-select-session');
    expect(modelSelect).not.toBeNull();

    const options = modelSelect?.querySelectorAll('option');
    const optionValues = Array.from(options ?? []).map((o) => o.value);
    expect(optionValues).toContain('claude-haiku-4-5');
    expect(optionValues).toContain('claude-sonnet-4-6');
  });

  test('changing provider calls onProviderChange', () => {
    const onProviderChange = vi.fn();
    const props = createBaseProps({
      provider: 'anthropic',
      onProviderChange,
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    const providerSelect = container.querySelector('select#provider-select-session') as HTMLSelectElement;

    act(() => {
      providerSelect.value = 'openai';
      providerSelect.dispatchEvent(new Event('change', { bubbles: true }));
    });

    expect(onProviderChange).toHaveBeenCalledWith('openai');
  });

  test('changing model calls onModelChange', () => {
    const onModelChange = vi.fn();
    const props = createBaseProps({
      provider: 'anthropic',
      model: 'claude-haiku-4-5',
      onModelChange,
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    const modelSelect = container.querySelector('select#model-select-session') as HTMLSelectElement;

    act(() => {
      modelSelect.value = 'claude-sonnet-4-6';
      modelSelect.dispatchEvent(new Event('change', { bubbles: true }));
    });

    expect(onModelChange).toHaveBeenCalledWith('claude-sonnet-4-6');
  });

  test('model dropdown is disabled when no provider selected', () => {
    const props = createBaseProps({
      provider: '',
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    const modelSelect = container.querySelector('select#model-select-session');
    expect(modelSelect?.disabled).toBe(true);
  });

  test('provider change resets model when current model not available in new provider', () => {
    const onProviderChange = vi.fn();
    const onModelChange = vi.fn();
    const props = createBaseProps({
      provider: 'anthropic',
      model: 'claude-haiku-4-5',
      onProviderChange,
      onModelChange,
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    const providerSelect = container.querySelector('select#provider-select-session') as HTMLSelectElement;

    act(() => {
      providerSelect.value = 'openai';
      providerSelect.dispatchEvent(new Event('change', { bubbles: true }));
    });

    expect(onProviderChange).toHaveBeenCalledWith('openai');
    expect(onModelChange).toHaveBeenCalledWith('');
  });

  test('selecting Default provider clears model', () => {
    const onProviderChange = vi.fn();
    const onModelChange = vi.fn();
    const props = createBaseProps({
      provider: 'anthropic',
      model: 'claude-haiku-4-5',
      onProviderChange,
      onModelChange,
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    const providerSelect = container.querySelector('select#provider-select-session') as HTMLSelectElement;

    act(() => {
      providerSelect.value = '';
      providerSelect.dispatchEvent(new Event('change', { bubbles: true }));
    });

    expect(onProviderChange).toHaveBeenCalledWith('');
    expect(onModelChange).toHaveBeenCalledWith('');
  });

  test('renders fallback option when model is not in models array', () => {
    const props = createBaseProps({
      provider: 'anthropic',
      model: 'some-unknown-model',
      models: ['claude-haiku-4-5', 'claude-sonnet-4-6'],
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    const modelSelect = container.querySelector('select#model-select-session');
    expect(modelSelect).not.toBeNull();

    const options = modelSelect?.querySelectorAll('option');
    const optionValues = Array.from(options ?? []).map((o) => o.value);
    expect(optionValues).toContain('some-unknown-model');
  });
});

// ─── Loading state ───

describe('Loading state', () => {
  test('shows skeleton when loading=true', () => {
    const props = createBaseProps({
      loading: true,
      inheritedValue: 'Inherited from global: anthropic/claude-3',
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    const skeleton = container.querySelector('[data-testid="skeleton"]');
    expect(skeleton).not.toBeNull();

    expect(container.querySelector('.inherited-display')).toBeNull();
    expect(container.querySelectorAll('select').length).toBe(0);
  });
});

// ─── Disabled state ───

describe('Disabled state', () => {
  test('all controls disabled when disabled=true', () => {
    const props = createBaseProps({
      provider: 'anthropic',
      model: 'claude-haiku-4-5',
      disabled: true,
      inheritedValue: 'Inherited from global: anthropic/claude-3',
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    const providerSelect = container.querySelector('select#provider-select-session');
    const modelSelect = container.querySelector('select#model-select-session');
    const clearBtn = container.querySelector('button.form-btn.cancel');

    expect(providerSelect?.disabled).toBe(true);
    expect(modelSelect?.disabled).toBe(true);
    expect(clearBtn?.disabled).toBe(true);
  });

  test('override button is disabled when disabled=true in inherited mode', () => {
    const props = createBaseProps({
      disabled: true,
      inheritedValue: 'Inherited from global: anthropic/claude-3',
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    const overrideBtn = container.querySelector('button');
    expect(overrideBtn?.disabled).toBe(true);
  });
});

// ─── No inherited value (direct dropdown mode) ───

describe('No inherited value (direct dropdown mode)', () => {
  test('always shows dropdowns when no inheritedValue is provided', () => {
    const props = createBaseProps();

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    expect(container.querySelectorAll('select').length).toBe(2);
    expect(container.querySelector('.inherited-display')).toBeNull();
    expect(container.querySelector('button')).toBeNull();
  });
});

// ─── Scope uniqueness ───

describe('Scope uniqueness', () => {
  test('select IDs are scoped', () => {
    const props = createBaseProps({
      scope: 'workspace' as const,
    });

    act(() => {
      root.render(createElement(ProviderModelSubsection, props));
    });

    expect(container.querySelector('select#provider-select-workspace')).not.toBeNull();
    expect(container.querySelector('select#model-select-workspace')).not.toBeNull();
    expect(container.querySelector('select#provider-select-session')).toBeNull();
    expect(container.querySelector('select#model-select-session')).toBeNull();
  });
});
