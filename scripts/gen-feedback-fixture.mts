/**
 * Generates the cross-language feedback contract fixture.
 *
 * Runs the REAL webui feedback builder and writes its output to
 * `pkg/design/testdata/webui-feedback/new-annotation.json`, which
 * `pkg/design/feedback_contract_test.go` then feeds to the real Go reader.
 *
 * Neither side mocks; the artifact is the webui's actual output, so the Go
 * test pins agreement rather than one language's guess at the other.
 *
 * The artifact carries BOTH halves of the webui's decision, which is what makes
 * the Go test able to fail on either:
 *   - `document`: the §4d JSON `buildFeedbackFile` produces
 *   - `filename`: the name `feedbackWriteTarget` derives, i.e. where the webui
 *     would PUT that document (`login.json`, not `design/wireframes/login.json`)
 *
 * If the Go test recomputed the filename itself it would be duplicating the
 * webui's rule rather than checking it, and a stem regression would move both
 * sides together and go unnoticed.
 *
 * Run after changing `webui/src/design/feedbackWrite.ts`'s output shape or its
 * path rule:
 *   npx tsx scripts/gen-feedback-fixture.mts
 */

import fs from 'node:fs';
import path from 'node:path';
import { buildFeedbackFile, feedbackWriteTarget } from '../webui/src/design/feedbackWrite.ts';

const target = 'design/wireframes/login.svg';
const document = buildFeedbackFile(
  target,
  'Primary CTA reads as secondary; swap emphasis',
  'hierarchy',
  '2026-09-15T10:36:47Z',
  { x: 0.42, y: 0.18 },
);

const artifact = {
  description:
    'Produced by scripts/gen-feedback-fixture.mts from the real webui builder. ' +
    'Do not hand-edit; regenerate after changing feedbackWrite.ts.',
  target,
  filename: `${feedbackWriteTarget(target)}.json`,
  document,
};

const outDir = path.join('pkg', 'design', 'testdata', 'webui-feedback');
fs.mkdirSync(outDir, { recursive: true });
const out = path.join(outDir, 'new-annotation.json');
fs.writeFileSync(out, `${JSON.stringify(artifact, null, 2)}\n`);

console.log(`wrote ${out}`);
console.log(`  target:   ${target}`);
console.log(`  filename: ${artifact.filename}`);
console.log(`  status:   ${document.status}`);
console.log(`  annos:    ${document.annotations.length}`);
