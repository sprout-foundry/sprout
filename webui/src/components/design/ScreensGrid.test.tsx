/**
 * SP-140-3 item 3.7 — Screens tab.
 *
 * Covers the §3c contract: the grid renders screen cards from the inventory,
 * status chips derive from the README `status:` markers, device-frame-aware
 * sizing comes from the README `frames:` declarations, a click opens
 * `LivePreview` in the detail pane with the screen's content/language/fileName,
 * and `LivePreview`'s `onContentChange` reaches the designApi write-back.
 *
 * `LivePreview` is mocked: its own split editor/renderer is item-3.x surface
 * already covered by its component tests, and mocking it lets the assertions
 * read the props it receives. `designApi` is mocked for the network/file-read
 * paths (`readAsset`/`writeAsset`) while keeping the real pure parsers
 * (`buildInventory`/`parseFrames`/`parseManifestStatuses`) in play, per the
 * FlowsCanvas.test convention.
 */

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { SproutAdapterProvider } from '../../contexts/SproutAdapterContext';
import {
  buildInventory,
  parseFrames,
  parseManifestStatuses,
  readAsset,
  writeAsset,
} from '../../services/api/designApi';
import type { FilesResponse } from '../../services/api/types';
import ScreensGrid, {
  DEFAULT_CARD_ASPECT,
  ScreensTabContainer,
  cardAspectStyle,
  designRelativePath,
  frameForScreen,
  framesOf,
  languageForScreen,
  screenCards,
  screenStem,
  statusOf,
  type ScreenCard,
} from './ScreensGrid';

vi.mock('../../services/api/designApi', async () => {
  const actual = await vi.importActual<Record<string, unknown>>('../../services/api/designApi');
  return { ...actual, readAsset: vi.fn(), writeAsset: vi.fn() };
});

vi.mock('../LivePreview', () => ({
  default: ({
    content,
    language,
    fileName,
    onContentChange,
  }: {
    content: string;
    language: string;
    fileName: string;
    onContentChange?: (content: string) => void;
  }) => (
    <div data-testid="live-preview-mock" data-language={language} data-file-name={fileName} data-content={content}>
      <textarea
        data-testid="live-preview-editor"
        value={content}
        onChange={(event) => onContentChange?.(event.target.value)}
      />
    </div>
  ),
}));

const mockedReadAsset = vi.mocked(readAsset);
const mockedWriteAsset = vi.mocked(writeAsset);

const README = [
  '# Design Workspace',
  '',
  '```',
  'frames:',
  '  desktop: 1440x900',
  '  mobile: 390x844',
  '```',
  '',
  '## Screens',
  '',
  '```',
  '- `login` — draft — sign-in entry point',
  '- `inbox` — ready — message list',
  '```',
].join('\n');

const LOGIN_HTML = '<!doctype html><html><body><h1>Login</h1></body></html>';
const INBOX_HTML = '<!doctype html><html><body><h1>Inbox</h1></body></html>';
const DASH_SVG = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1440 900"><rect/></svg>';

/**
 * The workspace file list as `/api/files` returns it. Paths are
 * workspace-relative — `buildInventory` keeps the `design/` prefix on the
 * entry path (it only classifies by the design/ segment).
 */
function filesResponse(): FilesResponse {
  const paths = [
    'design/README.md',
    'design/wireframes/dash.svg',
    'design/wireframes/login.svg',
    'design/screens/login.html',
    'design/screens/inbox.html',
  ];
  return { files: paths.map((path) => ({ path, size: 0, modified: 0 })) } as FilesResponse;
}

/** The declared frames the fixture README carries (desktop + mobile). */
const FRAMES = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'mobile', width: 390, height: 844 },
];

/** Inventory for the fixture tree: README frames + the README status markers. */
function fixtureInventory(): ReturnType<typeof buildInventory> {
  const inventory = buildInventory(filesResponse());
  const statuses = parseManifestStatuses(README);
  return {
    ...inventory,
    manifest: { ...inventory.manifest, chars: README.length, frames: parseFrames(README) },
    screens: inventory.screens.map((entry) => {
      const status = statuses[screenStem(entry)] ?? '';
      return status ? { ...entry, status } : entry;
    }),
  };
}

/** Fixture content keyed by the workspace-relative inventory path. */
const CONTENT: Record<string, string> = {
  'design/screens/login.html': LOGIN_HTML,
  'design/screens/inbox.html': INBOX_HTML,
  'design/wireframes/dash.svg': DASH_SVG,
};

function renderGrid(props: Partial<React.ComponentProps<typeof ScreensGrid>> = {}) {
  return render(
    <SproutAdapterProvider>
      <ScreensGrid inventory={fixtureInventory()} contentByPath={CONTENT} {...props} />
    </SproutAdapterProvider>,
  );
}

describe('ScreensGrid cards', () => {
  it('renders a thumbnail card per screen from the inventory', () => {
    renderGrid();

    expect(screen.getByTestId('design-screens-grid').getAttribute('data-screen-count')).toBe('3');
    // One card per screen plus a wireframe-only card for `dash` (no screen).
    expect(screen.getByTestId('design-screen-card-login')).toBeTruthy();
    expect(screen.getByTestId('design-screen-card-inbox')).toBeTruthy();
    expect(screen.getByTestId('design-screen-card-dash')).toBeTruthy();
    expect(screen.getByTestId('design-screen-card-dash').getAttribute('data-kind')).toBe('wireframe');
    expect(screen.getByTestId('design-screen-card-login').getAttribute('data-kind')).toBe('screen');
  });

  it('shows the screen name and a thumbnail per card', () => {
    renderGrid();

    expect(screen.getByText('login')).toBeTruthy();
    expect(screen.getByText('inbox')).toBeTruthy();
    // An HTML screen cannot be an <img> (the browser refuses to decode it, so
    // the thumb was always the empty placeholder): screens render in a
    // sandboxed iframe of the read HTML; wireframes (SVG) stay <img>s.
    const loginThumb = screen.getByTestId('design-screen-thumb-login');
    expect(loginThumb.tagName).toBe('IFRAME');
    expect(loginThumb.getAttribute('sandbox')).toBe('');
    expect(loginThumb.getAttribute('srcdoc')).toContain('<!doctype html>');
  });

  it('does not duplicate a wireframe that already has a screen', () => {
    renderGrid();
    // `login` exists as both a wireframe and a screen: one card, kind=screen.
    expect(screen.getAllByTestId(/^design-screen-card-login$/)).toHaveLength(1);
    expect(screen.getByTestId('design-screen-card-login').getAttribute('data-kind')).toBe('screen');
  });

  it('renders the empty state for an inventory without design/', () => {
    render(
      <SproutAdapterProvider>
        <ScreensGrid inventory={{ ...fixtureInventory(), exists: false }} contentByPath={CONTENT} />
      </SproutAdapterProvider>,
    );

    expect(screen.getByTestId('design-screens-grid').getAttribute('data-inventory')).toBe('missing');
    expect(screen.getByText("Couldn't load the design inventory.")).toBeTruthy();
  });
});

describe('ScreensGrid status chips', () => {
  it('renders a status chip for a screen listed in the README', () => {
    renderGrid();

    // The chip text is the README status marker.
    expect(screen.getByTestId('design-screen-status-inbox').textContent).toBe('ready');
    expect(screen.getByTestId('design-screen-status-login').textContent).toBe('draft');
    expect(screen.getByTestId('design-screen-card-inbox').getAttribute('data-status')).toBe('ready');
  });

  it('omits the chip for a screen the README does not list', () => {
    // The wireframe-only card (`dash`) is unlisted, so it carries no status.
    renderGrid();
    expect(screen.queryByTestId('design-screen-status-dash')).toBeNull();
    expect(screen.getByTestId('design-screen-card-dash').getAttribute('data-status')).toBe('');
  });

  it('derives the chips from the README status markers', () => {
    const inventory = fixtureInventory();
    const statuses = parseManifestStatuses(README);
    expect(statuses).toEqual({ login: 'draft', inbox: 'ready' });
    expect(inventory.screens.find((s) => s.name === 'inbox.html')?.status).toBe('ready');
    expect(inventory.screens.find((s) => s.name === 'login.html')?.status).toBe('draft');
  });

  it('carries only the SP-140 status markers', () => {
    renderGrid();
    for (const name of ['login', 'inbox']) {
      const chip = screen.getByTestId(`design-screen-status-${name}`).textContent ?? '';
      expect(['draft', 'review', 'ready']).toContain(chip);
    }
  });
});

describe('ScreensGrid device-frame-aware sizing', () => {
  it("sizes a card matching a declared frame to that frame's aspect ratio", () => {
    // The fixture README declares a `login` frame, matching the screen name.
    const inventory = fixtureInventory();
    renderGrid({
      inventory: {
        ...inventory,
        manifest: { ...inventory.manifest, frames: [{ name: 'login', width: 390, height: 844 }, ...FRAMES] },
      },
    });

    const login = screen.getByTestId('design-screen-card-login');
    expect(login.getAttribute('data-frame')).toBe('login');
    expect((screen.getByTestId('design-screen-thumb-box-login') as HTMLElement).style.aspectRatio).toBe('390 / 844');
    expect(screen.getByTestId('design-screen-frame-login').textContent).toContain('390×844');

    // `inbox` matches no declared frame: neutral ratio, no frame line.
    const inbox = screen.getByTestId('design-screen-card-inbox');
    expect(inbox.getAttribute('data-frame')).toBe('');
    expect((screen.getByTestId('design-screen-thumb-box-inbox') as HTMLElement).style.aspectRatio).toBe(
      DEFAULT_CARD_ASPECT,
    );
    expect(screen.queryByTestId('design-screen-frame-inbox')).toBeNull();
  });

  it('matches frames against the screen name, not the file extension', () => {
    expect(frameForScreen({ path: 'design/screens/desktop.html', name: 'desktop.html' }, FRAMES)?.width).toBe(1440);
    expect(frameForScreen({ path: 'design/screens/inbox.html', name: 'inbox.html' }, FRAMES)).toBeNull();
  });

  it('sizes every screen with the neutral ratio when no frames are declared', () => {
    const inventory = buildInventory(filesResponse());
    renderGrid({ inventory });

    expect(screen.getByTestId('design-screen-card-login').getAttribute('data-frame')).toBe('');
    expect((screen.getByTestId('design-screen-thumb-box-login') as HTMLElement).style.aspectRatio).toBe(
      DEFAULT_CARD_ASPECT,
    );
  });

  it('reads the declared frames off the inventory manifest', () => {
    expect(framesOf(fixtureInventory()).map((frame) => frame.name)).toEqual(['desktop', 'mobile']);
    expect(framesOf(null)).toEqual([]);
  });
});

describe('ScreensGrid → LivePreview detail pane', () => {
  it("opens LivePreview with the screen's content, language, and file name on click", () => {
    renderGrid();
    expect(screen.queryByTestId('live-preview-mock')).toBeNull();

    fireEvent.click(screen.getByTestId('design-screen-card-login'));

    const preview = screen.getByTestId('live-preview-mock');
    expect(preview.getAttribute('data-content')).toBe(LOGIN_HTML);
    expect(preview.getAttribute('data-language')).toBe('html');
    expect(preview.getAttribute('data-file-name')).toBe('design/screens/login.html');
    expect(screen.getByTestId('design-screens-grid').getAttribute('data-screen-selected')).toBe(
      'design/screens/login.html',
    );
  });

  it('opens a wireframe card as SVG content', () => {
    renderGrid();
    fireEvent.click(screen.getByTestId('design-screen-card-dash'));

    const preview = screen.getByTestId('live-preview-mock');
    expect(preview.getAttribute('data-language')).toBe('svg');
    expect(preview.getAttribute('data-file-name')).toBe('design/wireframes/dash.svg');
    expect(preview.getAttribute('data-content')).toBe(DASH_SVG);
  });

  it('reports the selection through the shared tab props', () => {
    const onSelectAsset = vi.fn();
    renderGrid({ onSelectAsset });

    fireEvent.click(screen.getByTestId('design-screen-card-inbox'));
    expect(onSelectAsset).toHaveBeenCalledWith('design/screens/inbox.html');
  });

  it('keeps a placeholder until a card is selected', () => {
    renderGrid();
    expect(screen.getByTestId('design-screen-placeholder')).toBeTruthy();
  });

  it('reads the screen text through the designApi read path when no content prop is given', async () => {
    mockedReadAsset.mockImplementation(async (_fetchFn, path) => CONTENT[`design/${path}`] ?? '');

    render(
      <SproutAdapterProvider>
        <ScreensGrid inventory={fixtureInventory()} />
      </SproutAdapterProvider>,
    );

    await waitFor(() => expect(mockedReadAsset).toHaveBeenCalled());
    await screen.findByTestId('design-screen-thumb-login');
    // Reads go through the design/ path designApi normalises.
    expect(mockedReadAsset.mock.calls.map((call) => call[1])).toEqual(
      expect.arrayContaining(['screens/login.html', 'screens/inbox.html', 'wireframes/dash.svg']),
    );

    fireEvent.click(screen.getByTestId('design-screen-card-inbox'));
    expect(screen.getByTestId('live-preview-mock').getAttribute('data-content')).toBe(INBOX_HTML);
  });
});

describe('ScreensGrid edit write-back', () => {
  it('routes LivePreview onContentChange to the edit handler', () => {
    const onEditAsset = vi.fn();
    renderGrid({ onEditAsset });

    fireEvent.click(screen.getByTestId('design-screen-card-login'));
    fireEvent.change(screen.getByTestId('live-preview-editor'), { target: { value: '<html>edited</html>' } });

    expect(onEditAsset).toHaveBeenCalledWith('design/screens/login.html', '<html>edited</html>');
  });

  it('writes the edited screen through designApi.writeAsset via the container', async () => {
    mockedReadAsset.mockImplementation(async (_fetchFn, path) => CONTENT[`design/${path}`] ?? '');
    mockedWriteAsset.mockResolvedValue({ path: 'design/screens/login.html' } as never);

    render(
      <SproutAdapterProvider>
        <ScreensTabContainer inventory={fixtureInventory()} />
      </SproutAdapterProvider>,
    );

    await screen.findByTestId('design-screen-card-login');
    fireEvent.click(screen.getByTestId('design-screen-card-login'));
    fireEvent.change(screen.getByTestId('live-preview-editor'), { target: { value: '<html>edited</html>' } });

    await waitFor(() =>
      expect(mockedWriteAsset).toHaveBeenCalledWith(
        expect.any(Function),
        'design/screens/login.html',
        '<html>edited</html>',
        expect.any(Function),
      ),
    );
  });

  it('keeps the edited text on screen when the write fails', async () => {
    mockedReadAsset.mockImplementation(async (_fetchFn, path) => CONTENT[`design/${path}`] ?? '');
    // The container swallows the rejected write, so nothing reaches the grid's
    // error line — the edited text must stay on screen regardless.
    mockedWriteAsset.mockRejectedValue(new Error('offline'));

    render(
      <SproutAdapterProvider>
        <ScreensTabContainer inventory={fixtureInventory()} />
      </SproutAdapterProvider>,
    );

    await screen.findByTestId('design-screen-card-login');
    fireEvent.click(screen.getByTestId('design-screen-card-login'));
    fireEvent.change(screen.getByTestId('live-preview-editor'), { target: { value: '<html>edited</html>' } });

    await waitFor(() => expect(mockedWriteAsset).toHaveBeenCalled());
    expect(screen.getByTestId('live-preview-mock').getAttribute('data-content')).toBe('<html>edited</html>');
  });

  it('surfaces a synchronous write failure on the grid', async () => {
    const onEditAsset = vi.fn(() => {
      throw new Error('offline');
    });
    renderGrid({ onEditAsset });

    fireEvent.click(screen.getByTestId('design-screen-card-login'));
    fireEvent.change(screen.getByTestId('live-preview-editor'), { target: { value: '<html>edited</html>' } });

    expect(screen.getByTestId('design-screen-error').textContent).toBe('Could not write design/screens/login.html.');
    // The preview stays mounted and controlled: the failed write leaves it on
    // the file's last committed content rather than unmounting the editor.
    expect(screen.getByTestId('live-preview-mock')).toBeTruthy();
  });

  it('renders the preview read-only when no edit handler is supplied', () => {
    renderGrid();
    fireEvent.click(screen.getByTestId('design-screen-card-login'));
    fireEvent.change(screen.getByTestId('live-preview-editor'), { target: { value: '<html>edited</html>' } });

    // No handler: the controlled content never leaves the read value.
    expect(screen.getByTestId('live-preview-mock').getAttribute('data-content')).toBe(LOGIN_HTML);
  });
});

describe('ScreensGrid helpers', () => {
  it('derives the screen stem and language from the asset path', () => {
    expect(screenStem({ name: 'login.html', path: 'design/screens/login.html' })).toBe('login');
    expect(screenStem({ name: 'dash.svg', path: 'design/wireframes/dash.svg' })).toBe('dash');
    expect(languageForScreen('design/screens/login.html')).toBe('html');
    expect(languageForScreen('design/wireframes/login.svg')).toBe('svg');
  });

  it('normalises a path for the designApi read/write helpers', () => {
    expect(designRelativePath('design/screens/login.html')).toBe('screens/login.html');
    expect(designRelativePath('./design/screens/login.html')).toBe('screens/login.html');
    expect(designRelativePath('screens/login.html')).toBe('screens/login.html');
  });

  it('builds frame-aware sizing styles', () => {
    expect(cardAspectStyle({ name: 'mobile', width: 390, height: 844 })).toEqual({ aspectRatio: '390 / 844' });
    expect(cardAspectStyle(null)).toEqual({ aspectRatio: DEFAULT_CARD_ASPECT });
  });

  it('collects cards from screens plus unmatched wireframes', () => {
    const inventory = fixtureInventory();
    const cards: ScreenCard[] = screenCards(inventory.screens, inventory.wireframes, FRAMES);
    expect(cards.map((card) => card.name)).toEqual(['inbox', 'login', 'dash']);
    expect(cards.find((card) => card.name === 'dash')?.kind).toBe('wireframe');
    // `dash` is a wireframe, but the fixture README declares no `dash` frame.
    expect(cards.find((card) => card.name === 'dash')?.frame).toBeNull();
    expect(
      screenCards([], [{ name: 'desktop.svg', path: 'design/wireframes/desktop.svg' }], FRAMES)[0].frame?.name,
    ).toBe('desktop');
  });

  it('reads a status off an inventory entry', () => {
    expect(statusOf({ name: 'inbox.html', path: 'design/screens/inbox.html', status: 'ready' })).toBe('ready');
    expect(statusOf({ name: 'x.html', path: 'design/screens/x.html' })).toBe('');
  });
});
