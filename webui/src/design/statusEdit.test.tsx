/**
 * Status curation tests (SP-140-7 §7d): the structured
 * manifest rewrite model and the ScreenStatusMenu component.
 */

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import ScreenStatusMenu from '../components/design/ScreenStatusMenu';
import { SproutAdapterProvider } from '../contexts/SproutAdapterContext';
import { currentStatusFor, setStatusInManifest } from './statusEdit';

const manifest = `# Design Workspace

frames:
  mobile: 390x844

## Screens

- \`login\` — draft — sign-in entry point
- \`home\` — ready — post-sign-in landing

## Flows

- \`sign-up\` — draft — account creation
`;

describe('statusEdit model', () => {
  it('reads the current status per stem', () => {
    expect(currentStatusFor(manifest, 'login')).toBe('draft');
    expect(currentStatusFor(manifest, 'home')).toBe('ready');
    expect(currentStatusFor(manifest, 'sign-up')).toBe('draft');
    expect(currentStatusFor(manifest, 'nope')).toBe('');
  });

  it('rewrites only the target listing, preserving everything else', () => {
    const { text } = setStatusInManifest(manifest, 'login', 'ready');
    const lines = text.split('\n');
    expect(lines.find((l) => l.includes('`login`'))).toContain('— ready —');
    expect(lines.find((l) => l.includes('`home`'))).toContain('— ready — post-sign-in landing');
    expect(lines.find((l) => l.includes('`sign-up`'))).toBe('- `sign-up` — draft — account creation');
    expect(text).toContain('frames:\n  mobile: 390x844');
    // The summary survives verbatim.
    expect(text).toContain('sign-in entry point');
  });

  it('clearing removes the status segment but keeps the summary', () => {
    const { text } = setStatusInManifest(manifest, 'home', '');
    expect(text).toContain('- `home` — post-sign-in landing');
  });

  it('adding a status to a listing that had none', () => {
    const noStatus = '- `login` — sign-in entry point\n';
    const { text, changed } = setStatusInManifest(noStatus, 'login', 'review');
    expect(changed).toBe(true);
    expect(text).toContain('- `login` — review — sign-in entry point');
  });

  it('an unparsable listing is refused, never rewritten', () => {
    const unparsable = '# just a heading\n\nsome prose without any listing\n';
    const { changed } = setStatusInManifest(unparsable, 'login', 'ready');
    expect(changed).toBe(false);
    const { text } = setStatusInManifest(unparsable, 'login', 'ready');
    expect(text).toBe(unparsable);
  });

  it('matching is case-insensitive on the stem', () => {
    expect(currentStatusFor('- `Login` — draft — x\n', 'LOGIN')).toBe('draft');
  });
});

// ---------------------------------------------------------------------------
// ScreenStatusMenu
// ---------------------------------------------------------------------------

const { transportMock } = vi.hoisted(() => ({ transportMock: vi.fn<typeof fetch>() }));

vi.mock('../contexts/SproutAdapterContext', async (importOriginal) => {
  const actual = (await importOriginal()) as Record<string, unknown>;
  return { ...actual, useSproutFetch: () => transportMock };
});

function textResponse(body: string, status = 200): Response {
  return new Response(body, { status });
}

describe('ScreenStatusMenu', () => {
  beforeEach(() => {
    transportMock.mockReset();
    transportMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'POST') return textResponse(JSON.stringify({ success: true }));
      return textResponse(manifest);
    });
  });

  it('sets a status: read manifest, structured rewrite, safe write', async () => {
    const onSaved = vi.fn();
    render(
      <SproutAdapterProvider>
        <ScreenStatusMenu stem="login" onSaved={onSaved} />
      </SproutAdapterProvider>,
    );
    fireEvent.click(screen.getByTestId('design-status-set-login-ready'));
    await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));

    const post = transportMock.mock.calls.find(([, init]) => init?.method === 'POST');
    expect(post).toBeDefined();
    const body = JSON.parse(String(post?.[1]?.body));
    expect(body.content).toContain('- `login` — ready — sign-in entry point');
    expect(body.content).toContain('- `sign-up` — draft — account creation');
  });

  it('an unparsable manifest opens the editor instead of writing', async () => {
    transportMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'POST') return textResponse(JSON.stringify({ success: true }));
      return textResponse('# prose only, no listings\n');
    });
    const onOpenManifest = vi.fn();
    render(
      <SproutAdapterProvider>
        <ScreenStatusMenu stem="login" onOpenManifest={onOpenManifest} />
      </SproutAdapterProvider>,
    );
    fireEvent.click(screen.getByTestId('design-status-set-login-ready'));
    await waitFor(() => expect(onOpenManifest).toHaveBeenCalledTimes(1));
    expect(transportMock.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(0);
  });

  it('a write failure surfaces the error', async () => {
    transportMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'POST') return new Response('denied', { status: 403 });
      return textResponse(manifest);
    });
    render(
      <SproutAdapterProvider>
        <ScreenStatusMenu stem="login" />
      </SproutAdapterProvider>,
    );
    fireEvent.click(screen.getByTestId('design-status-set-login-ready'));
    expect(await screen.findByTestId('design-status-error-login')).toBeInTheDocument();
  });
});
