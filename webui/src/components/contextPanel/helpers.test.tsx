/**
 * Tests for contextPanel/helpers
 *
 * Tests isSubagentTool, getSubagentPrompt, getToolIcon, getPersonaColor,
 * getStatusIcon, formatDuration, formatRelativeTime, formatTime,
 * formatDurationMs, formatTokens, formatCost.
 */

import { render } from '@testing-library/react';
import { act } from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';

// ── Mocks (before imports) ──────────────────────────────────────────

vi.mock('lucide-react', () => {
  const icons = [
    'Wrench',
    'Terminal',
    'BookOpen',
    'Pencil',
    'Search',
    'Eye',
    'FlaskConical',
    'Globe',
    'ArrowDown',
    'ClipboardList',
    'ScrollText',
    'RotateCcw',
    'Bot',
    'Rocket',
    'Zap',
    'CheckCircle2',
    'XCircle',
    'Hourglass',
  ];
  const result: Record<string, (props: any) => JSX.Element> = {};
  for (const name of icons) {
    result[name] = (props: any) => <svg data-testid={name.toLowerCase()} {...props} />;
  }
  return result;
});

vi.mock('../../utils/log', () => ({
  debugLog: vi.fn(),
}));

// ── Imports ──────────────────────────────────────────────────────────

import { debugLog } from '../../utils/log';
import {
  isSubagentTool,
  getSubagentPrompt,
  getToolIcon,
  getPersonaColor,
  getStatusIcon,
  formatDuration,
  formatRelativeTime,
  formatTime,
  formatDurationMs,
  formatTokens,
  formatCost,
} from './helpers';

// ── isSubagentTool ───────────────────────────────────────────────────

describe('isSubagentTool', () => {
  it('returns true for run_subagent', () => {
    expect(isSubagentTool({ tool: 'run_subagent' })).toBe(true);
  });

  it('returns true for run_parallel_subagents', () => {
    expect(isSubagentTool({ tool: 'run_parallel_subagents' })).toBe(true);
  });

  it('returns false for shell_command', () => {
    expect(isSubagentTool({ tool: 'shell_command' })).toBe(false);
  });

  it('returns false for read_file', () => {
    expect(isSubagentTool({ tool: 'read_file' })).toBe(false);
  });

  it('returns false for unknown tool', () => {
    expect(isSubagentTool({ tool: 'unknown_tool' })).toBe(false);
  });
});

// ── getSubagentPrompt ────────────────────────────────────────────────

describe('getSubagentPrompt', () => {
  it('returns prompt string when tool.arguments is valid JSON with prompt', () => {
    const tool = {
      tool: 'run_subagent',
      arguments: JSON.stringify({ prompt: 'Write tests' }),
    };
    expect(getSubagentPrompt(tool)).toBe('Write tests');
  });

  it('returns undefined when tool.arguments is undefined', () => {
    const tool = { tool: 'run_subagent' } as any;
    expect(getSubagentPrompt(tool)).toBeUndefined();
  });

  it('returns undefined when tool.arguments is invalid JSON', () => {
    const tool = { tool: 'run_subagent', arguments: '{not valid json' };
    expect(getSubagentPrompt(tool)).toBeUndefined();
    expect(debugLog).toHaveBeenCalled();
  });

  it('returns undefined when tool.arguments JSON has no prompt field', () => {
    const tool = { tool: 'run_subagent', arguments: JSON.stringify({ other: 'field' }) };
    expect(getSubagentPrompt(tool)).toBeUndefined();
  });

  it('returns undefined when tool.arguments JSON has prompt as non-string', () => {
    const tool = { tool: 'run_subagent', arguments: JSON.stringify({ prompt: 123 }) };
    expect(getSubagentPrompt(tool)).toBeUndefined();
  });

  it('returns undefined when tool.arguments is empty string', () => {
    const tool = { tool: 'run_subagent', arguments: '' };
    // Empty string is falsy so the function returns undefined at the first check
    expect(getSubagentPrompt(tool)).toBeUndefined();
  });

  it('returns undefined when tool.arguments JSON has null prompt', () => {
    const tool = { tool: 'run_subagent', arguments: JSON.stringify({ prompt: null }) };
    expect(getSubagentPrompt(tool)).toBeUndefined();
  });
});

// ── getToolIcon ──────────────────────────────────────────────────────

describe('getToolIcon', () => {
  it('returns non-null JSX for shell_command (Terminal)', () => {
    const { container } = render(<div>{getToolIcon('shell_command')}</div>);
    const icon = container.querySelector('[data-testid="terminal"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for read_file (BookOpen)', () => {
    const { container } = render(<div>{getToolIcon('read_file')}</div>);
    const icon = container.querySelector('[data-testid="bookopen"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for write_file (Pencil)', () => {
    const { container } = render(<div>{getToolIcon('write_file')}</div>);
    const icon = container.querySelector('[data-testid="pencil"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for edit_file (Pencil)', () => {
    const { container } = render(<div>{getToolIcon('edit_file')}</div>);
    const icon = container.querySelector('[data-testid="pencil"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for search_files (Search)', () => {
    const { container } = render(<div>{getToolIcon('search_files')}</div>);
    const icon = container.querySelector('[data-testid="search"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for web_search (Globe)', () => {
    const { container } = render(<div>{getToolIcon('web_search')}</div>);
    const icon = container.querySelector('[data-testid="globe"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for run_subagent (Bot)', () => {
    const { container } = render(<div>{getToolIcon('run_subagent')}</div>);
    const icon = container.querySelector('[data-testid="bot"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for run_parallel_subagents (Bot)', () => {
    const { container } = render(<div>{getToolIcon('run_parallel_subagents')}</div>);
    const icon = container.querySelector('[data-testid="bot"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for TodoWrite (ClipboardList)', () => {
    const { container } = render(<div>{getToolIcon('TodoWrite')}</div>);
    const icon = container.querySelector('[data-testid="clipboardlist"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for TodoRead (ClipboardList)', () => {
    const { container } = render(<div>{getToolIcon('TodoRead')}</div>);
    const icon = container.querySelector('[data-testid="clipboardlist"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for mcp_tools (Wrench)', () => {
    const { container } = render(<div>{getToolIcon('mcp_tools')}</div>);
    const icon = container.querySelector('[data-testid="wrench"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for view_history (ScrollText)', () => {
    const { container } = render(<div>{getToolIcon('view_history')}</div>);
    const icon = container.querySelector('[data-testid="scrolltext"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for rollback_changes (RotateCcw)', () => {
    const { container } = render(<div>{getToolIcon('rollback_changes')}</div>);
    const icon = container.querySelector('[data-testid="rotateccw"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for analyze_ui_screenshot (Eye)', () => {
    const { container } = render(<div>{getToolIcon('analyze_ui_screenshot')}</div>);
    const icon = container.querySelector('[data-testid="eye"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for analyze_image_content (FlaskConical)', () => {
    const { container } = render(<div>{getToolIcon('analyze_image_content')}</div>);
    const icon = container.querySelector('[data-testid="flaskconical"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for fetch_url (ArrowDown)', () => {
    const { container } = render(<div>{getToolIcon('fetch_url')}</div>);
    const icon = container.querySelector('[data-testid="arrowdown"]');
    expect(icon).toBeTruthy();
  });

  it('returns default Wrench for unknown tool name', () => {
    const { container } = render(<div>{getToolIcon('nonexistent_tool')}</div>);
    const icon = container.querySelector('[data-testid="wrench"]');
    expect(icon).toBeTruthy();
  });
});

// ── getPersonaColor ──────────────────────────────────────────────────

describe('getPersonaColor', () => {
  it('returns #58a6ff for coder', () => {
    expect(getPersonaColor('coder')).toBe('#58a6ff');
  });

  it('returns #d2a8ff for reviewer', () => {
    expect(getPersonaColor('reviewer')).toBe('#d2a8ff');
  });

  it('returns #d2a8ff for code_reviewer', () => {
    expect(getPersonaColor('code_reviewer')).toBe('#d2a8ff');
  });

  it('returns #7ee787 for tester', () => {
    expect(getPersonaColor('tester')).toBe('#7ee787');
  });

  it('returns #f0883e for debugger', () => {
    expect(getPersonaColor('debugger')).toBe('#f0883e');
  });

  it('returns #79c0ff for refactor', () => {
    expect(getPersonaColor('refactor')).toBe('#79c0ff');
  });

  it('returns #ff7b72 for researcher', () => {
    expect(getPersonaColor('researcher')).toBe('#ff7b72');
  });

  it('returns #6e7681 for general', () => {
    expect(getPersonaColor('general')).toBe('#6e7681');
  });

  it('returns #6e7681 for undefined persona', () => {
    expect(getPersonaColor(undefined)).toBe('#6e7681');
  });

  it('returns #6e7681 for unknown persona', () => {
    expect(getPersonaColor('unknown_persona')).toBe('#6e7681');
  });

  it('returns #6e7681 for empty string persona', () => {
    expect(getPersonaColor('')).toBe('#6e7681');
  });
});

// ── getStatusIcon ────────────────────────────────────────────────────

describe('getStatusIcon', () => {
  it('returns non-null JSX for "started" (Rocket)', () => {
    const { container } = render(<div>{getStatusIcon('started')}</div>);
    const icon = container.querySelector('[data-testid="rocket"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for "running" (Zap)', () => {
    const { container } = render(<div>{getStatusIcon('running')}</div>);
    const icon = container.querySelector('[data-testid="zap"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for "completed" (CheckCircle2)', () => {
    const { container } = render(<div>{getStatusIcon('completed')}</div>);
    const icon = container.querySelector('[data-testid="checkcircle2"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for "error" (XCircle)', () => {
    const { container } = render(<div>{getStatusIcon('error')}</div>);
    const icon = container.querySelector('[data-testid="xcircle"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for unknown status (Hourglass default)', () => {
    const { container } = render(<div>{getStatusIcon('unknown')}</div>);
    const icon = container.querySelector('[data-testid="hourglass"]');
    expect(icon).toBeTruthy();
  });

  it('returns non-null JSX for empty string status (Hourglass default)', () => {
    const { container } = render(<div>{getStatusIcon('')}</div>);
    const icon = container.querySelector('[data-testid="hourglass"]');
    expect(icon).toBeTruthy();
  });
});

// ── formatDuration ───────────────────────────────────────────────────

describe('formatDuration', () => {
  it('returns "0ms" for 0ms duration', () => {
    const now = new Date();
    expect(formatDuration(now, now)).toBe('0ms');
  });

  it('returns "500ms" for 500ms duration', () => {
    const end = new Date();
    const start = new Date(end.getTime() - 500);
    expect(formatDuration(start, end)).toBe('500ms');
  });

  it('returns "999ms" for 999ms duration', () => {
    const end = new Date();
    const start = new Date(end.getTime() - 999);
    expect(formatDuration(start, end)).toBe('999ms');
  });

  it('returns "1.0s" for 1000ms duration', () => {
    const end = new Date();
    const start = new Date(end.getTime() - 1000);
    expect(formatDuration(start, end)).toBe('1.0s');
  });

  it('returns "5.0s" for 5000ms duration', () => {
    const end = new Date();
    const start = new Date(end.getTime() - 5000);
    expect(formatDuration(start, end)).toBe('5.0s');
  });

  it('returns "5.5s" for 5500ms duration', () => {
    const end = new Date();
    const start = new Date(end.getTime() - 5500);
    expect(formatDuration(start, end)).toBe('5.5s');
  });

  it('returns "59.0s" for 59000ms duration', () => {
    const end = new Date();
    const start = new Date(end.getTime() - 59000);
    expect(formatDuration(start, end)).toBe('59.0s');
  });

  it('returns "1.0m" for 60000ms duration', () => {
    const end = new Date();
    const start = new Date(end.getTime() - 60000);
    expect(formatDuration(start, end)).toBe('1.0m');
  });

  it('returns "61.0m" for 3660000ms (61 min) duration', () => {
    const end = new Date();
    const start = new Date(end.getTime() - 3660000);
    expect(formatDuration(start, end)).toBe('61.0m');
  });

  it('uses current time when endTime is omitted', () => {
    const start = new Date(Date.now() - 500);
    const result = formatDuration(start);
    // Should be in ms range since only 500ms ago
    expect(result).toMatch(/\d+ms/);
  });
});

// ── formatRelativeTime ───────────────────────────────────────────────

describe('formatRelativeTime', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('returns "10s ago" for 10 seconds ago', () => {
    const now = Date.now();
    vi.setSystemTime(now);
    const date = new Date(now - 10000).toISOString();
    expect(formatRelativeTime(date)).toBe('10s ago');
  });

  it('returns "0s ago" for current time', () => {
    const now = Date.now();
    vi.setSystemTime(now);
    const date = new Date(now).toISOString();
    expect(formatRelativeTime(date)).toBe('0s ago');
  });

  it('returns "1m ago" for 1 minute ago', () => {
    const now = Date.now();
    vi.setSystemTime(now);
    const date = new Date(now - 60000).toISOString();
    expect(formatRelativeTime(date)).toBe('1m ago');
  });

  it('returns "59m ago" for 59 minutes ago', () => {
    const now = Date.now();
    vi.setSystemTime(now);
    const date = new Date(now - 59 * 60 * 1000).toISOString();
    expect(formatRelativeTime(date)).toBe('59m ago');
  });

  it('returns "2h ago" for 2 hours ago', () => {
    const now = Date.now();
    vi.setSystemTime(now);
    const date = new Date(now - 2 * 60 * 60 * 1000).toISOString();
    expect(formatRelativeTime(date)).toBe('2h ago');
  });

  it('returns "23h ago" for 23 hours ago', () => {
    const now = Date.now();
    vi.setSystemTime(now);
    const date = new Date(now - 23 * 60 * 60 * 1000).toISOString();
    expect(formatRelativeTime(date)).toBe('23h ago');
  });

  it('returns date string for 1 day ago', () => {
    const now = Date.now();
    vi.setSystemTime(now);
    const date = new Date(now - 24 * 60 * 60 * 1000).toISOString();
    const result = formatRelativeTime(date);
    // Should be a locale-formatted date string (not Xs/Xm/Xh ago)
    expect(result).not.toMatch(/\d+s ago/);
    expect(result).not.toMatch(/\d+m ago/);
    expect(result).not.toMatch(/\d+h ago/);
    expect(result).toBeTruthy();
  });

  it('returns "0s ago" for future date (negative diff clamped)', () => {
    const now = Date.now();
    vi.setSystemTime(now);
    const future = new Date(now + 5000).toISOString();
    expect(formatRelativeTime(future)).toBe('0s ago');
  });

  it('handles ISO date strings', () => {
    const now = Date.now();
    vi.setSystemTime(now);
    const date = new Date(now - 30000).toISOString();
    expect(formatRelativeTime(date)).toBe('30s ago');
  });
});

// ── formatTime ───────────────────────────────────────────────────────

describe('formatTime', () => {
  it('returns a non-empty formatted time string', () => {
    const result = formatTime(new Date());
    expect(typeof result).toBe('string');
    expect(result.length).toBeGreaterThan(0);
  });

  it('formats time with 2-digit hour and minute', () => {
    const result = formatTime(new Date('2024-01-01T14:30:00Z'));
    expect(result).toMatch(/\d{2}:\d{2}/);
  });

  it('handles invalid Date gracefully', () => {
    const result = formatTime(new Date('invalid'));
    // Should return "Invalid Date" or similar (locale dependent)
    expect(typeof result).toBe('string');
  });
});

// ── formatDurationMs ─────────────────────────────────────────────────

describe('formatDurationMs', () => {
  it('returns "0ms" for 0', () => {
    expect(formatDurationMs(0)).toBe('0ms');
  });

  it('returns "500ms" for 500', () => {
    expect(formatDurationMs(500)).toBe('500ms');
  });

  it('returns "999ms" for 999', () => {
    expect(formatDurationMs(999)).toBe('999ms');
  });

  it('returns "1s" for 1000', () => {
    expect(formatDurationMs(1000)).toBe('1s');
  });

  it('returns "5s" for 5000', () => {
    expect(formatDurationMs(5000)).toBe('5s');
  });

  it('returns "59s" for 59000', () => {
    expect(formatDurationMs(59000)).toBe('59s');
  });

  it('returns "1m 0s" for 60000', () => {
    expect(formatDurationMs(60000)).toBe('1m 0s');
  });

  it('returns "1m 5s" for 65000', () => {
    expect(formatDurationMs(65000)).toBe('1m 5s');
  });

  it('returns "61m 1s" for 3661000 (61m 1s)', () => {
    expect(formatDurationMs(3661000)).toBe('61m 1s');
  });

  it('returns ms format for negative values (less than 1000)', () => {
    expect(formatDurationMs(-100)).toBe('-100ms');
  });

  it('returns ms format for negative values (-1000 < 1000)', () => {
    expect(formatDurationMs(-1000)).toBe('-1000ms');
  });

  it('returns "2m 30s" for 150000', () => {
    expect(formatDurationMs(150000)).toBe('2m 30s');
  });
});

// ── formatTokens ─────────────────────────────────────────────────────

describe('formatTokens', () => {
  it('returns "0" for 0', () => {
    expect(formatTokens(0)).toBe('0');
  });

  it('returns "42" for 42', () => {
    expect(formatTokens(42)).toBe('42');
  });

  it('returns "999" for 999', () => {
    expect(formatTokens(999)).toBe('999');
  });

  it('returns "1.0K" for 1000', () => {
    expect(formatTokens(1000)).toBe('1.0K');
  });

  it('returns "1.5K" for 1500', () => {
    expect(formatTokens(1500)).toBe('1.5K');
  });

  it('returns "10.0K" for 10000', () => {
    expect(formatTokens(10000)).toBe('10.0K');
  });

  it('returns "1.0M" for 1000000', () => {
    expect(formatTokens(1000000)).toBe('1.0M');
  });

  it('returns "1.5M" for 1500000', () => {
    expect(formatTokens(1500000)).toBe('1.5M');
  });

  it('returns "—" for Infinity', () => {
    expect(formatTokens(Infinity)).toBe('—');
  });

  it('returns "—" for NaN', () => {
    expect(formatTokens(NaN)).toBe('—');
  });

  it('returns "—" for negative number', () => {
    expect(formatTokens(-100)).toBe('—');
  });
});

// ── formatCost ───────────────────────────────────────────────────────

describe('formatCost', () => {
  it('returns "$0.0000" for 0', () => {
    expect(formatCost(0)).toBe('$0.0000');
  });

  it('returns "$0.1235" for 0.12345 (rounds to 4 decimals)', () => {
    expect(formatCost(0.12345)).toBe('$0.1235');
  });

  it('returns "$1.0000" for 1', () => {
    expect(formatCost(1)).toBe('$1.0000');
  });

  it('returns "$0.0001" for 0.00005 (rounds up)', () => {
    expect(formatCost(0.00005)).toBe('$0.0001');
  });

  it('returns "—" for Infinity', () => {
    expect(formatCost(Infinity)).toBe('—');
  });

  it('returns "—" for NaN', () => {
    expect(formatCost(NaN)).toBe('—');
  });

  it('formats negative costs', () => {
    expect(formatCost(-0.1234)).toBe('$-0.1234');
  });

  it('returns "$10.5000" for 10.5', () => {
    expect(formatCost(10.5)).toBe('$10.5000');
  });
});
