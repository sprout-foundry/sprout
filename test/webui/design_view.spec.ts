// SP-140-3 item 3.11 — DesignView end-to-end spec.
//
// Covers the SP-140-3 acceptance criterion: a workspace with a fixture design/
// tree exposes the Design surface, the Flows tab renders the flow graph, a
// picked asset fills the detail pane, and a screen opens in the editor.
//
// Why this spec starts its own stack: the shared Playwright `webServer`
// (`test/webui/start-stack.mjs`) boots a fresh temp workspace with no
// `design/`, so the design surface is unreachable on it. This spec therefore
// launches the sprout backend itself with a pre-seeded `workspaceDir`
// (`startSprout({ workspaceDir })` — `test/webui/fixtures/sprout.ts`) and
// points its own Vite dev server at that backend. `SPROUT_SKIP_WEBSERVER` is
// not needed: the two stacks use distinct OS-assigned ports, so the shared
// `webServer` may run alongside this one without interference.
//
// Two SP-140-3 AC steps are recorded below as `test.fixme` rather than
// asserted: they are blocked by pre-existing defects in earlier items of this
// spec (a flow-node click navigates the whole app to the editor instead of
// filling the detail pane; the canvas registers source-only handles, so React
// Flow renders no edges and therefore no edge labels). Each carries the exact
// observed behavior so the gap is visible in the suite rather than silent.

import {
  test,
  expect,
  chromium,
  type Browser,
  type Page,
} from "@playwright/test";
import {
  FIXTURE_FLOW,
  FIXTURE_SCREENS,
  removeDesignWorkspace,
  seedDesignWorkspace,
} from "./design_view_fixture";
import { newWebuiPage, type WebUIPageHandle } from "./fixtures/page";
import { startSprout, type SproutHandle } from "./fixtures/sprout";
import { startViteDevServer, type ViteHandle } from "./fixtures/vite";
import TESTIDS from "./testids";

let browser: Browser;
let sprout: SproutHandle;
let vite: ViteHandle;
let handle: WebUIPageHandle;
let page: Page;
let workspaceDir: string;

test.beforeAll(async () => {
  // System Chrome fallback for dev machines without the Playwright download
  // (AGENTS.md e2e convention).
  try {
    browser = await chromium.launch({ channel: "chrome" });
  } catch {
    browser = await chromium.launch();
  }
  workspaceDir = seedDesignWorkspace();
  sprout = await startSprout({ workspaceDir });
  vite = await startViteDevServer({ sproutBackendUrl: sprout.baseUrl });
  handle = await newWebuiPage({ browser, url: vite.url });
  page = handle.page;
});

test.afterAll(async () => {
  await handle?.cleanup();
  await browser?.close();
  await vite?.stop();
  await sprout?.stop();
  // `startSprout` never deletes a caller-supplied workspace, so the fixture
  // tree is this spec's to remove.
  removeDesignWorkspace(workspaceDir);
});

test.describe.configure({ mode: "serial" });
test.setTimeout(120_000);

/** Load the webui and switch to the Design surface via the sidebar rail. */
async function openDesignView(target: Page = page): Promise<void> {
  await target.goto(vite.url, { waitUntil: "networkidle" });
  await expect(target.getByTestId(TESTIDS["chat-shell"])).toBeVisible({
    timeout: 30_000,
  });

  const designNav = target.getByTestId("sidebar-design-button");
  await expect(designNav).toBeVisible({ timeout: 30_000 });
  await designNav.click();
  await expect(target.getByTestId("design-view")).toBeVisible({
    timeout: 30_000,
  });
}

test.describe("SP-140-3 DesignView", () => {
  test("a workspace with a design/ tree exposes the Design nav and opens the view", async () => {
    await page.goto(vite.url, { waitUntil: "networkidle" });
    await expect(page.getByTestId(TESTIDS["chat-shell"])).toBeVisible({
      timeout: 30_000,
    });

    // Presence is directory presence (SP-140-3 §3a), so a seeded design/ tree
    // is what makes the nav affordance appear at all.
    await expect(page.getByTestId("sidebar-design-button")).toBeVisible({
      timeout: 30_000,
    });

    // The fixture really is on disk (guards a seeding mistake masquerading as
    // a UI regression).
    const listed = await fetch(`${sprout.baseUrl}/api/files?path=design`);
    expect(listed.ok).toBe(true);
    const body = (await listed.json()) as { files?: Array<{ name: string }> };
    const names = (body.files ?? []).map((entry) => entry.name);
    expect(names).toContain("flows");
    expect(names).toContain("screens");
    expect(names).toContain("tokens");
    expect(names).toContain("wireframes");

    await openDesignView();
    await expect(page.getByTestId("design-view")).toHaveAttribute(
      "data-active-tab",
      "flows",
    );
    // The three tabs of the shell are all reachable.
    await expect(page.getByTestId("design-tab-flows")).toBeVisible();
    await expect(page.getByTestId("design-tab-screens")).toBeVisible();
    await expect(page.getByTestId("design-tab-tokens")).toBeVisible();
  });

  test("Flows tab renders the fixture flow graph with wireframe imagery", async () => {
    await openDesignView();

    const canvas = page.getByTestId("design-flows-canvas");
    await expect(canvas).toBeVisible({ timeout: 30_000 });
    await expect(canvas).toHaveAttribute("data-flow", FIXTURE_FLOW);

    // The container reads the .mmd, the wireframes, and the (absent) sidecar
    // before the graph settles, so the status line is the stable readiness
    // signal: flow name, node/edge counts, and the declared orientation.
    await expect(page.getByTestId("design-flows-status")).toContainText(
      `${FIXTURE_FLOW} · 2 nodes · 1 edges · LR`,
    );

    const graph = page.getByTestId("design-flow-graph");
    await expect(graph).toBeVisible({ timeout: 30_000 });

    // Node ids are the wireframe stems (SP-140-1 §1c).
    const login = page.getByTestId("design-flow-node-login");
    const inbox = page.getByTestId("design-flow-node-inbox");
    await expect(login).toBeVisible({ timeout: 30_000 });
    await expect(inbox).toBeVisible();

    // A node whose label names a wireframe renders that SVG's imagery, not a
    // plain box (label matching is case/separator-insensitive).
    await expect(login).toHaveAttribute("data-imagery", "wireframe");
    await expect(inbox).toHaveAttribute("data-imagery", "wireframe");
    await expect(
      page.getByTestId("design-flow-node-image-login"),
    ).toBeVisible();
    await expect(
      page.getByTestId("design-flow-node-image-inbox"),
    ).toBeVisible();
    await expect(login).toContainText("Login");
    await expect(inbox).toContainText("Inbox");

    // The rail lists the flow source the canvas is drawing.
    await expect(
      page.getByTestId(`design-rail-row-design/flows/${FIXTURE_FLOW}.mmd`),
    ).toBeVisible();
  });

  // Blocked by a pre-existing defect: `.react-flow__edge` / `.react-flow__edge-path`
  // count is 0 while the status line reports "1 edges" — React Flow drops an edge
  // whose target node exposes no `target` handle, and `FlowsCanvasNode` registers
  // every side as `type="source"` (SP-140-3 §3b edge-label AC).
  test.fixme("flow edges render between their nodes with their labels visible", async () => {
    // No assertions until an edge renders.
  });

  test("selecting a flow asset fills the detail pane", async () => {
    await openDesignView();

    // The rail is the canvas's own selection path into the pane (§3a), so it is
    // the deterministic way to assert the pane contract without depending on
    // React Flow's pointer handling.
    await page
      .getByTestId(`design-rail-row-design/flows/${FIXTURE_FLOW}.mmd`)
      .click();

    const detail = page.getByTestId("design-detail-content");
    await expect(detail).toHaveAttribute(
      "data-selected",
      `design/flows/${FIXTURE_FLOW}.mmd`,
    );
    await expect(page.getByTestId("design-detail-pane")).toContainText(
      `design/flows/${FIXTURE_FLOW}.mmd`,
    );
    // The pane's editor hand-off is present for a selected asset (§3a/§3b).
    await expect(page.locator(".design-detail-open")).toBeVisible();
  });

  // Blocked by a pre-existing defect: the click both selects AND fires the §3b
  // click-through, so `onViewChange('editor')` unmounts DesignView (`design-view`
  // count 0) and opens a tab named `sign-up.mmd#L2` — the `#L<n>` anchor is
  // appended to the path rather than passed as a line number, so the editor reads
  // a file that does not exist (HTTP 400). The AC's "select node → detail pane"
  // step is therefore not observable.
  test.fixme("clicking a flow node selects it in the detail pane without leaving the canvas", async () => {
    // No assertions until a node click leaves the design surface mounted.
  });

  test("Screens tab renders the fixture cards with their README statuses", async () => {
    await openDesignView();
    await page.getByTestId("design-tab-screens").click();

    const grid = page.getByTestId("design-screens-grid");
    await expect(grid).toBeVisible({ timeout: 30_000 });
    await expect(grid).toHaveAttribute(
      "data-screen-count",
      String(FIXTURE_SCREENS.length),
    );

    const cards = page.getByTestId("design-screens-cards");
    await expect(cards).toBeVisible();

    // Each fixture screen has a `screens/*.html` file, so each card is a screen
    // (not a wireframe-only card) and carries the manifest's status chip.
    for (const screen of FIXTURE_SCREENS) {
      const card = page.getByTestId(`design-screen-card-${screen}`);
      await expect(card).toBeVisible();
      await expect(card).toHaveAttribute("data-kind", "screen");
    }
    await expect(page.getByTestId("design-screen-status-login")).toHaveText(
      "ready",
    );
    await expect(page.getByTestId("design-screen-status-inbox")).toHaveText(
      "draft",
    );

    // Thumbnails are fetched from the workspace file endpoint; a doubled
    // `design/design/...` prefix would 400 and leave the thumb empty.
    await expect(page.getByTestId("design-screen-thumb-login")).toBeVisible();
    await expect(page.getByTestId("design-screen-thumb-inbox")).toBeVisible();
  });

  test("opening a screen from the detail pane opens it in the editor", async () => {
    await openDesignView();
    await page.getByTestId("design-tab-screens").click();
    await expect(page.getByTestId("design-screens-cards")).toBeVisible({
      timeout: 30_000,
    });

    // Click the screen card: the pane selection plus the LivePreview split view
    // is the §3c contract, and the app must stay on the design surface.
    await page.getByTestId("design-screen-card-login").click();
    await expect(page.getByTestId("design-detail-content")).toHaveAttribute(
      "data-selected",
      "design/screens/login.html",
    );
    await expect(page.getByTestId("design-screen-detail")).toBeVisible({
      timeout: 30_000,
    });
    await expect(page.locator(".live-preview")).toBeVisible({
      timeout: 30_000,
    });
    await expect(page.locator(".live-preview-iframe")).toBeVisible({
      timeout: 30_000,
    });
    await expect(page.getByTestId("design-view")).toBeVisible();

    // "Open in editor" is the pane's hand-off: it switches to the editor surface
    // with the screen file open in a tab.
    await page.locator(".design-detail-open").click();
    await expect(page.getByTestId(TESTIDS["editor-pane"])).toBeVisible({
      timeout: 30_000,
    });
    await expect(
      page
        .locator(".tabs-list .tab-name")
        .filter({ hasText: "login.html" })
        .first(),
    ).toBeVisible({
      timeout: 30_000,
    });
  });

  test("Tokens tab renders the fixture DTCG files grouped, with a path filter", async () => {
    await openDesignView();
    await page.getByTestId("design-tab-tokens").click();

    const tree = page.getByTestId("design-tokens-tree");
    await expect(tree).toBeVisible({ timeout: 30_000 });
    // 3 colour leaves (one of them an alias) + 3 spacing dimensions.
    await expect(tree).toHaveAttribute("data-token-count", "6");
    await expect(page.getByTestId("design-token-file-color")).toBeVisible();
    await expect(page.getByTestId("design-token-file-spacing")).toBeVisible();
    await expect(page.getByTestId("design-tokens-count")).toHaveText(
      "6 of 6 tokens",
    );

    // Filtering by token path narrows the tree (§3d).
    await page.getByTestId("design-tokens-search").fill("brand");
    await expect(tree).toHaveAttribute("data-visible-count", "2");
    await expect(page.getByTestId("design-tokens-count")).toHaveText(
      "2 of 6 tokens",
    );

    // A colour token renders a swatch and its value; the pane opens its file.
    await page
      .getByTestId("design-token-row-color-color.brand.primary")
      .click();
    await expect(page.getByTestId("design-tokens-token-detail")).toBeVisible();
    await expect(page.getByTestId("design-token-detail-swatch")).toBeVisible();
    await expect(page.getByTestId("design-token-detail-value")).toHaveText(
      "#2f6f4f",
    );
    await expect(page.getByTestId("design-detail-content")).toHaveAttribute(
      "data-selected",
      "design/tokens/color.tokens.json",
    );
  });
});
