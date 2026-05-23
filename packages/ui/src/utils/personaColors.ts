/**
 * Persona ID → hex color map for visual identification of subagent activity
 * across chat bubbles, the subagent activity feed, and the tool timeline.
 *
 * Tracks the CLI's persona color scheme in `pkg/console/persona_style.go`
 * with two divergences for theme legibility: the CLI uses white for
 * `orchestrator` (high contrast on the always-dark terminal), and dim gray
 * for `general` and unknowns — both invisible on a light WebUI theme. Here
 * they use amber and a darker neutral so badges read on both themes.
 *
 * Single source of truth — previously duplicated in
 * `webui/src/components/chat/SubagentActivityFeed.tsx`. SP-053-1a.
 */
export const PERSONA_COLORS: Record<string, string> = {
  coder: '#58a6ff',
  reviewer: '#d2a8ff',
  code_reviewer: '#d2a8ff',
  tester: '#7ee787',
  debugger: '#f0883e',
  refactor: '#79c0ff',
  researcher: '#ff7b72',
  orchestrator: '#d29922',
  executive_assistant: '#58a6ff',
  general: '#6e7681',
};

/** Fallback color for unknown personas — neutral mid-gray, readable on both
 *  the dark and light WebUI themes. */
const DEFAULT_PERSONA_COLOR = '#6e7681';

/**
 * Look up a persona's display color. Unknown / empty / undefined personas
 * return the dim-gray fallback so the caller never has to null-check.
 */
export function getPersonaColor(persona?: string): string {
  if (!persona) return DEFAULT_PERSONA_COLOR;
  return PERSONA_COLORS[persona] || DEFAULT_PERSONA_COLOR;
}
