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

const BRAND = "#2f6f4f";

/**
 * The wireframes: the flow's node imagery. Structured like real low-fi
 * wireframes (device chrome, labeled blocks, one brand accent on the primary
 * action) rather than bare rectangles — they are what the canvas renders, and
 * the screens below implement them. `data-nav` wiring is the §4a contract:
 * login's primary action navigates to inbox, and the validator treats a
 * dangling target as a hard error.
 */
function loginWireframeSvg(): string {
  return [
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${FIXTURE_FRAMES.mobile.width} ${FIXTURE_FRAMES.mobile.height}" font-family="system-ui, sans-serif">`,
    '  <rect width="390" height="844" fill="#ffffff"/>',
    '  <rect x="0" y="0" width="390" height="44" fill="#fafbfa"/>',
    '  <text x="24" y="28" font-size="14" fill="#5b6b62">9:41</text>',
    '  <circle cx="23" cy="92" r="14" fill="#2f6f4f"/>',
    '  <text x="45" y="97" font-size="16" font-weight="600" fill="#1a2420">Sprout</text>',
    '  <text x="24" y="180" font-size="28" font-weight="700" fill="#1a2420">Welcome back</text>',
    '  <text x="24" y="208" font-size="14" fill="#5b6b62">Sign in to continue to your workspace.</text>',
    '  <text x="24" y="264" font-size="12" fill="#5b6b62">EMAIL</text>',
    '  <rect x="24" y="274" width="342" height="48" rx="12" fill="#f4f6f4" stroke="#e3e8e4"/>',
    '  <text x="40" y="303" font-size="14" fill="#9aa8a0">you@example.com</text>',
    '  <text x="24" y="352" font-size="12" fill="#5b6b62">PASSWORD</text>',
    '  <rect x="24" y="362" width="342" height="48" rx="12" fill="#f4f6f4" stroke="#e3e8e4"/>',
    '  <text x="40" y="391" font-size="14" fill="#9aa8a0">••••••••</text>',
    `  <rect id="submit" x="24" y="440" width="342" height="52" rx="12" fill="${BRAND}" data-nav="inbox"/>`,
    '  <text x="195" y="472" font-size="16" font-weight="600" fill="#ffffff" text-anchor="middle">Sign in</text>',
    '  <text x="195" y="524" font-size="13" fill="#2f6f4f" text-anchor="middle">Create an account</text>',
    "</svg>",
    "",
  ].join("\n");
}

function inboxWireframeSvg(): string {
  const row = (y: number, name: string, snippet: string, unread: boolean): string[] => [
    `  <circle cx="48" cy="${y + 28}" r="18" fill="#eef2ef"/>`,
    `  <text x="48" y="${y + 33}" font-size="13" font-weight="600" fill="#2f6f4f" text-anchor="middle">${name[0]}</text>`,
    `  <text x="78" y="${y + 24}" font-size="14" font-weight="${unread ? 600 : 400}" fill="#1a2420">${name}</text>`,
    `  <text x="78" y="${y + 44}" font-size="12" fill="#5b6b62">${snippet}</text>`,
    `  <text x="366" y="${y + 24}" font-size="11" fill="#9aa8a0" text-anchor="end">${unread ? "now" : "2h"}</text>`,
    ...(unread ? [`  <circle cx="370" cy="${y + 40}" r="4" fill="${BRAND}"/>`] : []),
    `  <line x1="24" y1="${y + 68}" x2="366" y2="${y + 68}" stroke="#eef1ee"/>`,
  ];
  return [
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${FIXTURE_FRAMES.mobile.width} ${FIXTURE_FRAMES.mobile.height}" font-family="system-ui, sans-serif">`,
    '  <rect width="390" height="844" fill="#ffffff"/>',
    '  <rect x="0" y="0" width="390" height="44" fill="#fafbfa"/>',
    '  <text x="24" y="28" font-size="14" fill="#5b6b62">9:41</text>',
    '  <text x="24" y="100" font-size="24" font-weight="700" fill="#1a2420">Inbox</text>',
    '  <circle cx="342" cy="92" r="16" fill="#2f6f4f"/>',
    '  <text x="342" y="97" font-size="16" fill="#ffffff" text-anchor="middle">+</text>',
    ...row(140, "Amara Okafor", "Render pass is ready for review", true),
    ...row(216, "Ben Ito", "Re: spacing tokens for the cards", true),
    ...row(292, "Carla Ruiz", "Sprint board updated", false),
    ...row(368, "Dev Anand", "Field study notes from Thursday", false),
    "</svg>",
    "",
  ].join("\n");
}

/**
 * The screens: self-contained, styled with the same values the fixture's DTCG
 * token files declare (brand #2f6f4f, surface #ffffff, space 4/8/16/24) — the
 * demo content should look like what the tool and its token pipeline produce.
 */
function loginScreenHtml(): string {
  return [
    "<!doctype html>",
    '<html lang="en">',
    "<head>",
    '<meta charset="utf-8">',
    '<meta name="viewport" content="width=device-width, initial-scale=1">',
    "<title>Sign in — Sprout</title>",
    "<style>",
    ":root {",
    "  --brand: #2f6f4f; --brand-ink: #ffffff; --surface: #ffffff;",
    "  --ink: #1a2420; --ink-soft: #5b6b62; --line: #e3e8e4; --field: #f4f6f4;",
    "  --radius: 12px;",
    "  --space-2: 8px; --space-3: 16px; --space-4: 24px; --space-5: 32px;",
    "}",
    "* { box-sizing: border-box; }",
    "body {",
    "  margin: 0;",
    "  font-family: -apple-system, 'Segoe UI', Roboto, sans-serif;",
    "  background: var(--surface); color: var(--ink);",
    "}",
    ".phone { max-width: 390px; min-height: 100vh; margin: 0 auto; display: flex; flex-direction: column; padding: var(--space-5) var(--space-4); }",
    ".brand { display: flex; align-items: center; gap: 10px; margin-top: var(--space-4); }",
    ".brand-mark { width: 28px; height: 28px; border-radius: 8px; background: var(--brand); }",
    ".brand-name { font-size: 16px; font-weight: 600; }",
    "h1 { font-size: 28px; line-height: 1.2; margin: 72px 0 8px; }",
    ".sub { margin: 0 0 var(--space-5); font-size: 14px; color: var(--ink-soft); }",
    "label { display: block; font-size: 12px; font-weight: 600; letter-spacing: 0.04em; color: var(--ink-soft); margin: var(--space-4) 0 var(--space-2); }",
    "input { width: 100%; height: 48px; padding: 0 var(--space-3); font-size: 15px; color: var(--ink); background: var(--field); border: 1px solid var(--line); border-radius: var(--radius); }",
    "input:focus { outline: none; border-color: var(--brand); background: var(--surface); }",
    "button { width: 100%; height: 52px; margin-top: var(--space-5); font-size: 16px; font-weight: 600; color: var(--brand-ink); background: var(--brand); border: none; border-radius: var(--radius); cursor: pointer; }",
    ".alt { margin: var(--space-3) 0 0; text-align: center; font-size: 13px; }",
    ".alt a { color: var(--brand); text-decoration: none; font-weight: 600; }",
    "</style>",
    "</head>",
    "<body>",
    '<main class="phone">',
    '  <div class="brand"><span class="brand-mark"></span><span class="brand-name">Sprout</span></div>',
    "  <h1>Welcome back</h1>",
    '  <p class="sub">Sign in to continue to your workspace.</p>',
    '  <form>',
    '    <label for="email">Email</label>',
    '    <input id="email" type="email" placeholder="you@example.com" autocomplete="email">',
    '    <label for="password">Password</label>',
    '    <input id="password" type="password" placeholder="••••••••" autocomplete="current-password">',
    '    <button type="button" data-nav="inbox">Sign in</button>',
    "  </form>",
    '  <p class="alt">New here? <a href="#">Create an account</a></p>',
    "</main>",
    "</body>",
    "</html>",
    "",
  ].join("\n");
}

function inboxScreenHtml(): string {
  const row = (name: string, initial: string, snippet: string, time: string, unread: boolean): string =>
    [
      '<li class="row">',
      `  <span class="avatar">${initial}</span>`,
      '  <span class="body">',
      `    <span class="top"><span class="who${unread ? " unread" : ""}">${name}</span><span class="time">${time}</span></span>`,
      `    <span class="snippet">${snippet}</span>`,
      "  </span>",
      ...(unread ? ['  <span class="dot"></span>'] : []),
      "</li>",
    ].join("");
  return [
    "<!doctype html>",
    '<html lang="en">',
    "<head>",
    '<meta charset="utf-8">',
    '<meta name="viewport" content="width=device-width, initial-scale=1">',
    "<title>Inbox — Sprout</title>",
    "<style>",
    ":root {",
    "  --brand: #2f6f4f; --surface: #ffffff; --ink: #1a2420; --ink-soft: #5b6b62;",
    "  --line: #eef1ee; --tint: #eef2ef; --radius: 12px;",
    "  --space-3: 16px; --space-4: 24px;",
    "}",
    "* { box-sizing: border-box; }",
    "body { margin: 0; font-family: -apple-system, 'Segoe UI', Roboto, sans-serif; background: var(--surface); color: var(--ink); }",
    ".phone { max-width: 390px; min-height: 100vh; margin: 0 auto; padding: var(--space-4); }",
    ".bar { display: flex; align-items: center; justify-content: space-between; margin-bottom: var(--space-3); }",
    "h1 { font-size: 24px; margin: 0; }",
    ".compose { width: 32px; height: 32px; border-radius: 50%; background: var(--brand); color: #fff; font-size: 18px; line-height: 32px; text-align: center; }",
    "ul { list-style: none; margin: 0; padding: 0; }",
    ".row { display: flex; gap: var(--space-3); padding: var(--space-3) 0; border-bottom: 1px solid var(--line); }",
    ".avatar { flex: 0 0 36px; height: 36px; border-radius: 50%; background: var(--tint); color: var(--brand); font-size: 13px; font-weight: 600; line-height: 36px; text-align: center; }",
    ".body { flex: 1; min-width: 0; }",
    ".top { display: flex; justify-content: space-between; gap: var(--space-3); }",
    ".who { font-size: 14px; font-weight: 500; }",
    ".who.unread { font-weight: 700; }",
    ".time { font-size: 11px; color: var(--ink-soft); }",
    ".snippet { display: block; margin-top: 2px; font-size: 12px; color: var(--ink-soft); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }",
    ".dot { flex: 0 0 8px; height: 8px; margin-top: 14px; border-radius: 50%; background: var(--brand); }",
    "</style>",
    "</head>",
    "<body>",
    '<main class="phone">',
    '  <div class="bar"><h1>Inbox</h1><span class="compose">+</span></div>',
    "  <ul>",
    row("Amara Okafor", "A", "Render pass is ready for review", "now", true),
    row("Ben Ito", "B", "Re: spacing tokens for the cards", "now", true),
    row("Carla Ruiz", "C", "Sprint board updated", "2h", false),
    row("Dev Anand", "D", "Field study notes from Thursday", "2h", false),
    "  </ul>",
    "</main>",
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
  "design/wireframes/login.svg": loginWireframeSvg(),
  "design/wireframes/inbox.svg": inboxWireframeSvg(),
  [`design/flows/${FIXTURE_FLOW}.mmd`]: FLOW_MMD,
  "design/screens/login.html": loginScreenHtml(),
  "design/screens/inbox.html": inboxScreenHtml(),
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
