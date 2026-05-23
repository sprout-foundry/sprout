/**
 * Persona ID → hex color map for visual identification of subagent activity
 * across chat bubbles, the subagent activity feed, and the tool timeline.
 *
 * Companion to the CLI palette in `pkg/console/persona_style.go`. The two
 * surfaces necessarily use different color systems — CLI ships 8-color
 * ANSI codes that the terminal theme interprets, WebUI ships GitHub-themed
 * hex values that render verbatim — so exact color matching across both
 * isn't possible. Semantic class (warm/cool/neutral, distinctness from
 * neighbors) is what stays aligned. Update both files together when
 * reassigning a persona.
 *
 * Two WebUI-specific reassignments vs the original CLI choices:
 *   - `orchestrator` uses amber `#d29922` (was `#ffffff`) — white was
 *     invisible on the light theme.
 *   - `general` and the fallback use `#6e7681` (was `#8b949e`) — the
 *     lighter gray washed out on white backgrounds.
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
