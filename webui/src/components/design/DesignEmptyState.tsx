/**
 * DesignEmptyState — Design mode when the workspace has no recognized
 * `design/` tree.
 *
 * This is the mode's front door: instead of hiding Design until the tree
 * exists, an empty workspace gets an onboarding surface — a short intro and
 * starter cards, each of which seeds the agent chat with a ready-to-run
 * prompt (the same prefill handoff the loop results use: fill the input,
 * never auto-send). Every card's prompt opens with a question because the
 * agent starts by asking the one thing it needs (a URL, file paths, the
 * product idea) — it never guesses.
 *
 * Cards are ordered by workspace position: a workspace that already shows
 * frontend code sees its code-discovery cards first; a bare workspace sees
 * drafting and import cards first. A foreign `design/` folder (files that
 * don't match Sprout's tree) gets an explicit banner whose prompt instructs
 * the agent to inventory and ask before moving or overwriting anything.
 *
 * Presentational only: the chat payload and the prefill handoff come from
 * the host (DesignSurface), which owns the side column.
 */

import { Camera, FolderInput, Image, Puzzle, Search, Sprout, Palette, RefreshCw } from 'lucide-react';
export interface DesignEmptyStateProps {
  /** Seeds the agent chat input and flips to the Agent tab (never sends). */
  onAskAgent: (prompt: string) => void;
  /** Re-run the design-presence probe (a tree may have appeared). */
  onRecheck: () => void;
  /** True when the workspace listing shows frontend code (position signal). */
  frontendCode?: boolean;
  /** True when a design/ folder exists but holds nothing Sprout recognizes. */
  foreignTree?: boolean;
}

interface StarterCard {
  id: string;
  icon: typeof Search;
  title: string;
  body: string;
  prompt: string;
  /** Order weight when the workspace shows frontend code. */
  codeOrder: number;
  /** Order weight for a bare workspace. */
  freshOrder: number;
}

const STARTER_CARDS: StarterCard[] = [
  {
    id: 'discover-code',
    icon: Search,
    title: 'Discover the design in your code',
    body: 'Your components and styles already imply a visual language. The agent reads them and drafts the tree that mirrors the app.',
    prompt:
      'This workspace has frontend code but no design/ tree yet. Before reading anything, ask me which pages or components matter most. Then read those components and styles, distill the visual language into DTCG token files under design/tokens/, and draft design/README.md (manifest), wireframes, and flows that mirror what the app actually renders. Run design_validate after each artifact.',
    codeOrder: 0,
    freshOrder: 3,
  },
  {
    id: 'running-app',
    icon: Camera,
    title: 'Capture your running app',
    body: 'If the app runs, the agent screenshots real screens with its headless browser and drafts tokens, wireframes, and flows from what the app actually looks like.',
    prompt:
      'I want to start a design tree from my running app. Ask me for the URL (or how to start the dev server and which port it uses), then use analyze_ui_screenshot on each main page and distill what you see into design/ tokens, wireframes, and flows — asking me which pages to capture before you start.',
    codeOrder: 1,
    freshOrder: 4,
  },
  {
    id: 'images',
    icon: Image,
    title: 'Import images & whiteboards',
    body: 'Photos of whiteboards, exported mockups, paper sketches — the agent extracts wireframes, tokens, or flows from each image in the workspace.',
    prompt:
      'I have design material as images (whiteboard photos, exported mockups, sketches). Ask me where the images live in this workspace and what each one should become, then use design_import_sketch per image and write the extracted artifacts into the design/ tree, validating each one. If an image is not yet in the workspace, tell me to drop it in first.',
    codeOrder: 2,
    freshOrder: 1,
  },
  {
    id: 'figma',
    icon: Puzzle,
    title: 'Connect Figma (MCP)',
    body: 'The agent adds the Figma MCP server, asks you to paste a token (stored securely, never visible to it), then imports frames, components, and styles.',
    prompt:
      'I want to bring my Figma designs into this workspace. Set it up end to end: (1) use mcp_refresh (operation: add) to connect a Figma MCP server — pick the official HTTP endpoint or the npx stdio package; (2) when the server needs my Figma access token, ask me for it with ask_user (sensitive: true, credential_key: mcp/figma/FIGMA_TOKEN) so it goes straight to the credential store; (3) verify with mcp_refresh (operation: list) that the server is running and the credential is set; (4) then use mcp_tools to discover the Figma tools, ask me for the file URL and which frames matter, and import them into the design/ tree.',
    codeOrder: 3,
    freshOrder: 2,
  },
  {
    id: 'draft',
    icon: Sprout,
    title: 'Start from an idea',
    body: 'No code, no assets — just describe the product. The agent interviews you first, then runs the loop: brief, tokens, wireframes, flows, screens.',
    prompt:
      'Start a design system for a new project in this workspace. Interview me first: ask me what the product does, who uses it, the main flows, and any brand direction. Then draft design/README.md (manifest with screens and frames), a starter DTCG token file, and wireframes + flows for the primary screens — running design_validate between steps.',
    codeOrder: 4,
    freshOrder: 0,
  },
];

const FOREIGN_BANNER_PROMPT =
  "This workspace has a design/ folder whose contents do not follow Sprout's tree layout (tokens/, wireframes/, screens/, flows/, brand/, icons/, feedback/, README.md manifest). Before anything else, inventory what is actually in design/ and ask me what it is and whether I want it kept, moved, or woven into a new tree. Do not move, rename, overwrite, or delete anything without my explicit say-so.";

export default function DesignEmptyState({
  onAskAgent,
  onRecheck,
  frontendCode = false,
  foreignTree = false,
}: DesignEmptyStateProps) {
  const cards = [...STARTER_CARDS].sort((a, b) =>
    frontendCode ? a.codeOrder - b.codeOrder : a.freshOrder - b.freshOrder,
  );

  return (
    <div className="design-empty" data-testid="design-empty-state">
      <header className="design-empty-header">
        <Palette size={28} aria-hidden="true" />
        <h1>Design mode</h1>
        <p>
          {foreignTree
            ? 'This workspace has a design/ folder, but its contents don&apos;t follow Sprout&apos;s tree (tokens, wireframes, screens, flows). Nothing was touched — pick a way forward, or ask the agent to take a look first.'
            : 'This workspace has no designs yet. Design mode is where the design tree lives — flows, screens, and tokens versioned beside the code — and where you and the agent build it together. Pick a way to start, or just talk to the agent on the right.'}
        </p>
      </header>
      {foreignTree && (
        <button
          type="button"
          className="design-empty-foreign-banner"
          data-testid="design-empty-foreign"
          onClick={() => onAskAgent(FOREIGN_BANNER_PROMPT)}
        >
          <FolderInput size={16} aria-hidden="true" />
          <span>
            <strong>Ask the agent to look at the existing design/ folder</strong> — it will inventory what&apos;s there
            and ask before moving or changing anything.
          </span>
        </button>
      )}
      <div className="design-empty-cards" role="list">
        {cards.map((card) => {
          const Icon = card.icon;
          return (
            <button
              key={card.id}
              type="button"
              role="listitem"
              className="design-empty-card"
              data-testid={`design-empty-card-${card.id}`}
              onClick={() => onAskAgent(card.prompt)}
            >
              <span className="design-empty-card-icon" aria-hidden="true">
                <Icon size={20} />
              </span>
              <span className="design-empty-card-title">{card.title}</span>
              <span className="design-empty-card-body">{card.body}</span>
            </button>
          );
        })}
      </div>
      <footer className="design-empty-footer">
        <button type="button" className="design-empty-recheck" data-testid="design-empty-recheck" onClick={onRecheck}>
          <RefreshCw size={14} aria-hidden="true" />
          Check again
        </button>
        <span className="design-empty-hint">Created a design/ tree elsewhere? Check again and it appears here.</span>
      </footer>
    </div>
  );
}
