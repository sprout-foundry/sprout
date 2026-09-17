// SP-140-3 item 3.11 — fixture design/ tree for the DesignView e2e spec.
//
// The standard `start-stack.mjs` webServer boots a fresh temp workspace with no
// `design/`, so a spec that needs the design surface must start its own stack
// with a pre-seeded `workspaceDir` (`test/webui/fixtures/sprout.ts`
// `StartSproutOptions`). This module builds that workspace.
//
// Everything here is synthetic per the repo's user-data hygiene rule (public
// repo: no real sessions, paths, domains, or user data). The tree is the
// smallest one that exercises every DesignView tab:
//
//   README.md            frames: block + screen/flow status listings (SP-140-1 §1e)
//   wireframes/*.svg     node imagery, viewBox matching a declared frame
//   flows/*.mmd          screen flow, node ids == wireframe stems (SP-140-1 §1c)
//   screens/*.html       self-contained screens for the Screens tab/LivePreview
//   tokens/*.tokens.json DTCG leaves with a colour, a dimension, and an alias
//
// Kept spec-local on purpose: other e2e specs run against the shared
// no-design/ stack, and a design tree in the shared fixtures dir would invite
// accidental reuse from specs that do not want one.

import fs from "node:fs";
import os from "node:os";
import path from "node:path";

/** The device frames the fixture manifest declares, and the wireframes use. */
export const FIXTURE_FRAMES = {
  desktop: { width: 1440, height: 900 },
  mobile: { width: 390, height: 844 },
};

/** Screen stems in the fixture: each has a wireframe, a screen, and a status. */
export const FIXTURE_SCREENS = ["login", "inbox"] as const;

/** The flow's file stem — the `.mmd` name the canvas reports as `data-flow`. */
export const FIXTURE_FLOW = "sign-up";

/** Token file stems under `design/tokens/`. */
export const FIXTURE_TOKEN_FILES = ["color", "spacing"] as const;

function wireframeSvg(label: string, height: number, nav?: string): string {
  const navAttr = nav ? ` data-nav="${nav}"` : "";
  return [
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${FIXTURE_FRAMES.mobile.width} ${height}">`,
    `<text x="24" y="64" font-size="28">${label}</text>`,
    `<rect id="field" x="24" y="140" width="342" height="48"/>`,
    `<rect id="submit" x="24" y="200" width="342" height="52"${navAttr}/>`,
    "</svg>",
    "",
  ].join("\n");
}

function screenHtml(title: string, nav?: string): string {
  const navAttr = nav ? ` data-nav="${nav}"` : "";
  return [
    "<!doctype html>",
    '<html lang="en">',
    "<head>",
    '<meta charset="utf-8">',
    `<title>${title}</title>`,
    "<style>",
    "body { margin: 0; font-family: system-ui, sans-serif; }",
    ".card { width: 342px; margin: 24px; }",
    "</style>",
    "</head>",
    "<body>",
    `<div class="card"><h1>${title}</h1><button type="button"${navAttr}>Continue</button></div>`,
    "</body>",
    "</html>",
    "",
  ].join("\n");
}

/**
 * The fixture manifest: a machine-parseable `frames:` block plus the screen and
 * flow listings the tabs read their status chips from (SP-140-1 §1e).
 *
 * The declared frames are device frames (`desktop`, `mobile`) — the shape the
 * charter specifies. A screen is sized by a frame only when the frame's name
 * matches the screen stem, so the fixture's screens keep the neutral card
 * aspect while the frame block still exercises the manifest's `frames:` parse.
 */
const MANIFEST = [
  "# Design Workspace",
  "",
  "Synthetic fixture tree for the DesignView end-to-end spec.",
  "",
  "## Device frames",
  "",
  "```",
  "frames:",
  `  desktop: ${FIXTURE_FRAMES.desktop.width}x${FIXTURE_FRAMES.desktop.height}`,
  `  mobile: ${FIXTURE_FRAMES.mobile.width}x${FIXTURE_FRAMES.mobile.height}`,
  "```",
  "",
  "## Screens",
  "",
  "```",
  "- `login` — ready — sign-in entry point with credential recovery",
  "- `inbox` — draft — message list with unread badges",
  "```",
  "",
  "## Flows",
  "",
  "```",
  `- \`${FIXTURE_FLOW}\` — draft — account creation from landing to first run`,
  "```",
  "",
].join("\n");

/**
 * `login` → `inbox` on submit; node ids are wireframe stems (SP-140-1 §1c).
 *
 * Nodes are declared on their own lines before the connect statement: the
 * mermaid-subset parser drops the bracket label of the node on the right of a
 * connect operator (`a[A] --> b[B]` yields `b`'s label as its id), so this form
 * is the one that keeps both labels.
 */
const FLOW_MMD = [
  "flowchart LR",
  "  login[Login]",
  "  inbox[Inbox]",
  "  login -->|tap Submit| inbox",
  "",
].join("\n");

/** DTCG colour tier: a literal leaf plus an alias leaf (SP-140-1 §1a). */
const COLOR_TOKENS = JSON.stringify(
  {
    color: {
      $type: "color",
      brand: {
        primary: {
          $value: "#2f6f4f",
          $type: "color",
          $description: "Primary brand colour",
        },
        "on-primary": { $value: "{color.surface}", $type: "color" },
      },
      surface: { $value: "#ffffff", $type: "color" },
    },
  },
  null,
  2,
);

/** DTCG spacing tier: `dimension` leaves drive the spacing specimens (§3d). */
const SPACING_TOKENS = JSON.stringify(
  {
    space: {
      $type: "dimension",
      sm: { $value: "4px", $type: "dimension" },
      md: { $value: "8px", $type: "dimension" },
      lg: { $value: "16px", $type: "dimension" },
    },
  },
  null,
  2,
);

/** The whole synthetic tree, as design-root-relative POSIX paths → contents. */
export const FIXTURE_DESIGN_TREE: Record<string, string> = {
  "design/README.md": MANIFEST,
  "design/wireframes/login.svg": wireframeSvg(
    "Login",
    FIXTURE_FRAMES.mobile.height,
    "inbox",
  ),
  "design/wireframes/inbox.svg": wireframeSvg(
    "Inbox",
    FIXTURE_FRAMES.mobile.height,
  ),
  [`design/flows/${FIXTURE_FLOW}.mmd`]: FLOW_MMD,
  "design/screens/login.html": screenHtml("Login", "inbox"),
  "design/screens/inbox.html": screenHtml("Inbox"),
  "design/tokens/color.tokens.json": COLOR_TOKENS,
  "design/tokens/spacing.tokens.json": SPACING_TOKENS,
  // SP-140-1 §1h git contract: keeps `design_validate` clean if a later spec
  // runs it against this workspace; harmless for the DesignView assertions.
  ".gitattributes": "* text=auto eol=lf\ndesign/**/*.svg diff=html\n",
  ".gitignore": "node_modules/\ndesign/.cache/\n",
};

/**
 * Write the fixture tree into a fresh temp directory and return its path.
 *
 * The caller passes the result to `startSprout({ workspaceDir })`; `startSprout`
 * does not delete a caller-supplied workspace, so the spec owns cleanup
 * (see `removeDesignWorkspace`).
 */
export function seedDesignWorkspace(): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "sprout-design-view-"));
  for (const [rel, content] of Object.entries(FIXTURE_DESIGN_TREE)) {
    const target = path.join(dir, rel);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, content, "utf8");
  }
  return dir;
}

/** Remove a workspace created by `seedDesignWorkspace` (best effort). */
export function removeDesignWorkspace(dir: string | undefined): void {
  if (!dir) return;
  try {
    fs.rmSync(dir, { recursive: true, force: true, maxRetries: 3 });
  } catch {
    // A temp dir the OS will reap; failing the suite on cleanup is worse.
  }
}
