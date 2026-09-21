/**
 * DesignEmptyState — Design mode when the workspace has no `design/` tree.
 *
 * This is the mode's front door: instead of hiding Design until the tree
 * exists, an empty workspace gets an onboarding surface — a short intro and
 * starter cards, each of which seeds the agent chat with a ready-to-run
 * prompt (the same prefill handoff the loop results use: fill the input,
 * never auto-send). Imports from Figma and other tools are agent-mediated
 * via MCP; the cards describe the outcome, the agent does the work.
 *
 * Presentational only: the chat payload and the prefill handoff come from
 * the host (DesignSurface), which owns the side column.
 */

import { FilePlus2, FolderInput, Image, Palette, RefreshCw } from 'lucide-react';

export interface DesignEmptyStateProps {
  /** Seeds the agent chat input and flips to the Agent tab (never sends). */
  onAskAgent: (prompt: string) => void;
  /** Re-run the design-presence probe (a tree may have appeared). */
  onRecheck: () => void;
}

interface StarterCard {
  id: string;
  icon: typeof Image;
  title: string;
  body: string;
  prompt: string;
}

const STARTER_CARDS: StarterCard[] = [
  {
    id: 'figma',
    icon: Image,
    title: 'Import from Figma',
    body: 'Connect a Figma MCP server and the agent pulls frames, components, and styles into the design tree.',
    prompt:
      "I want to start this project's design tree from Figma. Help me connect a Figma MCP server (the mcp-setup skill has the steps), then import my Figma file's frames into design/ as screens and wireframes, and capture its colors and text styles as DTCG token files under design/tokens/. Ask me for the Figma file URL and which frames matter before you start.",
  },
  {
    id: 'tools',
    icon: FolderInput,
    title: 'Bring designs from another tool',
    body: "Sketch, Penpot, screenshots, or a spec document — point the agent at them and it drafts the tree in sprout's format.",
    prompt:
      'I have existing design material (exported screens, a deck, a spec doc) outside this project. Ask me where it lives and what it contains, then distill it into a design/ tree: a README manifest, DTCG tokens for the visual language, wireframes and flows for the main screens. Validate each artifact as you go.',
  },
  {
    id: 'draft',
    icon: FilePlus2,
    title: 'Draft a new project',
    body: 'Describe the product in a sentence or two and the agent runs the design loop: brief, tokens, wireframes, flows, screens.',
    prompt:
      'Start a design system for a new project. Interview me first: what the product does, who uses it, the main flows, and any brand direction. Then draft design/README.md (manifest with screens and frames), a starter DTCG token file, and wireframes + flows for the primary screens — running design_validate between steps.',
  },
];

export default function DesignEmptyState({ onAskAgent, onRecheck }: DesignEmptyStateProps) {
  return (
    <div className="design-empty" data-testid="design-empty-state">
      <header className="design-empty-header">
        <Palette size={28} aria-hidden="true" />
        <h1>Design mode</h1>
        <p>
          This workspace has no designs yet. Design mode is where the design tree lives — flows, screens, and tokens
          versioned beside the code — and where you and the agent build it together. Pick a way to start, or just talk
          to the agent on the right.
        </p>
      </header>
      <div className="design-empty-cards" role="list">
        {STARTER_CARDS.map((card) => {
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
