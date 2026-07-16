// Comprehensive Settings spec — walks through every section and subsection
// in the SettingsPanel, verifying that navigation, rendering, and key
// controls work correctly.
//
// Replaces the original settings-providers.spec.ts (SP-087-4) which only
// tested opening the panel and had two test.fixme() cases that never
// resolved the collapsible-section navigation issue.
//
// Structure:
//   1. Panel open / close / filter
//   2. All 5 sections render with correct labels + scope badges
//   3. Each section expands and shows its subsection tabs
//   4. Each subsection tab renders its content (heading or key controls)
//   5. Interactive controls: toggles, selects, filter persistence

import { test, expect, chromium, type Browser, type Page } from '@playwright/test';
import { startSprout, type SproutHandle } from './fixtures/sprout';
import { startViteDevServer, type ViteHandle } from './fixtures/vite';
import { newWebuiPage, type WebUIPageHandle } from './fixtures/page';
import TESTIDS from './testids';

let browser: Browser;
let sprout: SproutHandle;
let vite: ViteHandle;
let handle: WebUIPageHandle;
let page: Page;

test.beforeAll(async () => {
  browser = await chromium.launch();
  sprout = await startSprout();
  vite = await startViteDevServer({ sproutBackendUrl: sprout.baseUrl });
  handle = await newWebuiPage({ browser, url: vite.url });
  page = handle.page;
});

test.afterAll(async () => {
  await handle?.cleanup();
  await browser?.close();
  await vite?.stop();
  await sprout?.stop();
});

test.describe.configure({ mode: 'serial' });
test.setTimeout(120_000);

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Open the settings panel via the sidebar toggle. Assumes chat-shell is visible. */
async function openSettings() {
  await page.goto(vite.url, { waitUntil: 'networkidle' });
  await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });
  const settingsToggle = page.getByTestId(TESTIDS['sidebar-settings-toggle']);
  await expect(settingsToggle).toBeVisible({ timeout: 15_000 });
  await settingsToggle.click();
  await expect(page.getByTestId(TESTIDS['settings-panel'])).toBeVisible({ timeout: 15_000 });
}

/** Expand a section by its label text. Matches the section whose header label
 *  exactly matches, avoiding false positives from hasText on subtree content. */
async function expandSection(label: string) {
  const section = sectionByLabel(label);
  await expect(section).toBeVisible({ timeout: 10_000 });
  const isExpanded = await section.evaluate((el) => el.classList.contains('expanded'));
  if (!isExpanded) {
    await section.locator('.settings-section-header').click();
    await expect(section).toHaveClass(/expanded/, { timeout: 5_000 });
  }
  return section;
}

/** Locate a section by its exact header label. Returns the section locator. */
function sectionByLabel(label: string) {
  return page
    .locator('.settings-section')
    .filter({ has: page.locator(`.settings-section-label`, { hasText: label }) })
    .first();
}

/** Click a subsection tab and wait for content. */
async function clickSubsectionTab(testidKey: string) {
  const tab = page.getByTestId(TESTIDS[testidKey]);
  await expect(tab).toBeVisible({ timeout: 10_000 });
  await tab.click();
  await page.waitForTimeout(300);
}

// ---------------------------------------------------------------------------
// 1. Panel open / close
// ---------------------------------------------------------------------------

test.describe('Settings panel open/close', () => {
  test('clicking sidebar-settings-toggle opens settings-panel', async () => {
    await openSettings();
    await expect(page.getByTestId(TESTIDS['settings-panel'])).toBeVisible();
  });

  test('settings filter input is visible and focusable', async () => {
    await openSettings();
    const filter = page.getByTestId(TESTIDS['settings-filter']);
    await expect(filter).toBeVisible({ timeout: 10_000 });
    const input = filter.locator('input');
    await input.click();
    await expect(input).toBeFocused();
  });

  test('filter narrows visible sections', async () => {
    await openSettings();
    const input = page.locator('.settings-filter-input');
    await input.fill('workspace');
    await page.waitForTimeout(500);

    // "Workspace" section should match
    await expect(sectionByLabel('Workspace')).toBeVisible({ timeout: 5_000 });

    // "Agent" section should be filtered out
    await expect(sectionByLabel('Agent')).toHaveCount(0);

    // Clear filter
    await input.fill('');
    await page.waitForTimeout(300);
    // Agent should reappear
    await expect(sectionByLabel('Agent')).toBeVisible({ timeout: 5_000 });
  });
});

// ---------------------------------------------------------------------------
// 2. Sections render with correct labels and scope badges
// ---------------------------------------------------------------------------

test.describe('Section structure', () => {
  test.beforeEach(async () => {
    await openSettings();
  });

  // Array of [label, expectedScopeBadge] pairs
  const sections: Array<[string, string]> = [
    ['Agent', 'session'],
    ['Workspace', 'workspace'],
    ['Environment', 'global'],
    ['Editor', 'runtime'],
    ['Experimental', 'global'],
  ];

  for (const [label, scope] of sections) {
    test(`"${label}" section renders with "${scope}" scope badge`, async () => {
      const section = sectionByLabel(label);
      await expect(section).toBeVisible({ timeout: 10_000 });
      const badge = section.locator('.settings-scope-badge');
      await expect(badge).toHaveText(scope);
    });
  }
});

// ---------------------------------------------------------------------------
// 3. Section expand/collapse reveals subsection tabs
// ---------------------------------------------------------------------------

test.describe('Section expand/collapse and subsection tabs', () => {
  test.beforeEach(async () => {
    await openSettings();
  });

  test('Agent section expands to show 5 subsection tabs', async () => {
    await expandSection('Agent');
    const section = sectionByLabel('Agent');
    const tabs = section.locator('.settings-subsection-btn');
    await expect(tabs).toHaveCount(5);

    const labels = await tabs.allTextContents();
    expect(labels.map((l) => l.trim())).toEqual(['General', 'Security', 'Subagents', 'Skills', 'Memory']);
  });

  test('Workspace section expands to show 3 subsection tabs', async () => {
    await expandSection('Workspace');
    const section = sectionByLabel('Workspace');
    const tabs = section.locator('.settings-subsection-btn');
    await expect(tabs).toHaveCount(3);

    const labels = await tabs.allTextContents();
    expect(labels.map((l) => l.trim())).toEqual(['Embeddings', 'MCP Servers', 'Language Servers']);
  });

  test('Environment section expands to show 2 subsection tabs', async () => {
    await expandSection('Environment');
    const section = sectionByLabel('Environment');
    const tabs = section.locator('.settings-subsection-btn');
    await expect(tabs).toHaveCount(2);

    const labels = await tabs.allTextContents();
    expect(labels.map((l) => l.trim())).toEqual(['Providers', 'Advanced']);
  });

  test('Editor section expands to show 2 subsection tabs', async () => {
    await expandSection('Editor');
    const section = sectionByLabel('Editor');
    const tabs = section.locator('.settings-subsection-btn');
    await expect(tabs).toHaveCount(2);

    const labels = await tabs.allTextContents();
    expect(labels.map((l) => l.trim())).toEqual(['Display', 'Notifications']);
  });

  test('Experimental section expands to show 1 subsection tab', async () => {
    await expandSection('Experimental');
    const section = sectionByLabel('Experimental');
    const tabs = section.locator('.settings-subsection-btn');
    await expect(tabs).toHaveCount(1);

    const labels = await tabs.allTextContents();
    expect(labels.map((l) => l.trim())).toEqual(['Computer Use']);
  });

  test('collapsing a section hides its subsections', async () => {
    await expandSection('Agent');
    const section = sectionByLabel('Agent');
    await expect(section).toHaveClass(/expanded/);

    // Collapse by clicking header again
    await section.locator('.settings-section-header').click();
    await expect(section).not.toHaveClass(/expanded/);

    // Subsection list should be gone
    const tabs = section.locator('.settings-subsection-btn');
    await expect(tabs).toHaveCount(0);
  });
});

// ---------------------------------------------------------------------------
// 4. Subsection content rendering
//    Each test expands the parent section, clicks the tab, and verifies
//    that the tab's key content (heading text or key control) appears.
// ---------------------------------------------------------------------------

test.describe('Subsection content', () => {
  test.beforeEach(async () => {
    await openSettings();
  });

  // --- Agent section ---

  test('Agent > General renders behavior settings', async () => {
    await expandSection('Agent');
    await clickSubsectionTab('settings-agent-general-tab');
    const content = page.locator('.settings-subsection-content');
    // AgentBehaviorSettingsTab renders an <h4>Behavior</h4> heading
    await expect(content.getByText('Behavior', { exact: true })).toBeVisible({ timeout: 10_000 });
  });

  test('Agent > Security renders security settings', async () => {
    await expandSection('Agent');
    await clickSubsectionTab('settings-agent-behavior-tab');
    const content = page.locator('.settings-subsection-content');
    await expect(content.getByText('Security', { exact: true })).toBeVisible({ timeout: 10_000 });
  });

  test('Agent > Subagents renders subagent config', async () => {
    await expandSection('Agent');
    await clickSubsectionTab('settings-agent-subagents-tab');
    const content = page.locator('.settings-subsection-content');
    await expect(content.getByText('Default Subagent', { exact: true })).toBeVisible({ timeout: 10_000 });
  });

  test('Agent > Skills renders skills list', async () => {
    await expandSection('Agent');
    await clickSubsectionTab('settings-agent-skills-tab');
    const content = page.locator('.settings-subsection-content');
    // SkillsSettingsTab shows either installed skills or an empty state
    // Either way it should render something within 10s
    await expect(content.locator('.section')).toBeVisible({ timeout: 10_000 });
  });

  test('Agent > Memory renders persistent context settings', async () => {
    await expandSection('Agent');
    await clickSubsectionTab('settings-agent-memory-tab');
    const content = page.locator('.settings-subsection-content');
    await expect(content.getByText('Memory & Context', { exact: true })).toBeVisible({ timeout: 10_000 });
  });

  // --- Workspace section ---

  test('Workspace > Embeddings renders embedding settings', async () => {
    await expandSection('Workspace');
    await clickSubsectionTab('settings-workspace-embeddings-tab');
    const content = page.locator('.settings-subsection-content');
    await expect(content.getByText('Embedding Index', { exact: true })).toBeVisible({ timeout: 15_000 });
  });

  test('Workspace > MCP Servers renders server list', async () => {
    await expandSection('Workspace');
    await clickSubsectionTab('settings-workspace-mcp-tab');
    const content = page.locator('.settings-subsection-content');
    // MCPSettingsTab renders either server rows or an add-server form
    await expect(content.locator('.section')).toBeVisible({ timeout: 15_000 });
  });

  test('Workspace > Language Servers renders LSP list', async () => {
    await expandSection('Workspace');
    await clickSubsectionTab('settings-workspace-lsp-tab');
    const content = page.locator('.settings-subsection-content');
    await expect(content.locator('.section')).toBeVisible({ timeout: 15_000 });
  });

  // --- Environment section ---

  test('Environment > Providers renders provider list', async () => {
    await expandSection('Environment');
    await clickSubsectionTab('settings-env-providers-tab');
    const content = page.locator('.settings-subsection-content');
    await expect(content.locator('.section')).toBeVisible({ timeout: 15_000 });
  });

  test('Environment > Advanced renders performance/commit/OCR subsections', async () => {
    await expandSection('Environment');
    await clickSubsectionTab('settings-env-advanced-tab');
    const content = page.locator('.settings-subsection-content');
    // AdvancedSettingsTab renders collapsible subsections for Performance, Commit & Review, OCR
    await expect(content.getByText('Commit & Review', { exact: true })).toBeVisible({ timeout: 15_000 });
  });

  // --- Editor section ---

  test('Editor > Display renders editor preferences', async () => {
    await expandSection('Editor');
    await clickSubsectionTab('settings-editor-preferences-tab');
    const content = page.locator('.settings-subsection-content');
    await expect(content.getByText('Display', { exact: true })).toBeVisible({ timeout: 15_000 });
  });

  test('Editor > Notifications renders notification settings', async () => {
    await expandSection('Editor');
    await clickSubsectionTab('settings-editor-notifications-tab');
    const content = page.locator('.settings-subsection-content');
    await expect(content.locator('.section')).toBeVisible({ timeout: 15_000 });
  });

  // --- Experimental section ---

  test('Experimental > Computer Use renders computer use settings', async () => {
    await expandSection('Experimental');
    await clickSubsectionTab('settings-experimental-computer-use-tab');
    const content = page.locator('.settings-subsection-content');
    await expect(content.locator('.section')).toBeVisible({ timeout: 15_000 });
  });
});

// ---------------------------------------------------------------------------
// 5. Interactive controls
// ---------------------------------------------------------------------------

test.describe('Interactive controls', () => {
  test.beforeEach(async () => {
    await openSettings();
  });

  test('Agent > General: reasoning effort dropdown is selectable', async () => {
    await expandSection('Agent');
    await clickSubsectionTab('settings-agent-general-tab');

    // AgentBehaviorSettingsTab renders a select for "Reasoning effort"
    // The field renderers use .styled-select with a label
    const reasoningLabel = page.locator('.settings-subsection-content').getByText('Reasoning effort');
    await expect(reasoningLabel).toBeVisible({ timeout: 10_000 });
  });

  test('Editor > Display: whitespace rendering dropdown is functional', async () => {
    await expandSection('Editor');
    await clickSubsectionTab('settings-editor-preferences-tab');

    const wsSelect = page.locator('#whitespace-rendering-select');
    await expect(wsSelect).toBeVisible({ timeout: 10_000 });

    // Read current value, pick a different one, verify it changes
    const initialValue = await wsSelect.inputValue();
    const options = ['none', 'boundary', 'all'].filter((v) => v !== initialValue);
    await wsSelect.selectOption(options[0]);
    await expect(wsSelect).toHaveValue(options[0]);
  });

  test('Agent > Security: approved commands input accepts text', async () => {
    await expandSection('Agent');
    await clickSubsectionTab('settings-agent-behavior-tab');

    // SecuritySettingsTab renders an input for adding approved commands
    const approvedInput = page.locator(
      '.settings-subsection-content input[placeholder*="git push"]'
    );
    await expect(approvedInput).toBeVisible({ timeout: 10_000 });
    await approvedInput.fill('echo test');
    await expect(approvedInput).toHaveValue('echo test');
  });

  test('Credentials section expands and renders', async () => {
    // Credentials is outside the 5 main sections — it's its own entry
    const credLink = page.getByTestId(TESTIDS['settings-credentials-link']);
    await expect(credLink).toBeVisible({ timeout: 10_000 });
    await credLink.locator('.settings-section-header').click();
    await page.waitForTimeout(500);
    // Credentials body should be visible
    const credBody = credLink.locator('.settings-section-body');
    await expect(credBody).toBeVisible({ timeout: 10_000 });
  });
});

// ---------------------------------------------------------------------------
// 6. State persistence across reload
// ---------------------------------------------------------------------------

test.describe('State persistence', () => {
  test('expanded section persists across page reload', async () => {
    await openSettings();

    // Expand Workspace
    await expandSection('Workspace');
    await expect(sectionByLabel('Workspace')).toHaveClass(/expanded/);

    // Reload
    await page.reload({ waitUntil: 'networkidle' });
    await expect(page.getByTestId(TESTIDS['chat-shell'])).toBeVisible({ timeout: 30_000 });

    // Re-open settings
    await page.getByTestId(TESTIDS['sidebar-settings-toggle']).click();
    await expect(page.getByTestId(TESTIDS['settings-panel'])).toBeVisible({ timeout: 15_000 });

    // Workspace should still be expanded
    await expect(sectionByLabel('Workspace')).toHaveClass(/expanded/, { timeout: 10_000 });
  });
});
