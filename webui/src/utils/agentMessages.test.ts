import { describe, it, expect } from 'vitest';
import {
  AGENT_CHAT_LEAK_PATTERNS,
  TODO_STATUSES,
  shouldSuppressAgentMessageInChat,
  extractToolNameFromToolLogTarget,
  normalizeTodoList,
} from './agentMessages';

// ── AGENT_CHAT_LEAK_PATTERNS ─────────────────────────────────────────────

describe('AGENT_CHAT_LEAK_PATTERNS', () => {
  it('is a non-empty array of RegExp', () => {
    expect(Array.isArray(AGENT_CHAT_LEAK_PATTERNS)).toBe(true);
    expect(AGENT_CHAT_LEAK_PATTERNS.length).toBeGreaterThan(0);
    expect(AGENT_CHAT_LEAK_PATTERNS[0]).toBeInstanceOf(RegExp);
  });
});

// ── TODO_STATUSES ────────────────────────────────────────────────────────

describe('TODO_STATUSES', () => {
  it('is a Set containing the four valid statuses', () => {
    expect(TODO_STATUSES.has('pending')).toBe(true);
    expect(TODO_STATUSES.has('in_progress')).toBe(true);
    expect(TODO_STATUSES.has('completed')).toBe(true);
    expect(TODO_STATUSES.has('cancelled')).toBe(true);
  });

  it('does not contain other strings', () => {
    expect(TODO_STATUSES.has('unknown')).toBe(false);
    expect(TODO_STATUSES.has('')).toBe(false);
    expect(TODO_STATUSES.has('done')).toBe(false);
  });
});

// ── shouldSuppressAgentMessageInChat ──────────────────────────────────────

describe('shouldSuppressAgentMessageInChat', () => {
  // ── Suppress empty ─────────────────────────────────────────────────────

  it('suppresses empty string', () => {
    expect(shouldSuppressAgentMessageInChat('')).toBe(true);
  });

  it('suppresses whitespace-only string', () => {
    expect(shouldSuppressAgentMessageInChat('   ')).toBe(true);
  });

  it('suppresses newline-only string', () => {
    expect(shouldSuppressAgentMessageInChat('\n')).toBe(true);
  });

  it('suppresses tab-only string', () => {
    expect(shouldSuppressAgentMessageInChat('\t')).toBe(true);
  });

  // ── Suppress known leak patterns ───────────────────────────────────────

  it('suppresses "[N - N%] executing tool" pattern', () => {
    expect(shouldSuppressAgentMessageInChat('[1 - 100%] executing tool')).toBe(true);
    expect(shouldSuppressAgentMessageInChat('[50 - 100%] executing tool')).toBe(true);
  });

  it('suppresses "[N - N%] executing tool" with spaces in percentage', () => {
    expect(shouldSuppressAgentMessageInChat('[1   -   100%] executing tool')).toBe(true);
  });

  it('suppresses "executing tool [target]" pattern', () => {
    expect(shouldSuppressAgentMessageInChat('executing tool [run_command ls]')).toBe(true);
    expect(shouldSuppressAgentMessageInChat('executing tool [read_file]')).toBe(true);
  });

  it('suppresses TodoWrite pattern', () => {
    // Source pattern is `\bTodoWrite\(/i` — the parenthesis is required
    // so we only suppress actual tool-call leak text, not stray mentions
    // of the word "TodoWrite" inside normal agent prose. Source comment
    // is explicit: "via TodoWrite( call patterns only".
    const msg1 = 'TodoWrite(' + '[{content: "task"}]' + ')';
    const msg2 = '  TodoWrite(' + '[{id: "1"}]' + ')';
    expect(shouldSuppressAgentMessageInChat(msg1)).toBe(true);
    expect(shouldSuppressAgentMessageInChat(msg2)).toBe(true);
    // Case insensitivity still applies, but the open-paren is required.
    expect(shouldSuppressAgentMessageInChat('todowrite(args)')).toBe(true);
    // Bare word with no call shape is NOT suppressed — left to flow as
    // normal text so we don't eat references like "the TodoWrite tool".
    expect(shouldSuppressAgentMessageInChat('todoWrite')).toBe(false);
  });

  it('suppresses todos=N pattern', () => {
    expect(shouldSuppressAgentMessageInChat('todos=5')).toBe(true);
    expect(shouldSuppressAgentMessageInChat('todos=0')).toBe(true);
  });

  it('suppresses status bar pattern [ ]=N [~]=N [x]=N [-]=N', () => {
    expect(shouldSuppressAgentMessageInChat('[ ]=1 [~]=2 [x]=3 [-]=4')).toBe(true);
    expect(shouldSuppressAgentMessageInChat('[ ]=0 [~]=0 [x]=0 [-]=0')).toBe(true);
  });

  it('suppresses subagent progress pattern', () => {
    expect(shouldSuppressAgentMessageInChat('Subagent: [1 - 100%]')).toBe(true);
    expect(shouldSuppressAgentMessageInChat('Subagent: [50 - 100%]')).toBe(true);
  });

  it('suppresses leak patterns with leading whitespace', () => {
    expect(shouldSuppressAgentMessageInChat('  [1 - 100%] executing tool')).toBe(true);
    expect(shouldSuppressAgentMessageInChat('  Subagent: [1 - 100%]')).toBe(true);
  });

  // ── Do NOT suppress normal messages ────────────────────────────────────

  it('does not suppress normal chat messages', () => {
    expect(shouldSuppressAgentMessageInChat('Hello, world!')).toBe(false);
    expect(shouldSuppressAgentMessageInChat('I have completed the task.')).toBe(false);
    expect(shouldSuppressAgentMessageInChat('Running tests...')).toBe(false);
  });

  it('does not suppress messages that contain substrings but dont match patterns', () => {
    expect(shouldSuppressAgentMessageInChat('I will write todos for this')).toBe(false);
    expect(shouldSuppressAgentMessageInChat('The todo list is ready')).toBe(false);
    expect(shouldSuppressAgentMessageInChat('This is a tool [but not a leak]')).toBe(false);
  });

  it('does not suppress empty-looking but not-empty messages', () => {
    expect(shouldSuppressAgentMessageInChat('x')).toBe(false);
    expect(shouldSuppressAgentMessageInChat('0')).toBe(false);
  });

  it('does not suppress "executing tool" without bracket target or percentage', () => {
    // "executing tool" by itself doesn't match the patterns
    expect(shouldSuppressAgentMessageInChat('executing tool')).toBe(false);
  });

  it('does not suppress random numbers', () => {
    expect(shouldSuppressAgentMessageInChat('42')).toBe(false);
  });

  it('does not suppress tool names in brackets without prefix', () => {
    expect(shouldSuppressAgentMessageInChat('[run_command] ls')).toBe(false);
  });
});

// ── extractToolNameFromToolLogTarget ─────────────────────────────────────

describe('extractToolNameFromToolLogTarget', () => {
  it('returns null for undefined', () => {
    expect(extractToolNameFromToolLogTarget(undefined as any)).toBeNull();
  });

  it('returns null for null', () => {
    expect(extractToolNameFromToolLogTarget(null as any)).toBeNull();
  });

  it('returns null for empty string', () => {
    expect(extractToolNameFromToolLogTarget('')).toBeNull();
  });

  it('returns null for whitespace-only string', () => {
    expect(extractToolNameFromToolLogTarget('   ')).toBeNull();
  });

  it('returns null for string without brackets', () => {
    expect(extractToolNameFromToolLogTarget('run_command ls')).toBeNull();
  });

  it('returns null for string with only opening bracket', () => {
    expect(extractToolNameFromToolLogTarget('[run_command')).toBeNull();
  });

  it('returns null for string with only closing bracket', () => {
    expect(extractToolNameFromToolLogTarget('run_command]')).toBeNull();
  });

  it('returns null for empty brackets', () => {
    expect(extractToolNameFromToolLogTarget('[]')).toBeNull();
  });

  it('returns null for whitespace inside brackets', () => {
    expect(extractToolNameFromToolLogTarget('[  ]')).toBeNull();
  });

  it('extracts tool name from [tool_name]', () => {
    expect(extractToolNameFromToolLogTarget('[run_command]')).toBe('run_command');
  });

  it('extracts tool name from [tool_name args]', () => {
    expect(extractToolNameFromToolLogTarget('[run_command ls -la]')).toBe('run_command');
  });

  it('extracts tool name with leading/trailing whitespace', () => {
    expect(extractToolNameFromToolLogTarget('  [read_file /path/to/file]  ')).toBe('read_file');
  });

  it('extracts first token from multi-word content', () => {
    expect(extractToolNameFromToolLogTarget('[write_file /path file]')).toBe('write_file');
  });

  it('extracts tool name from single word', () => {
    expect(extractToolNameFromToolLogTarget('[TodoWrite]')).toBe('TodoWrite');
  });

  it('handles tool names with dashes', () => {
    expect(extractToolNameFromToolLogTarget('[run-command args]')).toBe('run-command');
  });

  it('handles tool names with underscores and camelCase', () => {
    expect(extractToolNameFromToolLogTarget('[shell_command echo hello]')).toBe('shell_command');
  });
});

// ── normalizeTodoList ────────────────────────────────────────────────────

describe('normalizeTodoList', () => {
  // ── Non-array inputs ───────────────────────────────────────────────────

  it('returns empty array for non-array input', () => {
    expect(normalizeTodoList(null)).toEqual([]);
    expect(normalizeTodoList(undefined)).toEqual([]);
    expect(normalizeTodoList('string')).toEqual([]);
    expect(normalizeTodoList(42)).toEqual([]);
    expect(normalizeTodoList({})).toEqual([]);
  });

  it('returns empty array for empty array', () => {
    expect(normalizeTodoList([])).toEqual([]);
  });

  // ── Valid entries ──────────────────────────────────────────────────────

  it('normalizes valid todo entry', () => {
    const result = normalizeTodoList([
      {
        id: 'task-1',
        content: 'Implement feature',
        status: 'pending',
      },
    ]);
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({
      id: 'task-1',
      content: 'Implement feature',
      status: 'pending',
    });
  });

  it('generates ID when missing', () => {
    const result = normalizeTodoList([
      {
        content: 'Do something',
        status: 'in_progress',
      },
    ]);
    expect(result).toHaveLength(1);
    expect(result[0].id).toBeTruthy();
    expect(typeof result[0].id).toBe('string');
  });

  it('trims content and status', () => {
    const result = normalizeTodoList([
      {
        id: '1',
        content: '  Trim me  ',
        status: '  pending  ',
      },
    ]);
    expect(result[0].content).toBe('Trim me');
    expect(result[0].status).toBe('pending');
  });

  it('handles all valid statuses', () => {
    const result = normalizeTodoList([
      { id: '1', content: 'Task 1', status: 'pending' },
      { id: '2', content: 'Task 2', status: 'in_progress' },
      { id: '3', content: 'Task 3', status: 'completed' },
      { id: '4', content: 'Task 4', status: 'cancelled' },
    ]);
    expect(result).toHaveLength(4);
    expect(result[0].status).toBe('pending');
    expect(result[1].status).toBe('in_progress');
    expect(result[2].status).toBe('completed');
    expect(result[3].status).toBe('cancelled');
  });

  // ── Invalid entries ────────────────────────────────────────────────────

  it('rejects null entries', () => {
    const result = normalizeTodoList([null, { id: '1', content: 'OK', status: 'pending' }]);
    expect(result).toHaveLength(1);
    expect(result[0].content).toBe('OK');
  });

  it('rejects non-object entries', () => {
    const result = normalizeTodoList(['string', 42, true, { id: '1', content: 'OK', status: 'pending' }]);
    expect(result).toHaveLength(1);
  });

  it('rejects entries with empty content', () => {
    const result = normalizeTodoList([{ id: '1', content: '', status: 'pending' }]);
    expect(result).toEqual([]);
  });

  it('rejects entries with whitespace-only content', () => {
    const result = normalizeTodoList([{ id: '1', content: '   ', status: 'pending' }]);
    expect(result).toEqual([]);
  });

  it('rejects entries with missing content', () => {
    const result = normalizeTodoList([{ id: '1', status: 'pending' }]);
    expect(result).toEqual([]);
  });

  it('rejects entries with invalid status', () => {
    const result = normalizeTodoList([{ id: '1', content: 'Task', status: 'unknown' }]);
    expect(result).toEqual([]);
  });

  it('rejects entries with missing status', () => {
    const result = normalizeTodoList([{ id: '1', content: 'Task' }]);
    expect(result).toEqual([]);
  });

  it('rejects entries with non-string content', () => {
    const result = normalizeTodoList([{ id: '1', content: 123, status: 'pending' }]);
    expect(result).toEqual([]);
  });

  it('rejects entries with non-string status', () => {
    const result = normalizeTodoList([{ id: '1', content: 'Task', status: 123 }]);
    expect(result).toEqual([]);
  });

  // ── Deduplication ──────────────────────────────────────────────────────

  it('deduplicates entries with same id, status, and content', () => {
    const entry = { id: '1', content: 'Task', status: 'pending' };
    const result = normalizeTodoList([entry, entry]);
    expect(result).toHaveLength(1);
  });

  it('allows same content with different ids', () => {
    const result = normalizeTodoList([
      { id: '1', content: 'Task', status: 'pending' },
      { id: '2', content: 'Task', status: 'pending' },
    ]);
    expect(result).toHaveLength(2);
  });

  it('allows same id with different content', () => {
    const result = normalizeTodoList([
      { id: '1', content: 'Task A', status: 'pending' },
      { id: '1', content: 'Task B', status: 'pending' },
    ]);
    expect(result).toHaveLength(2);
  });

  it('allows same id with different status', () => {
    const result = normalizeTodoList([
      { id: '1', content: 'Task', status: 'pending' },
      { id: '1', content: 'Task', status: 'completed' },
    ]);
    expect(result).toHaveLength(2);
  });

  // ── ID generation ──────────────────────────────────────────────────────

  it('generates predictable ID from index and content', () => {
    const result = normalizeTodoList([
      {
        content: 'My Task',
        status: 'pending',
      },
    ]);
    expect(result[0].id).toBe('todo-0-pending-My Task');
  });

  it('generates ID with content truncated to 48 chars', () => {
    const longContent = 'a'.repeat(100);
    const result = normalizeTodoList([
      {
        content: longContent,
        status: 'pending',
      },
    ]);
    expect(result[0].id).toBe(`todo-0-pending-${'a'.repeat(48)}`);
  });

  it('uses provided ID when available', () => {
    const result = normalizeTodoList([
      {
        id: 'my-custom-id',
        content: 'Task',
        status: 'pending',
      },
    ]);
    expect(result[0].id).toBe('my-custom-id');
  });

  // ── Mixed valid and invalid ────────────────────────────────────────────

  it('handles mixed valid and invalid entries', () => {
    const result = normalizeTodoList([
      null,
      'not an object',
      { id: '1', content: 'Valid', status: 'pending' },
      { id: '2', content: '', status: 'pending' },
      { id: '3', content: 'Also valid', status: 'completed' },
      { content: 'No status' },
      { id: '4', content: 'Invalid status', status: 'unknown' },
      { id: '5', content: 'Third valid', status: 'in_progress' },
    ]);
    expect(result).toHaveLength(3);
    expect(result[0].content).toBe('Valid');
    expect(result[1].content).toBe('Also valid');
    expect(result[2].content).toBe('Third valid');
  });

  // ── Trimmed ID generation ──────────────────────────────────────────────

  it('trims generated ID if id is provided but whitespace-only', () => {
    const result = normalizeTodoList([
      {
        id: '   ',
        content: 'Task',
        status: 'pending',
      },
    ]);
    // Whitespace-only ID should fall back to generated ID
    expect(result[0].id).toBeTruthy();
    expect(result[0].id.startsWith('todo-')).toBe(true);
  });
});
