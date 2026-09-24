/**
 * screenBrief (SP-140-8 item 8.2) — the client-side §5g brief derivation.
 *
 * Pins the contract mirrors: the manifest listing rule (SP-140-1 §1e), the
 * flow in/out split with triggers, the `{group.token}` known/unknown
 * resolution against the tree's token files, the §4d feedback view, and the
 * brief's deterministic ordering + not-found guidance.
 */

import { describe, expect, it } from 'vitest';
import type { DesignInventory, DesignFeedbackFile } from '../../services/api/types';
import {
  deriveScreenBrief,
  feedbackModelOf,
  flowEdgesForBrief,
  knownTokenPaths,
  openAnnotationNotes,
  parseManifestListings,
  stemOf,
  tokenRefsOf,
  type ScreenBriefInput,
} from './screenBrief';

/* -------------------------------------------------------------------------- */
/* Stems + manifest listings                                                    */
/* -------------------------------------------------------------------------- */

describe('stemOf', () => {
  it('strips the extension and lowercases, keeping a dotted stem intact', () => {
    expect(stemOf('design/wireframes/Login.svg')).toBe('login');
    expect(stemOf('screens/Sign-Up.html')).toBe('sign-up');
    expect(stemOf('feedback/checkout.json')).toBe('checkout');
  });
});

describe('parseManifestListings', () => {
  const README = [
    '# Design Workspace',
    '',
    '## Screens',
    '',
    '- `login` — ready — sign-in entry point',
    '- `sign-up` — sign-up onboarding flow',
    '- `dashboard` — review',
    '- `orphan`',
    '- `notes` — draft',
    '- not a listing bullet with backticks',
    '- `plain` — ready',
  ].join('\n');

  it('splits a status-led multi-segment tail into status + purpose', () => {
    const { statuses, summaries } = parseManifestListings(README);
    expect(statuses.login).toBe('ready');
    expect(summaries.login).toBe('sign-in entry point');
  });

  it('treats a single segment as the purpose when it is not a status', () => {
    const { statuses, summaries } = parseManifestListings(README);
    expect(statuses['sign-up']).toBeUndefined();
    expect(summaries['sign-up']).toBe('sign-up onboarding flow');
  });

  it('reads a status with no summary', () => {
    const { statuses, summaries } = parseManifestListings(README);
    expect(statuses.dashboard).toBe('review');
    expect(summaries.dashboard).toBeUndefined();
    expect(statuses.plain).toBe('ready');
  });

  it('joins a multi-segment non-status tail into one summary', () => {
    const { summaries } = parseManifestListings('- `x` — a — b\n');
    expect(summaries.x).toBe('a — b');
  });

  it('lists a screen named in a bare bullet and ignores non-listings', () => {
    const { listed } = parseManifestListings(README);
    expect(listed).toEqual(['login', 'sign-up', 'dashboard', 'orphan', 'notes', 'plain']);
  });

  it('is empty for empty text', () => {
    const { statuses, summaries, listed } = parseManifestListings('');
    expect(statuses).toEqual({});
    expect(summaries).toEqual({});
    expect(listed).toEqual([]);
  });
});

/* -------------------------------------------------------------------------- */
/* Flow edges                                                                   */
/* -------------------------------------------------------------------------- */

const SIGN_UP_FLOW = [
  'flowchart LR',
  '    start[Start] -->|tap Sign up| sign-up',
  '    sign-up -->|submit| login',
  '    login --> login',
  '    login -->|reset link| dashboard',
].join('\n');

function edgesFor(stem: string) {
  return flowEdgesForBrief(SIGN_UP_FLOW, stem);
}

describe('flowEdgesForBrief', () => {
  it('splits in/out by the screen and carries the trigger label', () => {
    const edges = edgesFor('login');
    const inEdge = edges.find((edge) => edge.direction === 'in');
    const outEdge = edges.find((edge) => edge.direction === 'out');
    expect(inEdge).toMatchObject({
      source: 'sign-up',
      target: 'login',
      trigger: 'submit',
      otherStem: 'sign-up',
      otherLabel: 'sign-up',
    });
    expect(outEdge).toMatchObject({
      source: 'login',
      target: 'dashboard',
      trigger: 'reset link',
      otherStem: 'dashboard',
      otherLabel: 'dashboard',
    });
  });

  it('reports a self-edge as direction "both" with an empty far endpoint', () => {
    const self = edgesFor('login').find((edge) => edge.direction === 'both');
    expect(self).toMatchObject({ source: 'login', target: 'login', otherStem: '', otherLabel: '' });
  });

  it('labels the far endpoint with the flow node label, not the id', () => {
    // A standalone declaration preserves the label; the far endpoint of a
    // same-line labeled edge falls back to its id (a `flowText.ts` quirk:
    // the `|label|` prefix defeats inline label extraction).
    const edges = flowEdgesForBrief('flowchart TD\n  b[Settings]\n  a -->|go| b', 'a');
    expect(edges[0]).toMatchObject({ direction: 'out', otherStem: 'b', otherLabel: 'Settings' });
  });

  it('returns an empty list for an untouched stem', () => {
    expect(flowEdgesForBrief(SIGN_UP_FLOW, 'nowhere')).toEqual([]);
  });

  it('leaves an unlabelled edge with an empty trigger', () => {
    const edges = flowEdgesForBrief('flowchart TD\n  a --> b', 'a');
    expect(edges[0]?.trigger).toBe('');
  });
});

/* -------------------------------------------------------------------------- */
/* Tokens                                                                       */
/* -------------------------------------------------------------------------- */

const TOKENS_JSON = JSON.stringify({
  color: {
    primary: { $value: '#0f6', $type: 'color' },
    scale: {
      50: { $value: '#fff', $type: 'color' },
    },
  },
  spacing: {
    base: { $value: '4', $type: 'dimension' },
  },
});

describe('knownTokenPaths', () => {
  it('collects the dotted leaf paths across token files', () => {
    const known = knownTokenPaths({ 'design/tokens/base.tokens.json': TOKENS_JSON });
    expect(known).toEqual(new Set(['color.primary', 'color.scale.50', 'spacing.base']));
  });

  it('skips unreadable (unparseable) files rather than failing', () => {
    const known = knownTokenPaths({ 'design/tokens/bad.tokens.json': 'not json' });
    expect(known).toEqual(new Set());
  });

  it('yields an empty set with no token text', () => {
    expect(knownTokenPaths(undefined)).toEqual(new Set());
  });
});

describe('tokenRefsOf', () => {
  const known = new Set(['color.primary', 'spacing.base']);

  it('resolves refs against the known set, de-duplicated and sorted', () => {
    const text = [
      '<!-- {color.primary} -->',
      '<svg><!-- {spacing.base} -->',
      '  <!-- {color.primary} again -->',
      '  <!-- {motion.dur} -->',
      '  <text {color.primary} />',
      '</svg>',
    ].join('\n');
    expect(tokenRefsOf(text, known)).toEqual([
      { path: 'color.primary', known: true },
      { path: 'motion.dur', known: false },
      { path: 'spacing.base', known: true },
    ]);
  });

  it('ignores single-segment braces and returns nothing for empty text', () => {
    expect(tokenRefsOf('{color}', known)).toEqual([]);
    expect(tokenRefsOf('', known)).toEqual([]);
  });
});

/* -------------------------------------------------------------------------- */
/* Feedback                                                                     */
/* -------------------------------------------------------------------------- */

const FEEDBACK_FILE: DesignFeedbackFile = {
  target: 'login',
  status: 'changes-requested',
  resolution: '',
  annotations: [
    { id: 'a1', at: { x: 0.2, y: 0.3 }, area: 'header', note: 'Title wraps at 390', resolved: false },
    { id: 'a2', at: { x: 0.5, y: 0.9 }, area: 'footer', note: 'CTA contrast', resolved: true },
    { id: 'a3', at: { x: 0.5, y: 0.5 }, area: '', note: 'Bare note', resolved: false },
    { id: 'a4', at: { x: 0.1, y: 0.1 }, area: 'grid', note: '', resolved: false },
  ],
};

describe('openAnnotationNotes', () => {
  it('renders "[area] note" for open annotations in file order, skipping resolved', () => {
    expect(openAnnotationNotes(FEEDBACK_FILE.annotations)).toEqual([
      '[header] Title wraps at 390',
      'Bare note',
      '[grid]',
    ]);
  });

  it('is empty for no annotations', () => {
    expect(openAnnotationNotes(null)).toEqual([]);
  });
});

describe('feedbackModelOf', () => {
  it('prefers the inventory entry for existence and counts, the file for notes', () => {
    const model = feedbackModelOf(
      {
        name: 'login.json',
        path: 'design/feedback/login.json',
        status: 'changes-requested',
        annotationCount: 4,
        resolvedCount: 1,
      },
      FEEDBACK_FILE,
    );
    expect(model).toMatchObject({
      path: 'design/feedback/login.json',
      status: 'changes-requested',
      open: 3,
      total: 4,
      pending: true,
    });
    expect(model.notes).toEqual(['[header] Title wraps at 390', 'Bare note', '[grid]']);
  });

  it('falls back to the parsed file when the inventory has no entry', () => {
    const model = feedbackModelOf(null, { ...FEEDBACK_FILE, status: '' });
    expect(model.open).toBe(3);
    expect(model.total).toBe(4);
    expect(model.path).toBe('');
  });

  it('reports pending only for a pending status or open notes', () => {
    expect(
      feedbackModelOf(null, {
        ...FEEDBACK_FILE,
        status: 'resolved',
        annotations: FEEDBACK_FILE.annotations.filter((a) => a.resolved),
      }).pending,
    ).toBe(false);
    expect(feedbackModelOf(null, { ...FEEDBACK_FILE, status: '' }).pending).toBe(true);
  });

  it('yields an empty view for no entry and no file', () => {
    expect(feedbackModelOf(null, null)).toMatchObject({
      path: '',
      status: '',
      open: 0,
      total: 0,
      pending: false,
      notes: [],
    });
  });
});

/* -------------------------------------------------------------------------- */
/* The brief itself                                                             */
/* -------------------------------------------------------------------------- */

function inventoryFor(overrides: Partial<DesignInventory> = {}): DesignInventory {
  const base: DesignInventory = {
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
        annotationCount: 4,
        resolvedCount: 1,
      },
    ],
    tokenGroups: [
      { name: 'base', path: 'design/tokens/base.tokens.json', tokenCount: 3, types: ['color', 'dimension'] },
    ],
    tokenCount: 3,
    flowSummaries: [{ name: 'sign-up', path: 'design/flows/sign-up.mmd', nodeCount: 4, edgeCount: 4, direction: 'LR' }],
    summary: '',
  };
  return { ...base, ...overrides };
}

function inputFor(stem: string, overrides: Partial<ScreenBriefInput> = {}): ScreenBriefInput {
  return {
    stem,
    inventory: inventoryFor(),
    flowTexts: { 'design/flows/sign-up.mmd': SIGN_UP_FLOW },
    wireframeText: '<svg><!-- {color.primary} --><!-- {motion.dur} --></svg>',
    readmeText: '- `login` — review — sign-in entry point',
    tokenTexts: { 'design/tokens/base.tokens.json': TOKENS_JSON },
    feedback: FEEDBACK_FILE,
    ...overrides,
  };
}

describe('deriveScreenBrief', () => {
  it('derives the found brief: status, purpose, files, flows, tokens, feedback', () => {
    const brief = deriveScreenBrief(inputFor('login'));
    expect(brief).toMatchObject({
      screenName: 'login',
      found: true,
      status: 'review',
      purpose: 'sign-in entry point',
      listedInReadme: true,
      wireframe: 'design/wireframes/login.svg',
      wireframeExists: true,
      screenFile: 'design/screens/login.html',
      screenFileExists: true,
      guidance: '',
    });
    expect(brief.tokenRefs).toEqual([
      { path: 'color.primary', known: true },
      { path: 'motion.dur', known: false },
    ]);
    expect(brief.tokenGroups).toEqual(['base']);
    expect(brief.feedback).toMatchObject({ path: 'design/feedback/login.json', open: 3, total: 4, pending: true });
  });

  it('splits the flow edges into in/out with triggers, in deterministic order', () => {
    const brief = deriveScreenBrief(inputFor('login'));
    // A self-edge (login --> login) is direction "both" and lands in BOTH
    // lists, mirroring the Go design_brief contract. Sorted by flow, then
    // source, then target, then trigger.
    expect(brief.flowsIn).toEqual([
      {
        flow: 'design/flows/sign-up.mmd',
        flowName: 'sign-up',
        source: 'login',
        target: 'login',
        direction: 'both',
        trigger: '',
        otherStem: '',
        otherLabel: '',
      },
      {
        flow: 'design/flows/sign-up.mmd',
        flowName: 'sign-up',
        source: 'sign-up',
        target: 'login',
        direction: 'in',
        trigger: 'submit',
        otherStem: 'sign-up',
        otherLabel: 'sign-up',
      },
    ]);
    const out = brief.flowsOut;
    expect(out.map((edge) => edge.target)).toEqual(['dashboard', 'login']);
    expect(out[0]).toMatchObject({
      source: 'login',
      target: 'dashboard',
      direction: 'out',
      trigger: 'reset link',
      otherStem: 'dashboard',
    });
    expect(out[1]).toMatchObject({ source: 'login', target: 'login', direction: 'both' });
  });

  it('reports a not-found screen with guidance, no flows, and an empty feedback view', () => {
    const brief = deriveScreenBrief(
      inputFor('ghost', {
        inventory: inventoryFor({ wireframes: [], screens: [], feedback: [] }),
        feedback: null,
      }),
    );
    expect(brief.found).toBe(false);
    expect(brief.wireframeExists).toBe(false);
    expect(brief.flowsIn).toEqual([]);
    expect(brief.feedback.path).toBe('');
    expect(brief.guidance).toContain('design/wireframes/ghost.svg');
    expect(brief.guidance).toContain('ghost');
  });

  it('counts a screen that exists only as feedback as found', () => {
    const brief = deriveScreenBrief(
      inputFor('ghost', {
        inventory: inventoryFor({
          wireframes: [],
          screens: [],
          feedback: [
            {
              name: 'ghost.json',
              path: 'design/feedback/ghost.json',
              status: 'changes-requested',
              annotationCount: 1,
              resolvedCount: 0,
            },
          ],
        }),
        wireframeText: '',
      }),
    );
    expect(brief.found).toBe(true);
    expect(brief.guidance).toBe('');
    expect(brief.feedback.open).toBe(1);
  });

  it('skips an unreadable flow instead of failing the brief', () => {
    const brief = deriveScreenBrief(inputFor('login', { flowTexts: {} }));
    expect(brief.flowsIn).toEqual([]);
    expect(brief.found).toBe(true);
  });

  it('uses the inventory entry path over the canonical path when present', () => {
    const brief = deriveScreenBrief(
      inputFor('login', {
        inventory: inventoryFor({
          wireframes: [{ path: 'wireframes/login.svg', name: 'login.svg', kind: 'wireframe', size: 0, modified: 0 }],
        }),
      }),
    );
    expect(brief.wireframe).toBe('wireframes/login.svg');
  });

  it('orders edges across multiple flows by flow, then endpoints (the Go brief sort key)', () => {
    const brief = deriveScreenBrief(
      inputFor('login', {
        inventory: inventoryFor({
          flows: [
            { path: 'design/flows/zed-flow.mmd', name: 'zed-flow.mmd', kind: 'flow', size: 0, modified: 0 },
            { path: 'design/flows/sign-up.mmd', name: 'sign-up.mmd', kind: 'flow', size: 0, modified: 0 },
          ],
        }),
        flowTexts: {
          'design/flows/zed-flow.mmd': 'flowchart TD\n  login -->|later| dashboard\n',
          'design/flows/sign-up.mmd': SIGN_UP_FLOW,
        },
      }),
    );
    // Inventory order (zed first) must not matter: edges sort by flow path.
    // sign-up sorts before zed-flow; within sign-up the self-edge (login→login)
    // sorts before sign-up→login by source.
    expect(brief.flowsIn.map((edge) => edge.flowName)).toEqual(['sign-up', 'sign-up']);
    expect(brief.flowsOut.map((edge) => `${edge.flowName}:${edge.target}`)).toEqual([
      'sign-up:dashboard',
      'sign-up:login',
      'zed-flow:dashboard',
    ]);
  });
});
