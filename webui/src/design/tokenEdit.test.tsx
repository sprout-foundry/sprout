/**
 * Token editing tests (SP-140-7 §7c): the surgical DTCG model
 * and the TokenValueEditor component.
 */

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import TokenValueEditor from '../components/design/TokenValueEditor';
import { SproutAdapterProvider } from '../contexts/SproutAdapterContext';
import { applyTokenEdit, coerceTokenValue, referenceCount, setTokenValue } from './tokenEdit';

const doc = `{
  "color": {
    "brand": {
      "primary": { "$type": "color", "$value": "#0055ff" }
    }
  },
  "button": {
    "border": { "$type": "color", "$value": "{color.brand.primary}" }
  }
}`;

describe('tokenEdit model', () => {
  it('edits exactly one leaf, byte-minimally', () => {
    const next = applyTokenEdit(doc, 'color.brand.primary', '#ff0000');
    const parsed = JSON.parse(next) as { color: { brand: { primary: { $value: string } } } };
    expect(parsed.color.brand.primary.$value).toBe('#ff0000');
    // Everything else is untouched.
    expect(parsed.button.border.$value).toBe('{color.brand.primary}');
    // The edit is surgical: only the one leaf changed.
    const before = JSON.parse(doc);
    expect(Object.keys(parsed)).toEqual(Object.keys(before));
  });

  it('canonical serialization with trailing newline', () => {
    const next = applyTokenEdit(doc, 'color.brand.primary', '#ff0000');
    expect(next.endsWith('\n')).toBe(true);
    expect(next).toBe(JSON.stringify(JSON.parse(next), null, 2) + '\n');
  });

  it('refuses to cross a non-object mid-path (no data destruction)', () => {
    const hostile = { color: { brand: 'not an object' } };
    expect(() => setTokenValue(hostile, 'color.brand.primary.$value', 'x')).toThrow(/non-object/);
  });

  it('a non-DTCG file throws instead of being clobbered', () => {
    expect(() => applyTokenEdit('not json', 'a.b', 'x')).toThrow();
  });

  it('coerces numbers for numeric types, preserves unit spelling', () => {
    expect(coerceTokenValue('number', '16')).toEqual({ value: 16, plausible: true });
    expect(coerceTokenValue('dimension', '16px')).toEqual({ value: '16px', plausible: true });
    expect(coerceTokenValue('duration', '120ms')).toEqual({ value: '120ms', plausible: true });
    expect(coerceTokenValue('dimension', 'abc')).toEqual({ value: 'abc', plausible: false });
  });

  it('color plausibility and alias pass-through', () => {
    expect(coerceTokenValue('color', '#ff0000').plausible).toBe(true);
    expect(coerceTokenValue('color', 'nope').plausible).toBe(false);
    expect(coerceTokenValue('color', '{color.brand.primary}').plausible).toBe(true);
  });

  it('reference counts come from the tokenRefs map', () => {
    expect(referenceCount({ 'color.brand.primary': 2 }, 'color.brand.primary')).toBe(2);
    expect(referenceCount(undefined, 'color.brand.primary')).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// TokenValueEditor
// ---------------------------------------------------------------------------

const { transportMock } = vi.hoisted(() => ({ transportMock: vi.fn<typeof fetch>() }));

vi.mock('../contexts/SproutAdapterContext', async (importOriginal) => {
  const actual = (await importOriginal()) as Record<string, unknown>;
  return { ...actual, useSproutFetch: () => transportMock };
});

function textResponse(body: string, status = 200): Response {
  return new Response(body, { status });
}

describe('TokenValueEditor', () => {
  beforeEach(() => {
    transportMock.mockReset();
    transportMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'POST') return textResponse(JSON.stringify({ success: true }));
      return textResponse(doc);
    });
  });

  function renderEditor(overrides: Partial<Parameters<typeof TokenValueEditor>[0]> = {}) {
    return render(
      <SproutAdapterProvider>
        <TokenValueEditor
          tokenPath="color.brand.primary"
          filePath="design/tokens/color.tokens.json"
          type="color"
          currentText="#0055ff"
          {...overrides}
        />
      </SproutAdapterProvider>,
    );
  }

  it('saves a surgical edit: read file, one-leaf write through the seam', async () => {
    const onSaved = vi.fn();
    renderEditor({ onSaved });
    await act(async () => {
      fireEvent.change(screen.getByTestId('design-token-input'), { target: { value: '#ff0000' } });
    });
    fireEvent.click(screen.getByTestId('design-token-save'));
    await screen.findByTestId('design-token-saved');

    const [getUrl, getInit] = transportMock.mock.calls[0];
    expect(String(getUrl)).toContain('design%2Ftokens%2Fcolor.tokens.json');
    expect(getInit?.method ?? 'GET').toBe('GET');

    const post = transportMock.mock.calls.find(([, init]) => init?.method === 'POST');
    expect(post).toBeDefined();
    const body = JSON.parse(String(post?.[1]?.body));
    expect(body.content).toContain('"$value": "#ff0000"');
    expect(body.content).toContain('{color.brand.primary}');
    expect(onSaved).toHaveBeenCalledTimes(1);
  });

  it('warns with the reference count when the token is aliased', () => {
    renderEditor({ tokenRefs: { 'color.brand.primary': 2 } });
    expect(screen.getByTestId('design-token-refs')).toHaveTextContent('2 tokens reference');
  });

  it('an implausible color value disables save and warns', () => {
    renderEditor();
    fireEvent.change(screen.getByTestId('design-token-input'), { target: { value: 'nope' } });
    expect(screen.getByTestId('design-token-warn')).toBeInTheDocument();
    expect(screen.getByTestId('design-token-save')).toBeDisabled();
  });

  it('a §7a conflict surfaces the reload-and-retry message', async () => {
    transportMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'POST') {
        return new Response(JSON.stringify({ error: 'revision_conflict', path: 'design/tokens/color.tokens.json' }), {
          status: 409,
        });
      }
      return textResponse(doc);
    });
    renderEditor();
    fireEvent.change(screen.getByTestId('design-token-input'), { target: { value: '#ff0000' } });
    fireEvent.click(screen.getByTestId('design-token-save'));
    expect(await screen.findByTestId('design-token-conflict')).toBeInTheDocument();
  });

  it('an unchanged draft keeps save disabled', () => {
    renderEditor();
    expect(screen.getByTestId('design-token-save')).toBeDisabled();
  });
});
