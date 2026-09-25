/**
 * ScreenWorkbench (SP-140-8 item 8.2) — the §8b facet pane.
 *
 * Pins the facet contract: the fixed order (render first), the header's
 * screen-relevant health subset, actionable open feedback (per-note agent
 * prefill + the resolution link), the flows in/out links, known/unknown
 * token refs, the agent-state slice, and the not-found state. The pane is
 * presentational — the derivation is covered in `screenBrief.test.ts`.
 */

import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { SproutAdapterProvider } from '../../contexts/SproutAdapterContext';
import type { DesignFeedbackAnnotation, DesignInventory } from '../../services/api/types';
import { deriveScreenBrief } from './screenBrief';
import type { ScreenBriefModel } from './screenBrief';
import ScreenWorkbench from './ScreenWorkbench';

vi.mock('../LivePreview', () => ({
  default: ({ content, fileName, previewPath }: { content: string; fileName: string; previewPath?: string }) => (
    <div data-testid="mock-live-preview" data-file={fileName} data-preview-path={previewPath ?? ''}>
      {content}
    </div>
  ),
}));

vi.mock('./ScreenStatusMenu', () => ({
  default: ({ stem }: { stem: string }) => (
    <button type="button" data-testid={`mock-status-menu-${stem}`}>
      status menu
    </button>
  ),
}));

function inventory(): DesignInventory {
  return {
    exists: true,
    manifest: { path: 'design/README.md', exists: true, frames: [], chars: 0 },
    assets: [],
    wireframes: [{ path: 'design/wireframes/login.svg', name: 'login.svg', kind: 'wireframe', size: 0, modified: 0 }],
    screens: [
      { path: 'design/screens/login.html', name: 'login.html', kind: 'screen', size: 0, modified: 0, status: 'review' },
    ],
    flows: [{ path: 'design/flows/sign-up.mmd', name: 'sign-up.mmd', kind: 'flow', size: 0, modified: 0 }],
    layouts: [],
    tokenFiles: [
      { path: 'design/tokens/base.tokens.json', name: 'base.tokens.json', kind: 'tokens', size: 0, modified: 0 },
    ],
    feedback: [
      {
        name: 'login.json',
        path: 'design/feedback/login.json',
        status: 'changes-requested',
        annotationCount: 2,
        resolvedCount: 0,
      },
    ],
    tokenGroups: [{ name: 'base', path: 'design/tokens/base.tokens.json', tokenCount: 2, types: ['color'] }],
    tokenCount: 2,
    flowSummaries: [],
    summary: '',
  };
}

const FLOW_TEXT = ['flowchart LR', '    start -->|tap| login', '    login -->|submit| dashboard'].join('\n');

const FEEDBACK: DesignFeedbackAnnotation[] = [
  { id: 'a1', at: { x: 0.2, y: 0.3 }, area: 'header', note: 'Title wraps at 390', resolved: false, created: '' },
  { id: 'a2', at: { x: 0.5, y: 0.9 }, area: 'cta', note: 'CTA contrast', resolved: false, created: '' },
];

function briefFor(stem = 'login'): ScreenBriefModel {
  return deriveScreenBrief({
    stem,
    inventory: inventory(),
    flowTexts: { 'design/flows/sign-up.mmd': FLOW_TEXT },
    wireframeText: '<svg><!-- {color.primary} --><!-- {motion.dur} --></svg>',
    readmeText: '- `login` — review — sign-in entry point',
    tokenTexts: {
      'design/tokens/base.tokens.json': JSON.stringify({
        color: { primary: { $value: '#123', $type: 'color' } },
      }),
    },
    feedback: {
      target: stem,
      status: 'changes-requested',
      resolution: '',
      annotations: FEEDBACK,
    },
  });
}

function renderWorkbench(props: Partial<Parameters<typeof ScreenWorkbench>[0]> = {}) {
  const propsList: Parameters<typeof ScreenWorkbench>[0] = {
    brief: briefFor(),
    renderContent: '<html>login screen</html>',
    renderLanguage: 'html',
    renderFileName: 'design/screens/login.html',
    annotations: FEEDBACK,
    ...props,
  };
  return render(
    <SproutAdapterProvider>
      <ScreenWorkbench {...propsList} />
    </SproutAdapterProvider>,
  );
}

describe('ScreenWorkbench', () => {
  it('renders the header health subset: name, status, open count, purpose', () => {
    renderWorkbench();
    const header = screen.getByTestId('design-workbench-header');
    expect(header).toHaveTextContent('login');
    expect(screen.getByTestId('design-workbench-status-chip')).toHaveTextContent('review');
    expect(screen.getByTestId('design-workbench-open-count')).toHaveTextContent('2 open');
    expect(header).toHaveTextContent('sign-in entry point');
  });

  it('renders the facets in the §8b order: render first, then status, feedback, flows, tokens, agent', () => {
    renderWorkbench();
    const ids = [
      'design-workbench-render',
      'design-workbench-status',
      'design-workbench-feedback',
      'design-workbench-flows',
      'design-workbench-tokens',
      'design-workbench-agent',
    ];
    const elements = ids.map((id) => screen.getByTestId(id));
    for (let i = 0; i < elements.length - 1; i += 1) {
      expect(document.compareDocumentPosition(elements[i + 1], elements[i]) & Node.DOCUMENT_POSITION_FOLLOWING).toBe(
        Node.DOCUMENT_POSITION_FOLLOWING,
      );
    }
  });

  it('renders the screen content in the render facet with the pins overlay', () => {
    renderWorkbench();
    expect(screen.getByTestId('mock-live-preview')).toHaveTextContent('login screen');
    expect(screen.getByTestId('design-pins')).toBeInTheDocument();
    expect(screen.getByTestId('design-pin-a1')).toBeInTheDocument();
    expect(screen.getByTestId('design-pin-a2')).toBeInTheDocument();
  });

  // SP-143 §143.4: an HTML screen's render facet previews through the ref
  // rewriter (the iframe copy only — the edited content stays original);
  // a wireframe SVG has no external refs and gets no preview path.
  it('hands the preview the workspace path for a screen, not for a wireframe', () => {
    renderWorkbench();
    expect(screen.getByTestId('mock-live-preview').getAttribute('data-preview-path')).toBe('design/screens/login.html');
  });

  it('hands a wireframe render no preview path', () => {
    renderWorkbench({ renderLanguage: 'svg', renderFileName: 'design/wireframes/login.svg' });
    expect(screen.getByTestId('mock-live-preview').getAttribute('data-preview-path')).toBe('');
  });

  it('lists the open feedback notes, each with an agent prefill, plus the resolution link', () => {
    const onAskAgent = vi.fn();
    const onOpenFeedbackPane = vi.fn();
    renderWorkbench({ onAskAgent, onOpenFeedbackPane });

    const note = screen.getByTestId('design-workbench-annotation-a1');
    expect(note).toHaveTextContent('[header] Title wraps at 390');
    fireEvent.click(screen.getByTestId('design-workbench-annotation-ask-a1'));
    expect(onAskAgent).toHaveBeenCalledTimes(1);
    const prompt = onAskAgent.mock.calls[0][0] as string;
    expect(prompt).toContain('login');
    expect(prompt).toContain('Title wraps at 390');
    expect(prompt).toContain('design/feedback/login.json');

    fireEvent.click(screen.getByTestId('design-workbench-feedback-resolve'));
    expect(onOpenFeedbackPane).toHaveBeenCalled();
  });

  it('renders the flows in/out with triggers and the canvas links', () => {
    const onOpenFlow = vi.fn();
    renderWorkbench({ onOpenFlow });
    expect(screen.getByTestId('design-workbench-flow-in-sign-up')).toBeInTheDocument();
    expect(screen.getByText('start → login')).toBeInTheDocument();
    expect(screen.getByText('on “tap”')).toBeInTheDocument();
    expect(screen.getByText('login → dashboard')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('design-workbench-flow-in-sign-up-open'));
    expect(onOpenFlow).toHaveBeenCalledWith('design/flows/sign-up.mmd');
  });

  it('marks token refs known vs. unknown against the tree and links to the library', () => {
    const onOpenTokens = vi.fn();
    renderWorkbench({ onOpenTokens });
    const known = screen.getByTestId('design-workbench-token-ref-color.primary');
    const unknown = screen.getByTestId('design-workbench-token-ref-motion.dur');
    expect(known).toHaveAttribute('data-known', 'true');
    expect(unknown).toHaveAttribute('data-known', 'false');
    expect(unknown).toHaveTextContent('(unknown)');
    fireEvent.click(screen.getByTestId('design-workbench-token-link'));
    expect(onOpenTokens).toHaveBeenCalled();
  });

  it('surfaces the agent-state slice: the screen files and the scoped brief query', () => {
    const onAskAgent = vi.fn();
    renderWorkbench({ onAskAgent });
    const agent = screen.getByTestId('design-workbench-agent');
    expect(agent).toHaveTextContent('design/wireframes/login.svg');
    expect(agent).toHaveTextContent('design/feedback/login.json');
    expect(agent).toHaveTextContent('design/flows/sign-up.mmd');
    fireEvent.click(screen.getByTestId('design-workbench-agent-ask'));
    const prompt = onAskAgent.mock.calls[0][0] as string;
    expect(prompt).toContain('login');
    expect(prompt).toContain('design_brief(depth=full)');
  });

  it('reports no open annotations when the file exists but everything is resolved', () => {
    const resolved = FEEDBACK.map((annotation) => ({ ...annotation, resolved: true }));
    const brief = briefFor();
    renderWorkbench({ brief: { ...brief, feedback: { ...brief.feedback, open: 0 } }, annotations: resolved });
    expect(screen.getByTestId('design-workbench-feedback-empty')).toHaveTextContent('No open annotations.');
  });

  it('reports no feedback file when the screen has none', () => {
    const brief = briefFor();
    renderWorkbench({
      brief: { ...brief, feedback: { ...brief.feedback, path: '', open: 0, total: 0, pending: false, notes: [] } },
      annotations: [],
    });
    expect(screen.getByTestId('design-workbench-feedback-empty')).toHaveTextContent(
      'No feedback file for this screen yet.',
    );
  });

  it('shows the not-found guidance and no facets', () => {
    renderWorkbench({ brief: briefFor('ghost') });
    const root = screen.getByTestId('design-workbench');
    expect(root).toHaveAttribute('data-found', 'false');
    expect(screen.getByTestId('design-workbench-not-found')).toHaveTextContent('design/wireframes/ghost.svg');
    expect(screen.queryByTestId('design-workbench-render')).toBeNull();
  });
});
