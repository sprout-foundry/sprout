// Chromatic Workflow Validation Tests
// ====================================
// Validates .github/workflows/chromatic.yml for YAML validity, correct trigger
// configuration, action versions, non-blocking behavior, paths filter, and
// security (no hardcoded secrets).
//
// Run with: node .github/workflows/__tests__/chromatic-workflow.test.js

import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import yaml from 'js-yaml';

// ── Lazy-loading helpers ───────────────────────────────────────────────

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const WORKFLOW_PATH = path.resolve(__dirname, '../chromatic.yml');

let raw;
let workflow;

function readWorkflow() {
  if (raw === undefined) {
    raw = fs.readFileSync(WORKFLOW_PATH, 'utf-8');
  }
  return raw;
}

function getWorkflow() {
  if (!workflow) {
    const content = readWorkflow();
    workflow = yaml.load(content);
  }
  return workflow;
}

// ── Tests ──────────────────────────────────────────────────────────────

describe('chromatic.yml — YAML validity', () => {
  test('file is valid YAML that parses without errors', () => {
    assert.doesNotThrow(() => getWorkflow());
  });

  test('workflow has a name', () => {
    const w = getWorkflow();
    assert.ok(
      w.name && typeof w.name === 'string' && w.name.length > 0,
      'Workflow file did not parse to an object'
    );
  });

  test('workflow name references Chromatic', () => {
    const w = getWorkflow();
    assert.ok(
      w.name.toLowerCase().includes('chromatic'),
      `Workflow name "${w.name}" does not reference Chromatic`
    );
  });
});

describe('chromatic.yml — Trigger configuration', () => {
  const w = getWorkflow();
  test('has on field with trigger definitions', () => {
    assert.ok(
      w.on,
      'Workflow is missing "on" trigger configuration'
    );
  });

  test('triggers on pull_request', () => {
    assert.ok(
      w.on.pull_request,
      'Workflow does not trigger on pull_request'
    );
  });

  test('pull_request targets main branch', () => {
    const pr = w.on.pull_request;
    assert.ok(
      pr.branches && Array.isArray(pr.branches),
      'pull_request trigger is missing branches array'
    );
    assert.ok(
      pr.branches.includes('main'),
      `pull_request branches ${JSON.stringify(pr.branches)} does not include "main"`
    );
  });

  test('pull_request has paths filter for packages/ui/**', () => {
    const pr = w.on.pull_request;
    assert.ok(
      pr.paths && Array.isArray(pr.paths),
      'pull_request trigger is missing paths filter'
    );
    assert.ok(
      pr.paths.includes('packages/ui/**'),
      `pull_request paths ${JSON.stringify(pr.paths)} does not include "packages/ui/**"`
    );
  });

  test('triggers on push', () => {
    assert.ok(
      w.on.push,
      'Workflow does not trigger on push'
    );
  });

  test('push targets main branch', () => {
    const push = w.on.push;
    assert.ok(
      push.branches && Array.isArray(push.branches),
      'push trigger is missing branches array'
    );
    assert.ok(
      push.branches.includes('main'),
      `push branches ${JSON.stringify(push.branches)} does not include "main"`
    );
  });

  test('push has paths filter for packages/ui/**', () => {
    const push = w.on.push;
    assert.ok(
      push.paths && Array.isArray(push.paths),
      'push trigger is missing paths filter'
    );
    assert.ok(
      push.paths.includes('packages/ui/**'),
      `push paths ${JSON.stringify(push.paths)} does not include "packages/ui/**"`
    );
  });

  test('does NOT trigger on schedule', () => {
    assert.ok(
      !w.on.schedule,
      'Workflow should not trigger on schedule — that would run chromatic unnecessarily'
    );
  });

  test('does NOT trigger on workflow_dispatch (uncontrolled manual triggers)', () => {
    assert.ok(
      !w.on.workflow_dispatch,
      'Workflow should not trigger on workflow_dispatch — chromatic should only run on PRs and pushes'
    );
  });
});

describe('chromatic.yml — Concurrency configuration', () => {
  const w = getWorkflow();
  test('has concurrency group defined', () => {
    assert.ok(
      w.concurrency && w.concurrency.group,
      'Workflow is missing concurrency configuration'
    );
  });

  test('concurrency group uses chromatic- prefix', () => {
    assert.ok(
      w.concurrency.group.includes('chromatic-'),
      `Concurrency group "${w.concurrency.group}" does not use "chromatic-" prefix`
    );
  });

  test('concurrency group includes head_ref or ref for branch isolation', () => {
    const group = w.concurrency.group;
    assert.ok(
      group.includes('head_ref') || group.includes('github.ref'),
      `Concurrency group "${group}" should reference head_ref or github.ref for branch isolation`
    );
  });

  test('cancel-in-progress is enabled', () => {
    assert.equal(
      w.concurrency['cancel-in-progress'],
      true,
      'cancel-in-progress should be true to cancel stale runs'
    );
  });
});

describe('chromatic.yml — Job definition', () => {
  const w = getWorkflow();
  test('has a chromatic job', () => {
    assert.ok(
      w.jobs && w.jobs.chromatic,
      'Workflow is missing the "chromatic" job'
    );
  });

  test('chromatic job runs on ubuntu-latest', () => {
    assert.equal(
      w.jobs.chromatic['runs-on'],
      'ubuntu-latest',
      `chromatic job runs on "${w.jobs.chromatic['runs-on']}" instead of "ubuntu-latest"`
    );
  });

  test('chromatic job has a descriptive name', () => {
    const jobName = w.jobs.chromatic.name;
    assert.ok(
      jobName && jobName.length > 0,
      'chromatic job is missing a descriptive name'
    );
  });

  test('chromatic job has permissions defined', () => {
    assert.ok(
      w.jobs.chromatic.permissions,
      'chromatic job is missing permissions (should follow least-privilege principle)'
    );
  });

  test('chromatic job grants contents: read permission', () => {
    const perms = w.jobs.chromatic.permissions;
    assert.equal(
      perms.contents,
      'read',
      'chromatic job should have contents: read permission for checkout'
    );
  });

  test('chromatic job grants pull-requests: write permission', () => {
    const perms = w.jobs.chromatic.permissions;
    assert.equal(
      perms['pull-requests'],
      'write',
      'chromatic job should have pull-requests: write permission for status comments'
    );
  });

  test('chromatic job does NOT grant write to contents (principle of least privilege)', () => {
    assert.equal(
      w.jobs.chromatic.permissions.contents,
      'read',
      'chromatic job should only have read access to contents, not write'
    );
  });
});

describe('chromatic.yml — Steps', () => {
  const steps = getWorkflow().jobs.chromatic.steps;

  test('has steps array', () => {
    assert.ok(
      Array.isArray(steps) && steps.length > 0,
      'chromatic job has no steps'
    );
  });

  test('first step is checkout using actions/checkout@v4', () => {
    const checkout = steps.find(s => s.name && s.name.toLowerCase().includes('checkout'));
    assert.ok(
      checkout,
      'No checkout step found'
    );
    assert.equal(
      checkout.uses,
      'actions/checkout@v4',
      `Checkout uses "${checkout.uses}" — should be "actions/checkout@v4"`
    );
  });

  test('checkout step has fetch-depth: 0 for full history', () => {
    const checkout = steps.find(s => s.name && s.name.toLowerCase().includes('checkout'));
    assert.equal(
      checkout['with']['fetch-depth'],
      0,
      'Checkout should have fetch-depth: 0 for chromatic to detect changes'
    );
  });

  test('has setup-node step using actions/setup-node@v4', () => {
    const setupNode = steps.find(s => s.name && s.name.toLowerCase().includes('node'));
    assert.ok(
      setupNode,
      'No setup-node step found'
    );
    assert.equal(
      setupNode.uses,
      'actions/setup-node@v4',
      `Setup-node uses "${setupNode.uses}" — should be "actions/setup-node@v4"`
    );
  });

  test('setup-node uses node-version 22', () => {
    const setupNode = steps.find(s => s.name && s.name.toLowerCase().includes('node'));
    assert.ok(
      setupNode['with']['node-version'] === '22' || setupNode['with']['node-version'] === 22,
      `Node version is "${setupNode['with']['node-version']}" — should be "22"`
    );
  });

  test('setup-node has npm cache configured', () => {
    const setupNode = steps.find(s => s.name && s.name.toLowerCase().includes('node'));
    assert.equal(
      setupNode['with']['cache'],
      'npm',
      'Setup-node should use npm cache'
    );
  });

  test('setup-node has cache-dependency-path pointing to packages/ui/package-lock.json', () => {
    const setupNode = steps.find(s => s.name && s.name.toLowerCase().includes('node'));
    assert.equal(
      setupNode['with']['cache-dependency-path'],
      'packages/ui/package-lock.json',
      'cache-dependency-path should point to packages/ui/package-lock.json'
    );
  });

  test('has install dependencies step using npm ci', () => {
    const install = steps.find(s => s.name && s.name.toLowerCase().includes('install'));
    assert.ok(
      install,
      'No install step found'
    );
    assert.ok(
      install.run && install.run.includes('npm ci'),
      `Install step should use "npm ci" but runs: "${install.run}"`
    );
  });

  test('install step runs in packages/ui directory', () => {
    const install = steps.find(s => s.name && s.name.toLowerCase().includes('install'));
    assert.ok(
      (install.run && install.run.includes('packages/ui')) || install['working-directory'] === 'packages/ui',
      `Install step should run in packages/ui directory: "${install.run}"`
    );
  });

  test('has chromatic-action step using chromaui/chromatic-action@v11', () => {
    const chromaticStep = steps.find(s => s.name && s.name.toLowerCase().includes('chromatic'));
    assert.ok(
      chromaticStep,
      'No chromatic step found'
    );
    assert.equal(
      chromaticStep.uses,
      'chromaui/chromatic-action@v11',
      `Chromatic action uses "${chromaticStep.uses}" — should be "chromaui/chromatic-action@v11"`
    );
  });
});

describe('chromatic.yml — Chromatic action configuration', () => {
  const chromaticStep = getWorkflow().jobs.chromatic.steps.find(
    s => s.name && s.name.toLowerCase().includes('chromatic')
  );

  test('chromatic action specifies workingDirectory as packages/ui', () => {
    assert.equal(
      chromaticStep['with']['workingDirectory'],
      'packages/ui',
      `workingDirectory is "${chromaticStep['with']['workingDirectory']}" — should be "packages/ui"`
    );
  });

  test('chromatic action specifies buildCommandName as build-storybook', () => {
    assert.equal(
      chromaticStep['with']['buildCommandName'],
      'build-storybook',
      `buildCommandName is "${chromaticStep['with']['buildCommandName']}" — should be "build-storybook"`
    );
  });

  test('chromatic action has onlyChanged: true (non-blocking optimization)', () => {
    assert.equal(
      chromaticStep['with']['onlyChanged'],
      true,
      'onlyChanged should be true to only test changed stories'
    );
  });

  test('chromatic action has exitZeroOnChanges: true (non-blocking CI)', () => {
    assert.equal(
      chromaticStep['with']['exitZeroOnChanges'],
      true,
      'exitZeroOnChanges should be true so visual diffs do not fail CI'
    );
  });
});

describe('chromatic.yml — Security', () => {
  test('does NOT contain hardcoded tokens or secrets in raw content', () => {
    // Check for common secret patterns that should never be in source
    const secretPatterns = [
      /chromatic[_-]?token["'\s]*[:=]\s*['"][a-zA-Z0-9]{20,}['"]/i,
      /project[_-]?token["'\s]*[:=]\s*['"][a-zA-Z0-9]{20,}['"]/i,
      /ckey_[a-zA-Z0-9]{20,}/,
    ];
    const content = readWorkflow();
    for (const pattern of secretPatterns) {
      const match = content.match(pattern);
      assert.ok(
        !match,
        `Potential hardcoded secret found: "${match?.[0] ?? pattern}" — secrets should use GitHub secret references`
      );
    }
  });

  test('projectToken references GitHub secrets via expression syntax', () => {
    assert.ok(
      readWorkflow().includes('secrets.CHROMATIC_PROJECT_TOKEN'),
      'projectToken should reference secrets.CHROMATIC_PROJECT_TOKEN'
    );
  });

  test('projectToken uses GitHub expression syntax ${{ }}', () => {
    assert.ok(
      readWorkflow().includes('${{ secrets.CHROMATIC_PROJECT_TOKEN }}'),
      'projectToken should use ${{ secrets.CHROMATIC_PROJECT_TOKEN }} syntax'
    );
  });
});

describe('chromatic.yml — Consistency with other workflows', () => {
  // Read the reference workflow for comparison
  const publishWorkflowPath = path.resolve(__dirname, '../publish-ui.yml');
  const publishRaw = fs.readFileSync(publishWorkflowPath, 'utf-8');
  const publishWorkflow = yaml.load(publishRaw);

  test('uses same actions/checkout version as publish-ui.yml', () => {
    const chromaticCheckout = getWorkflow().jobs.chromatic.steps.find(
      s => s.name && s.name.toLowerCase().includes('checkout')
    );
    const publishCheckout = publishWorkflow.jobs['publish-ui'].steps.find(
      s => s.name && s.name.toLowerCase().includes('checkout')
    );
    assert.equal(
      chromaticCheckout.uses,
      publishCheckout.uses,
      `Checkout action versions differ: chromatic=${chromaticCheckout.uses}, publish=${publishCheckout.uses}`
    );
  });

  test('uses same actions/setup-node version as publish-ui.yml', () => {
    const chromaticSetup = getWorkflow().jobs.chromatic.steps.find(
      s => s.name && s.name.toLowerCase().includes('node')
    );
    const publishSetup = publishWorkflow.jobs['publish-ui'].steps.find(
      s => s.name && s.name.toLowerCase().includes('node')
    );
    assert.equal(
      chromaticSetup.uses,
      publishSetup.uses,
      `Setup-node action versions differ: chromatic=${chromaticSetup.uses}, publish=${publishSetup.uses}`
    );
  });

  test('uses same node version as publish-ui.yml', () => {
    const chromaticSetup = getWorkflow().jobs.chromatic.steps.find(
      s => s.name && s.name.toLowerCase().includes('node')
    );
    const publishSetup = publishWorkflow.jobs['publish-ui'].steps.find(
      s => s.name && s.name.toLowerCase().includes('node')
    );
    assert.equal(
      String(chromaticSetup['with']['node-version']),
      String(publishSetup['with']['node-version']),
      `Node versions differ: chromatic=${chromaticSetup['with']['node-version']}, publish=${publishSetup['with']['node-version']}`
    );
  });

  test('uses same npm cache configuration as publish-ui.yml', () => {
    const chromaticSetup = getWorkflow().jobs.chromatic.steps.find(
      s => s.name && s.name.toLowerCase().includes('node')
    );
    const publishSetup = publishWorkflow.jobs['publish-ui'].steps.find(
      s => s.name && s.name.toLowerCase().includes('node')
    );
    assert.equal(
      chromaticSetup['with']['cache'],
      publishSetup['with']['cache'],
      `Cache settings differ: chromatic=${chromaticSetup['with']['cache']}, publish=${publishSetup['with']['cache']}`
    );
    assert.equal(
      chromaticSetup['with']['cache-dependency-path'],
      publishSetup['with']['cache-dependency-path'],
      `Cache dependency paths differ`
    );
  });

  test('uses same install command pattern as publish-ui.yml (npm ci)', () => {
    const chromaticInstall = getWorkflow().jobs.chromatic.steps.find(
      s => s.name && s.name.toLowerCase().includes('install')
    );
    const publishInstall = publishWorkflow.jobs['publish-ui'].steps.find(
      s => s.name && s.name.toLowerCase().includes('install')
    );
    assert.ok(
      chromaticInstall.run.includes('npm ci'),
      'Chromatic should use npm ci like publish-ui.yml'
    );
    assert.ok(
      publishInstall.run.includes('npm ci'),
      'publish-ui.yml uses npm ci for reference'
    );
  });

  test('runs on same runner as publish-ui.yml (ubuntu-latest)', () => {
    assert.equal(
      getWorkflow().jobs.chromatic['runs-on'],
      publishWorkflow.jobs['publish-ui']['runs-on'],
      `Runner differs: chromatic=${getWorkflow().jobs.chromatic['runs-on']}, publish=${publishWorkflow.jobs['publish-ui']['runs-on']}`
    );
  });
});

describe('chromatic.yml — File structure', () => {
  test('workflow file exists at expected path', () => {
    assert.ok(
      fs.existsSync(WORKFLOW_PATH),
      `Workflow file not found at ${WORKFLOW_PATH}`
    );
  });

  test('workflow file is a .yml (not .yaml) to match GitHub Actions convention', () => {
    assert.ok(
      WORKFLOW_PATH.endsWith('.yml'),
      'Workflow should use .yml extension'
    );
  });

  test('workflow file is in .github/workflows/ directory', () => {
    assert.ok(
      WORKFLOW_PATH.includes('.github/workflows/'),
      'Workflow should be in .github/workflows/ directory'
    );
  });
});
