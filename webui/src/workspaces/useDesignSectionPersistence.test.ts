/**
 * Design-section persistence (SP-140-8 item 8.5).
 *
 * The Design mode's active section survives a reload: the store, the key
 * scoping, and the reload path (a fresh mount reads back what the previous
 * mount wrote). Best-effort by design — corrupt storage and storage failures
 * degrade to the default section, never throw.
 */

import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { DESIGN_SECTION_STORAGE_KEY, INSTANCE_PID_STORAGE_KEY } from '../constants/app';
import {
  DEFAULT_DESIGN_SECTION,
  designSectionStorageKey,
  persistDesignSection,
  readPersistedDesignSection,
  useDesignSectionPersistence,
} from './useDesignSectionPersistence';

const key = designSectionStorageKey();

beforeEach(() => {
  window.localStorage.removeItem(key);
  window.localStorage.removeItem(INSTANCE_PID_STORAGE_KEY);
});

afterEach(() => {
  window.localStorage.removeItem(key);
  window.localStorage.removeItem(INSTANCE_PID_STORAGE_KEY);
});

describe('designSectionStorageKey', () => {
  it('is scoped per instance + UI context like the workspace-mode key', () => {
    window.localStorage.setItem(INSTANCE_PID_STORAGE_KEY, 'pid-42');
    try {
      expect(designSectionStorageKey()).toBe(`${DESIGN_SECTION_STORAGE_KEY}:pid-42:local`);
    } finally {
      window.localStorage.removeItem(INSTANCE_PID_STORAGE_KEY);
    }
  });

  it('falls back to the default pid when no instance pin exists', () => {
    expect(designSectionStorageKey()).toBe(`${DESIGN_SECTION_STORAGE_KEY}:default:local`);
  });
});

describe('readPersistedDesignSection / persistDesignSection', () => {
  it('round-trips a section id', () => {
    expect(readPersistedDesignSection()).toBeNull();
    persistDesignSection('tokens');
    expect(readPersistedDesignSection()).toBe('tokens');
  });

  it('tolerates corrupt storage (degrades to null, never throws)', () => {
    window.localStorage.setItem(key, '{not json');
    expect(readPersistedDesignSection()).toBeNull();
  });

  it('rejects values that are no longer real section ids', () => {
    window.localStorage.setItem(key, JSON.stringify('feedback'));
    expect(readPersistedDesignSection()).toBeNull();
  });

  it('swallows write failures (a throwing store must not break the caller)', () => {
    const original = window.localStorage.setItem;
    window.localStorage.setItem = () => {
      throw new Error('QuotaExceededError');
    };
    try {
      expect(() => persistDesignSection('screens')).not.toThrow();
    } finally {
      window.localStorage.setItem = original;
    }
  });
});

describe('useDesignSectionPersistence', () => {
  it('mounts on the persisted section, not the default', () => {
    persistDesignSection('tokens');
    const { result } = renderHook(() => useDesignSectionPersistence());
    expect(result.current.section).toBe('tokens');
  });

  it('mounts on the default when nothing is persisted', () => {
    const { result } = renderHook(() => useDesignSectionPersistence());
    expect(result.current.section).toBe(DEFAULT_DESIGN_SECTION);
  });

  it('persists on every set, so a remount (reload simulation) restores it', () => {
    const first = renderHook(() => useDesignSectionPersistence());
    act(() => first.result.current.setSection('screens'));
    expect(first.result.current.section).toBe('screens');
    expect(readPersistedDesignSection()).toBe('screens');

    // The reload: a second, independent mount reads the store back.
    const second = renderHook(() => useDesignSectionPersistence());
    expect(second.result.current.section).toBe('screens');
  });

  it('keeps instances isolated: another instance pid reads its own section', () => {
    const first = renderHook(() => useDesignSectionPersistence());
    act(() => first.result.current.setSection('screens'));

    window.localStorage.setItem(INSTANCE_PID_STORAGE_KEY, 'pid-9');
    try {
      const other = renderHook(() => useDesignSectionPersistence());
      expect(other.result.current.section).toBe(DEFAULT_DESIGN_SECTION);
    } finally {
      window.localStorage.removeItem(INSTANCE_PID_STORAGE_KEY);
    }
  });
});
