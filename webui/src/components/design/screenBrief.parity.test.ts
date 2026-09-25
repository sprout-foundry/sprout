/**
 * Screen-brief parity (SP-140-8 acceptance 4): the webui's client-side brief
 * derivation and the Go `design_brief` are separate implementations of the
 * §5g contract, and until this fixture nothing asserted they agree on the
 * same tree.
 *
 * The fixture (`pkg/design/testdata/webui-brief/screen-brief.json`) carries
 * BOTH halves: the seeded design/ tree, and the Go brief `BuildScreenBrief`
 * produced over it. This test re-derives the brief client-side from that same
 * tree via `deriveScreenBrief` and pins the projected fields equal, so a
 * client-side drift from the Go contract fails here instead of shipping as a
 * silently different workbench.
 *
 * Known, deliberate divergences (documented on screenBrief.ts) are not
 * papered over: the far-endpoint *label* (`otherLabel`) differs when a flow
 * declares inline node labels (the fixture tree declares none), the known-
 * token set resolves without the alias projection, and feedback matching is
 * name-keyed. Every field this test pins is one the two arms must agree on.
 */

import fs from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import type { DesignFeedbackFile, DesignInventory } from '../../services/api/types';
import { deriveScreenBrief } from './screenBrief';

interface GoBriefEdge {
  flow: string;
  flowName: string;
  source: string;
  target: string;
  direction: string;
  trigger?: string;
  otherStem?: string;
}

interface GoBrief {
  screenName: string;
  found: boolean;
  purpose?: string;
  status?: string;
  listedInReadme: boolean;
  wireframe: string;
  wireframeExists: boolean;
  flowsIn: GoBriefEdge[];
  flowsOut: GoBriefEdge[];
  tokenPaths: Array<{ path: string; known: boolean }>;
  tokenGroups: Array<{ group: string; tokens: number }>;
  feedback: {
    path?: string;
    status?: string;
    open: number;
    total: number;
    pending: boolean;
    resolution?: string;
    notes?: string[];
  };
  screenFile?: string;
  guidance?: string;
}

interface BriefParityArtifact {
  description: string;
  screen: string;
  tree: Record<string, string>;
  goBrief: GoBrief;
}

const FIXTURE_PATH = path.resolve(__dirname, '../../../../pkg/design/testdata/webui-brief/screen-brief.json');
const artifact = JSON.parse(fs.readFileSync(FIXTURE_PATH, 'utf8')) as BriefParityArtifact;

/** The DesignInventory the webui's listAssets would build over the fixture tree. */
function inventoryFor(tree: Record<string, string>): DesignInventory {
  const wireframes = Object.keys(tree)
    .filter((p) => p.startsWith('design/wireframes/'))
    .map((p) => ({ path: p, name: p.split('/').pop() ?? p, kind: 'wireframe' as const, size: 0, modified: 0 }));
  const screens = Object.keys(tree)
    .filter((p) => p.startsWith('design/screens/'))
    .map((p) => ({ path: p, name: p.split('/').pop() ?? p, kind: 'screen' as const, size: 0, modified: 0 }));
  const flows = Object.keys(tree)
    .filter((p) => p.startsWith('design/flows/'))
    .map((p) => ({ path: p, name: p.split('/').pop() ?? p, kind: 'flow' as const, size: 0, modified: 0 }));
  const tokenFiles = Object.keys(tree)
    .filter((p) => p.startsWith('design/tokens/'))
    .map((p) => ({ path: p, name: p.split('/').pop() ?? p, kind: 'tokens' as const, size: 0, modified: 0 }));
  const feedbackEntry = {
    name: 'login.json',
    path: 'design/feedback/login.json',
    status: 'changes-requested',
    annotationCount: artifact.goBrief.feedback.total,
    resolvedCount: artifact.goBrief.feedback.total - artifact.goBrief.feedback.open,
  };
  return {
    exists: true,
    manifest: { path: 'design/README.md', exists: true, frames: [], chars: 0 },
    assets: [],
    wireframes,
    screens,
    flows,
    layouts: [],
    tokenFiles,
    feedback: [feedbackEntry],
    tokenGroups: artifact.goBrief.tokenGroups.map((g) => ({
      name: g.group,
      path: `design/tokens/${g.group}.tokens.json`,
      tokenCount: g.tokens,
      types: [],
    })),
    tokenCount: artifact.goBrief.tokenGroups.reduce((sum, g) => sum + g.tokens, 0),
    flowSummaries: [],
    summary: '',
  };
}

/** Project the Go brief to the client model's comparable shape. */
function goBriefProjection(go: GoBrief) {
  const edge = (e: GoBriefEdge) => ({
    flow: e.flow,
    flowName: e.flowName,
    source: e.source,
    target: e.target,
    direction: e.direction,
    trigger: e.trigger ?? '',
    otherStem: e.otherStem ?? '',
  });
  return {
    screenName: go.screenName,
    found: go.found,
    purpose: go.purpose ?? '',
    status: go.status ?? '',
    listedInReadme: go.listedInReadme,
    wireframe: go.wireframe,
    wireframeExists: go.wireframeExists,
    flowsIn: go.flowsIn.map(edge),
    flowsOut: go.flowsOut.map(edge),
    tokenRefs: go.tokenPaths,
    tokenGroups: go.tokenGroups.map((g) => g.group),
    feedback: {
      path: go.feedback.path ?? '',
      status: go.feedback.status ?? '',
      open: go.feedback.open,
      total: go.feedback.total,
      pending: go.feedback.pending,
      resolution: go.feedback.resolution ?? '',
      notes: go.feedback.notes ?? [],
    },
    screenFileExists: go.screenFile !== undefined && go.screenFile !== '',
    guidance: go.guidance ?? '',
  };
}

describe('screen-brief parity with the Go design_brief (shared fixture)', () => {
  const tree = artifact.tree;
  const brief = deriveScreenBrief({
    stem: artifact.screen,
    inventory: inventoryFor(tree),
    flowTexts: Object.fromEntries(Object.entries(tree).filter(([p]) => p.endsWith('.mmd'))),
    wireframeText: tree['design/wireframes/login.svg'] ?? '',
    readmeText: tree['design/README.md'] ?? '',
    tokenTexts: Object.fromEntries(Object.entries(tree).filter(([p]) => p.endsWith('.tokens.json'))),
    feedback: tree['design/feedback/login.json']
      ? (JSON.parse(tree['design/feedback/login.json']) as DesignFeedbackFile)
      : null,
  });

  it('agrees with the Go brief on every shared field', () => {
    expect(brief).toMatchObject(goBriefProjection(artifact.goBrief));
  });

  it('splits the flow edges into the same in/out lists with triggers', () => {
    const key = (e: { flow: string; source: string; target: string; direction: string; trigger?: string }) =>
      `${e.flow} ${e.source}->${e.target} (${e.direction}) "${e.trigger ?? ''}"`;
    expect(brief.flowsIn.map(key)).toEqual(artifact.goBrief.flowsIn.map(key));
    expect(brief.flowsOut.map(key)).toEqual(artifact.goBrief.flowsOut.map(key));
  });

  it('resolves the same token refs known/unknown', () => {
    expect(brief.tokenRefs).toEqual(artifact.goBrief.tokenPaths);
  });

  it('reads the same open-feedback state, including the note text', () => {
    expect(brief.feedback).toMatchObject(goBriefProjection(artifact.goBrief).feedback);
  });
});
