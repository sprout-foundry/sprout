/**
 * RoleModelsSection.test.tsx — SP-150 §150d "role models in settings,
 * collapsed by default".
 *
 * Pins:
 *   - The section is collapsed on initial render (the shared Collapsible
 *     <details> has no `open` attribute).
 *   - Clicking the summary expands it (`open` flips to true).
 *   - All five built-in roles render, pre-filled from settings.roles.
 *   - Editing a role's model/provider and clicking Save calls
 *     updateSetting('roles', <full map>) with empty rows dropped.
 *   - Custom (non-built-in) role entries in the persisted map survive every
 *     save untouched, and an emptied built-in row is still dropped.
 *   - The draft re-syncs when the persisted roles map changes by value, and
 *     in-flight typing survives refreshes that carry an unchanged map.
 *   - The section hides entirely when no updateSetting is provided
 *     (mirrors ProviderPrioritySection).
 */

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach, beforeAll, afterAll } from 'vitest';
import type { SproutSettings } from '../../services/api';
import RoleModelsSection from './RoleModelsSection';

let mountPoint: HTMLDivElement;
let root: Root | null = null;

function render(props: { settings: SproutSettings; updateSetting?: (k: string, v: unknown) => Promise<void> }) {
  // Default to a mock only when updateSetting is absent; an explicit
  // `undefined` is meaningful (the section must hide without a mutator).
  const updateSetting = 'updateSetting' in props ? props.updateSetting : vi.fn().mockResolvedValue(undefined);
  // eslint-disable-next-line testing-library/no-unnecessary-act
  act(() => {
    root = createRoot(mountPoint);
    root.render(<RoleModelsSection settings={props.settings} updateSetting={updateSetting} />);
  });
  // Re-render the same root with a new settings object but the SAME
  // updateSetting mock — the config-refresh scenario the draft re-sync
  // tests exercise.
  const rerender = (nextSettings: SproutSettings) => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    act(() => {
      root!.render(<RoleModelsSection settings={nextSettings} updateSetting={updateSetting} />);
    });
  };
  return { updateSetting, rerender };
}

function details(): HTMLDetailsElement {
  const el = document.querySelector('[data-testid="role-models-section"]') as HTMLDetailsElement | null;
  if (!el) throw new Error('role-models-section <details> not found');
  return el;
}

function clickSummary() {
  const summary = details().querySelector('summary')!;
  // The vitest setup mocks Element.prototype.click as a no-op, so dispatch a
  // real bubbling MouseEvent instead — the same technique the sibling
  // settings-tab tests use.
  act(() => {
    summary.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
}

function setInputValue(selector: string, value: string) {
  const input = document.querySelector(selector) as HTMLInputElement;
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!;
  act(() => {
    setter.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

function inputValue(testid: string): string {
  const el = document.querySelector(`[data-testid="${testid}"]`) as HTMLInputElement | null;
  if (!el) throw new Error(`input not found: ${testid}`);
  return el.value;
}

const modelValue = (role: string) => inputValue(`role-model-input-${role}`);
const providerValue = (role: string) => inputValue(`role-provider-input-${role}`);

function clickSelector(selector: string) {
  const el = document.querySelector(selector) as HTMLElement;
  act(() => {
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
}

beforeAll(() => {
  (globalThis as unknown as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as unknown as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  vi.clearAllMocks();
  mountPoint = document.createElement('div');
  document.body.appendChild(mountPoint);
});

afterEach(() => {
  act(() => {
    root?.unmount();
    root = null;
  });
  mountPoint.remove();
});

describe('RoleModelsSection', () => {
  it('is collapsed by default', () => {
    render({ settings: {} as SproutSettings });
    const d = details();
    expect(d).not.toHaveAttribute('open');
    expect(d.open).toBe(false);
  });

  it('expands when the summary is clicked', () => {
    render({ settings: {} as SproutSettings });
    expect(details().open).toBe(false);
    clickSummary();
    expect(details().open).toBe(true);
    // Clicking again collapses it.
    clickSummary();
    expect(details().open).toBe(false);
  });

  it('renders all five built-in roles', () => {
    render({ settings: {} as SproutSettings });
    clickSummary();
    for (const role of ['planner', 'coder', 'summarizer', 'reviewer', 'commit']) {
      expect(document.querySelector(`[data-testid="role-model-row-${role}"]`)).not.toBeNull();
      expect(document.querySelector(`[data-testid="role-model-input-${role}"]`)).not.toBeNull();
      expect(document.querySelector(`[data-testid="role-provider-input-${role}"]`)).not.toBeNull();
      expect(document.querySelector(`[data-testid="role-model-save-${role}"]`)).not.toBeNull();
    }
  });

  it('pre-fills inputs from settings.roles', () => {
    render({
      settings: {
        roles: {
          coder: { provider: 'anthropic', model: 'claude-sonnet-4-6' },
          planner: { model: 'gpt-5' },
        },
      } as SproutSettings,
    });
    clickSummary();
    expect((document.querySelector('[data-testid="role-model-input-coder"]') as HTMLInputElement).value).toBe(
      'claude-sonnet-4-6',
    );
    expect((document.querySelector('[data-testid="role-provider-input-coder"]') as HTMLInputElement).value).toBe(
      'anthropic',
    );
    // planner has only a model; its provider stays empty.
    expect((document.querySelector('[data-testid="role-model-input-planner"]') as HTMLInputElement).value).toBe(
      'gpt-5',
    );
    expect((document.querySelector('[data-testid="role-provider-input-planner"]') as HTMLInputElement).value).toBe('');
    // Untouched roles start empty.
    expect((document.querySelector('[data-testid="role-model-input-reviewer"]') as HTMLInputElement).value).toBe('');
  });

  it('commits the full roles map on save, dropping empty rows', () => {
    const { updateSetting } = render({
      settings: { roles: { coder: { provider: 'anthropic', model: 'old-model' } } } as SproutSettings,
    });
    clickSummary();
    // Change the coder model; the provider stays "anthropic".
    setInputValue('[data-testid="role-model-input-coder"]', 'claude-opus-4-7');
    clickSelector('[data-testid="role-model-save-coder"]');

    expect(updateSetting).toHaveBeenCalledTimes(1);
    expect(updateSetting).toHaveBeenCalledWith('roles', {
      coder: { provider: 'anthropic', model: 'claude-opus-4-7' },
    });
  });

  it('stores nothing when every row is left empty', () => {
    const { updateSetting } = render({ settings: {} as SproutSettings });
    clickSummary();
    // No rows filled in — the committed map should be empty.
    clickSelector('[data-testid="role-model-save-commit"]');
    expect(updateSetting).toHaveBeenCalledWith('roles', {});
  });

  it('trims whitespace and omits empty sub-fields in the committed entry', () => {
    const { updateSetting } = render({ settings: {} as SproutSettings });
    clickSummary();
    setInputValue('[data-testid="role-provider-input-reviewer"]', '  openai  ');
    // Model left empty.
    clickSelector('[data-testid="role-model-save-reviewer"]');
    expect(updateSetting).toHaveBeenCalledWith('roles', {
      reviewer: { provider: 'openai' },
    });
  });

  it('re-syncs the draft when the persisted roles map changes by value', () => {
    const { rerender } = render({
      settings: { roles: { coder: { provider: 'anthropic', model: 'old-model' } } } as SproutSettings,
    });
    clickSummary();
    expect(modelValue('coder')).toBe('old-model');

    // Another surface persists different roles; the next render carries the
    // new map and the inputs must follow it.
    rerender({ roles: { coder: { provider: 'openai', model: 'gpt-5' } } } as SproutSettings);
    expect(modelValue('coder')).toBe('gpt-5');
    expect(providerValue('coder')).toBe('openai');

    // Clearing a role externally resets its row to empty too.
    rerender({ roles: { planner: { model: 'gpt-5' } } } as SproutSettings);
    expect(modelValue('coder')).toBe('');
    expect(providerValue('coder')).toBe('');
    expect(modelValue('planner')).toBe('gpt-5');
  });

  it('keeps in-flight typing on refreshes that carry an unchanged roles map', () => {
    // Two distinct objects with identical contents — the settings surface is
    // re-created on every fetch, so the draft must compare by value.
    const roles = { coder: { provider: 'anthropic', model: 'm1' } };
    const { rerender } = render({ settings: { roles } as SproutSettings });
    clickSummary();

    setInputValue('[data-testid="role-model-input-coder"]', 'typed-but-unsaved');
    // A refresh re-creates the whole settings object with the same value.
    rerender({ roles: { ...roles } } as SproutSettings);
    expect(modelValue('coder')).toBe('typed-but-unsaved');
  });

  it('preserves a custom role entry when saving a built-in row', () => {
    const { updateSetting } = render({
      settings: {
        roles: {
          'my-custom-role': { provider: 'zai', model: 'glm-4' },
          coder: { model: 'old-coder-model' },
        },
      } as SproutSettings,
    });
    clickSummary();
    setInputValue('[data-testid="role-model-input-coder"]', 'new-coder-model');
    clickSelector('[data-testid="role-model-save-coder"]');

    expect(updateSetting).toHaveBeenCalledTimes(1);
    expect(updateSetting).toHaveBeenCalledWith('roles', {
      coder: { model: 'new-coder-model' },
      'my-custom-role': { provider: 'zai', model: 'glm-4' },
    });
  });

  it('preserves custom roles even when the saved built-in row is emptied', () => {
    const { updateSetting } = render({
      settings: {
        roles: {
          'team-role': { model: 'kept' },
          coder: { model: 'dropped' },
        },
      } as SproutSettings,
    });
    clickSummary();
    // Clear the coder row entirely, then save: the empty built-in row is
    // dropped, but the custom entry must survive.
    setInputValue('[data-testid="role-model-input-coder"]', '');
    clickSelector('[data-testid="role-model-save-coder"]');
    expect(updateSetting).toHaveBeenCalledWith('roles', {
      'team-role': { model: 'kept' },
    });
  });

  it('renders nothing when no updateSetting is provided', () => {
    // Render directly (not via the render helper) so the assertion is
    // unambiguous: the component receives updateSetting === undefined, so it
    // must hide entirely (no way to persist a change).
    // eslint-disable-next-line testing-library/no-unnecessary-act
    act(() => {
      root = createRoot(mountPoint);
      root.render(<RoleModelsSection settings={{} as SproutSettings} updateSetting={undefined} />);
    });
    expect(document.querySelector('[data-testid="role-models-section"]')).toBeNull();
  });
});
