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
// Two SP-140-3 AC steps were previously recorded as `test.fixme` because of
// defects in item 3.5's canvas: node-side handles were all registered as
// `type="source"` (so React Flow drew no edges and no edge labels), and a node
// click fired the editor hand-off, which unmounted the canvas and opened a
// `<path>#L<n>` file that does not exist. Both are fixed and asserted below;
// the edge-click hand-off carries its line as a number rather than a path
// fragment. Also covered here: drag persistence (AC 2) and the lazy chunk
// (AC 5).

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

/**
 * Load the webui and switch to the Design surface via the top-left mode
 * switcher (SP-140-5: Design is a workspace mode, not a rail icon).
 */
async function openDesignView(target: Page = page): Promise<void> {
  await target.goto(vite.url, { waitUntil: "networkidle" });

  // The active mode persists per instance, so a reload inside this serial run
  // may come back already in Design. Wait for the shell to mount either way,
  // then switch only if we are not already there.
  const designView = target.getByTestId("design-view");
  const codeShell = target.getByTestId(TESTIDS["chat-shell"]);
  await expect(designView.or(codeShell).first()).toBeVisible({
    timeout: 30_000,
  });

  if (!(await designView.isVisible())) {
    await switchToDesignMode(target);
  }

  await expect(designView).toBeVisible({ timeout: 30_000 });
}

/** Pick the Design option in the top-left mode switcher. */
async function switchToDesignMode(target: Page = page): Promise<void> {
  const trigger = target.getByTestId(TESTIDS["sidebar-brand-trigger"]);
  await expect(trigger).toBeVisible({ timeout: 30_000 });
  await trigger.click();
  const designOption = target.getByTestId(
    TESTIDS["sidebar-brand-option-design"],
  );
  await expect(designOption).toBeVisible({ timeout: 30_000 });
  await designOption.click();
}

test.describe("SP-140-3 DesignView", () => {
  test("a workspace with a design/ tree exposes the Design mode and opens the view", async () => {
    await page.goto(vite.url, { waitUntil: "networkidle" });
    await expect(page.getByTestId(TESTIDS["chat-shell"])).toBeVisible({
      timeout: 30_000,
    });

    // Presence is directory presence (SP-140-3 §3a), so a seeded design/ tree
    // is what offers the Design mode at all (top-left switcher, SP-140-5).
    await page.getByTestId(TESTIDS["sidebar-brand-trigger"]).click();
    await expect(
      page.getByTestId(TESTIDS["sidebar-brand-option-design"]),
    ).toBeVisible({ timeout: 30_000 });
    await page.keyboard.press("Escape");

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
    // The mode's rail (in the sidebar) lists the surface's sections.
    await expect(page.getByTestId(TESTIDS["design-rail-flows"])).toBeVisible();
    await expect(
      page.getByTestId(TESTIDS["design-rail-screens"]),
    ).toBeVisible();
    await expect(page.getByTestId(TESTIDS["design-rail-tokens"])).toBeVisible();
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

  test("flow edges render between their nodes with their labels visible", async () => {
    await openDesignView();
    await expect(page.getByTestId("design-flow-graph")).toBeVisible({
      timeout: 30_000,
    });

    // React Flow drops an edge whose target node exposes no `target` handle, so
    // a graph with nodes but no `.react-flow__edge` is the signature of the
    // handle-registration defect this asserts against.
    const edges = page.locator(".react-flow__edge");
    await expect(edges).toHaveCount(1, { timeout: 30_000 });

    // The edge carries a real path with an arrowhead, and the label reaches the
    // DOM. (A straight `LR` connection is a zero-height path, which Playwright's
    // visibility heuristic rejects as "hidden" — so assert the path's geometry
    // and its label rather than the element's box.)
    const path = page.locator(".react-flow__edge-path").first();
    await expect(path).toHaveAttribute("d", /^M\d+/);
    await expect(path).toHaveAttribute("marker-end", /arrowclosed/);

    const label = page.locator(".react-flow__edge-textwrapper").first();
    await expect(label).toBeVisible({ timeout: 30_000 });
    await expect(label).toContainText("tap Submit");

    // An edge that renders must not sit on top of its nodes: dagre must lay out
    // against the rendered (wireframe-derived) dimensions, not the defaults, or
    // a tall node overlaps its neighbour. Assert the boxes are disjoint.
    const login = await page
      .getByTestId("design-flow-node-login")
      .boundingBox();
    const inbox = await page
      .getByTestId("design-flow-node-inbox")
      .boundingBox();
    if (!login || !inbox) throw new Error("flow nodes have no bounding box");
    const disjoint =
      login.x + login.width <= inbox.x || inbox.x + inbox.width <= login.x;
    expect(disjoint).toBe(true);
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

  test("clicking a flow node selects it in the detail pane without leaving the canvas", async () => {
    await openDesignView();
    await expect(page.getByTestId("design-flow-node-login")).toBeVisible({
      timeout: 30_000,
    });

    await page.getByTestId("design-flow-node-login").click();

    // The click fills the pane with the node's owning flow and stays put: it
    // must NOT fire the editor hand-off, which unmounted the canvas and opened
    // a nonexistent `<path>#L<n>` file.
    await expect(page.getByTestId("design-view")).toBeVisible();
    await expect(page.getByTestId("design-detail-content")).toHaveAttribute(
      "data-selected",
      `design/flows/${FIXTURE_FLOW}.mmd`,
    );
    await expect(page.getByTestId("design-flows-canvas")).toBeVisible();
  });

  test("the node-click editor hand-off passes the line separately, not as a path fragment", async () => {
    await openDesignView();
    await expect(page.getByTestId("design-flow-node-login")).toBeVisible({
      timeout: 30_000,
    });

    // An edge click is the canvas's explicit source hand-off (§3b click-through).
    await page.locator(".react-flow__edge").first().click();
    await expect(page.getByTestId(TESTIDS["editor-pane"])).toBeVisible({
      timeout: 30_000,
    });

    // The flow source is open and no tab is named after a `#L` fragment — the
    // anchor travels as a line number, so the opened path is the real file.
    await expect(
      page
        .locator(".tabs-list .tab-name")
        .filter({ hasText: `${FIXTURE_FLOW}.mmd` })
        .first(),
    ).toBeVisible({ timeout: 30_000 });
    await expect(
      page.locator(".tabs-list .tab-name").filter({ hasText: "#L" }),
    ).toHaveCount(0);
  });

  test("drag persistence writes the sidecar with a derivedFrom hash", async () => {
    await openDesignView();
    const node = page.getByTestId("design-flow-node-login");
    await expect(node).toBeVisible({ timeout: 30_000 });

    const box = await node.boundingBox();
    if (!box) throw new Error("flow node has no bounding box");
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(
      box.x + box.width / 2 + 60,
      box.y + box.height / 2 + 40,
    );
    await page.mouse.up();

    // The sidecar is the canvas's only write (SP-140 invariant 2): positions
    // are derived state, and `derivedFrom` is the `.mmd` content hash that
    // makes hash drift detectable (SP-140-3 AC 2).
    await expect
      .poll(
        async () => {
          const res = await fetch(
            `${sprout.baseUrl}/api/file?path=design/flows/${FIXTURE_FLOW}.layout.json`,
          );
          return res.ok ? await res.text() : "";
        },
        { timeout: 30_000 },
      )
      .toContain("derivedFrom");
  });

  test("Screens tab renders the fixture cards with their README statuses", async () => {
    await openDesignView();
    await page.getByTestId(TESTIDS["design-rail-screens"]).click();

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
    await page.getByTestId(TESTIDS["design-rail-screens"]).click();
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
    await page.getByTestId(TESTIDS["design-rail-tokens"]).click();

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
