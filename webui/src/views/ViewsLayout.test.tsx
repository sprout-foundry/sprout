/**
 * ViewsLayout + resolveViewsArrangement.
 *
 * Pins the layout-configuration contract: per-slot merge semantics against
 * the default arrangement, the fail-fast throw on an unknown view kind,
 * slot placement, per-view props passthrough, and that an
 * embedding-supplied arrangement is honored wholesale.
 *
 * The composite views (ChatView, AgentChangesPanel, PreviewPanel) are
 * mocked to stubs that render a distinct testid and echo a received prop —
 * they need the webui context stack, which these tests do not provide (and
 * which is not the unit under test). FileTree is stubbed within a partial
 * @sprout/ui mock for the same reason (it fetches a workspace tree when
 * rendered bare). PreviewPane is NOT mocked: it is presentational and
 * renders without providers, so one case pins a real view in a slot.
 */

import { render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

vi.mock('../components/ChatView', () => ({
  default: (props: { inputValue?: string }) => <div data-testid="views-test-chat">input={props.inputValue ?? ''}</div>,
}));
vi.mock('../components/AgentChangesPanel', () => ({ default: () => <div data-testid="views-test-changes" /> }));
vi.mock('../components/PreviewPanel', () => ({
  PreviewPanel: (props: { open?: boolean }) => (
    <div data-testid="views-test-preview-panel" data-open={String(!!props.open)} />
  ),
}));
// Partial mock: FileTree stubbed (it fetches when rendered bare), the rest
// of the barrel — notably the real PreviewPane — untouched.
vi.mock('@sprout/ui', async (importOriginal) => {
  const actual = (await importOriginal()) as Record<string, unknown>;
  return {
    ...actual,
    FileTree: () => <div data-testid="views-test-file-tree" />,
  };
});

import { DEFAULT_VIEWS_ARRANGEMENT, SLOT_ORDER, ViewsLayout, resolveViewsArrangement } from './ViewsLayout';

describe('resolveViewsArrangement', () => {
  it('returns the default arrangement when nothing is supplied', () => {
    expect(resolveViewsArrangement()).toEqual(DEFAULT_VIEWS_ARRANGEMENT);
  });

  it('keeps the default for omitted slots', () => {
    const resolved = resolveViewsArrangement({ center: ['editor'] });
    expect(resolved.left).toEqual(DEFAULT_VIEWS_ARRANGEMENT.left);
    expect(resolved.right).toEqual(DEFAULT_VIEWS_ARRANGEMENT.right);
    expect(resolved.overlay).toEqual(DEFAULT_VIEWS_ARRANGEMENT.overlay);
    expect(resolved.center).toEqual(['editor']);
  });

  it('replaces a provided slot and honors an explicit empty one', () => {
    const resolved = resolveViewsArrangement({ left: [], overlay: ['chat'] });
    expect(resolved.left).toEqual([]);
    expect(resolved.overlay).toEqual(['chat']);
    expect(resolved.center).toEqual(DEFAULT_VIEWS_ARRANGEMENT.center);
  });

  it('throws a TypeError naming an unknown kind and listing the valid ones', () => {
    expect(() => resolveViewsArrangement({ center: ['chaat'] })).toThrow(TypeError);
    expect(() => resolveViewsArrangement({ center: ['chaat'] })).toThrow(/chaat/);
    expect(() => resolveViewsArrangement({ center: ['chaat'] })).toThrow(/chat/);
  });

  it('returns a fresh object — mutating the result does not touch the default', () => {
    const resolved = resolveViewsArrangement();
    resolved.center?.push('editor');
    resolved.left?.pop();
    expect(DEFAULT_VIEWS_ARRANGEMENT.center).toEqual(['chat']);
    expect(DEFAULT_VIEWS_ARRANGEMENT.left).toEqual(['fileTree']);
    expect(resolveViewsArrangement()).toEqual(DEFAULT_VIEWS_ARRANGEMENT);
  });
});

describe('ViewsLayout', () => {
  it('renders the default arrangement: every view stub in its slot', () => {
    render(<ViewsLayout />);
    expect(screen.getByTestId('views-layout')).toBeInTheDocument();
    for (const slot of SLOT_ORDER) {
      expect(screen.getByTestId(`views-slot-${slot}`)).toBeInTheDocument();
    }
    expect(within(screen.getByTestId('views-slot-left')).getByTestId('views-test-file-tree')).toBeInTheDocument();
    expect(within(screen.getByTestId('views-slot-center')).getByTestId('views-test-chat')).toBeInTheDocument();
    expect(within(screen.getByTestId('views-slot-right')).getByTestId('views-test-changes')).toBeInTheDocument();
    expect(
      within(screen.getByTestId('views-slot-overlay')).getByTestId('views-test-preview-panel'),
    ).toBeInTheDocument();
  });

  it('passes per-view props through to the views', () => {
    render(<ViewsLayout props={{ chat: { inputValue: 'hello from the host' } }} />);
    expect(screen.getByTestId('views-test-chat')).toHaveTextContent('input=hello from the host');
  });

  it('honors an embedding-supplied arrangement wholesale', () => {
    render(<ViewsLayout arrangement={{ left: [], center: ['changes', 'chat'], overlay: [] }} />);
    const center = screen.getByTestId('views-slot-center');
    expect(within(center).getByTestId('views-test-changes')).toBeInTheDocument();
    expect(within(center).getByTestId('views-test-chat')).toBeInTheDocument();
    // Replaced slots: the emptied left/overlay slots drop the default's
    // fileTree and preview panel entirely.
    expect(screen.queryByTestId('views-test-file-tree')).not.toBeInTheDocument();
    expect(screen.queryByTestId('views-test-preview-panel')).not.toBeInTheDocument();
  });

  it('applies a supplied className to the root', () => {
    render(<ViewsLayout className="host-shell" />);
    expect(screen.getByTestId('views-layout')).toHaveClass('sprout-views-layout', 'host-shell');
  });

  it('renders a real primitive view: PreviewPane inside its slot', () => {
    render(
      <ViewsLayout
        arrangement={{ left: [], center: ['previewPane'], right: [], overlay: [] }}
        props={{
          previewPane: {
            status: 'running',
            url: 'http://localhost:3000',
            title: 'Example',
            onRestart: () => undefined,
          },
        }}
      />,
    );
    expect(within(screen.getByTestId('views-slot-center')).getByTestId('preview-pane')).toBeInTheDocument();
    expect(within(screen.getByTestId('preview-pane')).getByTestId('preview-pane-iframe')).toHaveAttribute(
      'src',
      'http://localhost:3000',
    );
  });
});
