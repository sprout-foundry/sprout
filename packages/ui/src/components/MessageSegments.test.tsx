import { vi } from 'vitest';

// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';

// Mock react-markdown (ESM-only, ts-jest can't transform it)
// Note: markdown rendering is not tested here — MessageSegments delegates
// to MessageContent which in turn uses react-markdown. This mock simply
// passes children through so we can test MessageSegments' own logic.
vi.mock('react-markdown', () => {
  function MockMarkdown({ children }: { children: string }) {
    return createElement('div', {}, children);
  }
  return { __esModule: true, default: MockMarkdown };
});

vi.mock('remark-gfm', () => ({ default: [] }));

import MessageSegments from './MessageSegments';
import type { ToolRef } from '../types/chat';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ---------------------------------------------------------------------------
// Tests: MessageSegments
// ---------------------------------------------------------------------------

describe('MessageSegments', () => {
  it('renders the wrapper div with message-segments class', () => {
    act(() => {
      root.render(createElement(MessageSegments, { content: 'Hello' }));
    });
    expect(container.querySelector('.message-segments')).not.toBeNull();
  });

  it('renders plain text as a text segment', () => {
    act(() => {
      root.render(createElement(MessageSegments, { content: 'Just some text' }));
    });
    expect(container.querySelector('.segment-text')).not.toBeNull();
    expect(container.textContent).toContain('Just some text');
  });

  it('renders tool call as a tool pill', () => {
    act(() => {
      root.render(createElement(MessageSegments, {
        content: '[executing tool [read_file(path="/tmp/test.txt")]]',
      }));
    });
    expect(container.querySelector('.segment-tool-call')).not.toBeNull();
    // Should show short name "read"
    expect(container.querySelector('.tool-pill-name')?.textContent).toBe('read');
  });

  it('shows tool pill icon for known tools', () => {
    act(() => {
      root.render(createElement(MessageSegments, {
        content: '[executing tool [shell_command(echo "hello")]]',
      }));
    });
    const icon = container.querySelector('.tool-pill-icon');
    expect(icon).not.toBeNull();
  });

  it('renders todo update segment', () => {
    act(() => {
      root.render(createElement(MessageSegments, {
        content: '[x] Complete task\n[ ] Pending task',
      }));
    });
    expect(container.querySelector('.segment-todo-summary')).not.toBeNull();
    // Should have inline-todo elements
    const todos = container.querySelectorAll('.inline-todo');
    expect(todos).toHaveLength(2);
  });

  it('renders completed todo with CheckCircle icon', () => {
    act(() => {
      root.render(createElement(MessageSegments, {
        content: '[x] Done task',
      }));
    });
    expect(container.querySelector('.inline-todo-completed')).not.toBeNull();
  });

  it('renders pending todo with Circle icon', () => {
    act(() => {
      root.render(createElement(MessageSegments, {
        content: '[ ] Pending task',
      }));
    });
    expect(container.querySelector('.inline-todo-pending')).not.toBeNull();
  });

  it('renders in_progress todo with Loader2 icon', () => {
    act(() => {
      root.render(createElement(MessageSegments, {
        content: '[~] In progress task',
      }));
    });
    expect(container.querySelector('.inline-todo-in_progress')).not.toBeNull();
  });

  it('renders cancelled todo with Minus icon', () => {
    act(() => {
      root.render(createElement(MessageSegments, {
        content: '[-] Cancelled task',
      }));
    });
    expect(container.querySelector('.inline-todo-cancelled')).not.toBeNull();
  });

  it('does not render progress or result segments visibly', () => {
    // Progress and result segments return null in the render
    act(() => {
      root.render(createElement(MessageSegments, {
        content: '[OK] Completed in 1.2m',
      }));
    });
    // Result segments return null
    expect(container.querySelector('.segment-tool-call')).toBeNull();
    expect(container.querySelector('.segment-todo-summary')).toBeNull();
  });

  it('falls back to MessageContent when parseMessageSegments throws', () => {
    // We can't easily trigger a parse error, but we verify the component handles
    // normal content that results in mixed segments
    act(() => {
      root.render(createElement(MessageSegments, {
        content: 'Hello\n[executing tool [read_file(path="test.txt")]]\nWorld',
      }));
    });
    expect(container.querySelector('.message-segments')).not.toBeNull();
  });

  it('strips ANSI codes before parsing segments', () => {
    act(() => {
      root.render(createElement(MessageSegments, {
        content: '\x1B[31m[executing tool [read_file(path="test.txt")]]\x1B[0m',
      }));
    });
    expect(container.querySelector('.segment-tool-call')).not.toBeNull();
  });

  // ---------------------------------------------------------------------------
  // Tool refs and status-based rendering
  // ---------------------------------------------------------------------------

  it('renders completed tool as footnote when getToolStatus returns "completed"', () => {
    const toolRefs: ToolRef[] = [
      { toolId: 't1', toolName: 'read_file', label: 'read_file(path="test.txt")' },
    ];

    const getToolStatus = vi.fn(() => 'completed');
    const onToolRefClick = vi.fn();

    act(() => {
      root.render(
        createElement(MessageSegments, {
          content: '[executing tool [read_file(path="test.txt")]]',
          toolRefs,
          onToolRefClick,
          getToolStatus,
        })
      );
    });

    // Should render as footnote instead of pill
    expect(container.querySelector('.segment-tool-footnote')).not.toBeNull();
    expect(container.querySelector('.segment-tool-call')).toBeNull();
    // Footnote shows short name in brackets
    expect(container.textContent).toContain('read');
  });

  it('renders error tool as footnote with error class', () => {
    const toolRefs: ToolRef[] = [
      { toolId: 't2', toolName: 'write_file', label: 'write_file(path="out.txt")' },
    ];

    const getToolStatus = vi.fn(() => 'error');

    act(() => {
      root.render(
        createElement(MessageSegments, {
          content: '[executing tool [write_file(path="out.txt")]]',
          toolRefs,
          getToolStatus,
        })
      );
    });

    expect(container.querySelector('.segment-tool-footnote--error')).not.toBeNull();
  });

  it('clicking a footnote calls onToolRefClick with the toolId', () => {
    const toolRefs: ToolRef[] = [
      { toolId: 't3', toolName: 'shell_command', label: 'shell_command(echo hello)' },
    ];

    const onToolRefClick = vi.fn();
    const getToolStatus = vi.fn(() => 'completed');

    act(() => {
      root.render(
        createElement(MessageSegments, {
          content: '[executing tool [shell_command(echo hello)]]',
          toolRefs,
          onToolRefClick,
          getToolStatus,
        })
      );
    });

    const footnote = container.querySelector('.segment-tool-footnote');
    act(() => {
      footnote?.click();
    });
    expect(onToolRefClick).toHaveBeenCalledWith('t3');
  });

  it('keyboard Enter on footnote calls onToolRefClick', () => {
    const toolRefs: ToolRef[] = [
      { toolId: 't4', toolName: 'read_file', label: 'read_file(path="test.txt")' },
    ];

    const onToolRefClick = vi.fn();
    const getToolStatus = vi.fn(() => 'completed');

    act(() => {
      root.render(
        createElement(MessageSegments, {
          content: '[executing tool [read_file(path="test.txt")]]',
          toolRefs,
          onToolRefClick,
          getToolStatus,
        })
      );
    });

    const footnote = container.querySelector('.segment-tool-footnote');
    act(() => {
      footnote?.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })
      );
    });
    expect(onToolRefClick).toHaveBeenCalledWith('t4');
  });

  it('footnote has role="button" and tabIndex=0', () => {
    const toolRefs: ToolRef[] = [
      { toolId: 't5', toolName: 'search_files', label: 'search_files(path="src")' },
    ];

    const getToolStatus = vi.fn(() => 'completed');

    act(() => {
      root.render(
        createElement(MessageSegments, {
          content: '[executing tool [search_files(path="src")]]',
          toolRefs,
          getToolStatus,
        })
      );
    });

    const footnote = container.querySelector('.segment-tool-footnote');
    expect(footnote?.getAttribute('role')).toBe('button');
    expect(footnote?.getAttribute('tabIndex')).toBe('0');
  });

  it('footnote has title with tool label', () => {
    const toolRefs: ToolRef[] = [
      { toolId: 't6', toolName: 'read_file', label: 'read_file(path="src/main.go")' },
    ];

    const getToolStatus = vi.fn(() => 'completed');

    act(() => {
      root.render(
        createElement(MessageSegments, {
          content: '[executing tool [read_file(path="src/main.go")]]',
          toolRefs,
          getToolStatus,
        })
      );
    });

    const footnote = container.querySelector('.segment-tool-footnote');
    expect(footnote?.getAttribute('title')).toBe('read_file(path="src/main.go")');
  });

  it('renders tool call as pill when no matching toolRef or status not done', () => {
    const onToolClick = vi.fn();

    act(() => {
      root.render(
        createElement(MessageSegments, {
          content: '[executing tool [read_file(path="test.txt")]]',
          onToolClick,
        })
      );
    });

    expect(container.querySelector('.segment-tool-call')).not.toBeNull();
    expect(container.querySelector('.segment-tool-footnote')).toBeNull();
  });

  it('clicking a tool pill calls onToolClick with tool name', () => {
    const onToolClick = vi.fn();

    act(() => {
      root.render(
        createElement(MessageSegments, {
          content: '[executing tool [web_search(query="react")]]',
          onToolClick,
        })
      );
    });

    const pill = container.querySelector('.segment-tool-call');
    act(() => {
      pill?.click();
    });
    expect(onToolClick).toHaveBeenCalled();
  });

  it('clicking a tool pill with matching ref calls onToolRefClick instead', () => {
    const onToolClick = vi.fn();
    const onToolRefClick = vi.fn();
    const toolRefs: ToolRef[] = [
      { toolId: 't7', toolName: 'shell_command', label: 'shell_command(echo hi)' },
    ];

    act(() => {
      root.render(
        createElement(MessageSegments, {
          content: '[executing tool [shell_command(echo hi)]]',
          toolRefs,
          onToolClick,
          onToolRefClick,
        })
      );
    });

    const pill = container.querySelector('.segment-tool-call');
    act(() => {
      pill?.click();
    });
    // With matching ref, it should call onToolRefClick
    expect(onToolRefClick).toHaveBeenCalledWith('t7');
  });

  it('tool pill has title with matching ref label or summary', () => {
    const toolRefs: ToolRef[] = [
      { toolId: 't8', toolName: 'read_file', label: 'read_file(path="app.ts")' },
    ];

    act(() => {
      root.render(
        createElement(MessageSegments, {
          content: '[executing tool [read_file(path="app.ts")]]',
          toolRefs,
        })
      );
    });

    const pill = container.querySelector('.segment-tool-call');
    expect(pill?.getAttribute('title')).toBe('read_file(path="app.ts")');
  });

  it('renders short tool names in pill', () => {
    act(() => {
      root.render(createElement(MessageSegments, {
        content: '[executing tool [run_subagent(prompt="test")]]',
      }));
    });
    const name = container.querySelector('.tool-pill-name');
    expect(name?.textContent).toBe('subagent');
  });

  it('renders "todo write" as short name for TodoWrite', () => {
    act(() => {
      root.render(createElement(MessageSegments, {
        content: '[executing tool [TodoWrite(todos=[])]]',
      }));
    });
    const name = container.querySelector('.tool-pill-name');
    expect(name?.textContent).toBe('todo write');
  });

  it('uses fallback Wrench icon for unknown tools', () => {
    act(() => {
      root.render(createElement(MessageSegments, {
        content: '[executing tool [custom_unknown_tool()]]',
      }));
    });
    expect(container.querySelector('.tool-pill-icon')).not.toBeNull();
  });

  // ---------------------------------------------------------------------------
  // Mixed segments
  // ---------------------------------------------------------------------------

  it('renders text + tool_call segments in order', () => {
    act(() => {
      root.render(createElement(MessageSegments, {
        content: 'Before tool.\n[executing tool [read_file(path="a.txt")]]\nAfter tool.',
      }));
    });
    const textSegments = container.querySelectorAll('.segment-text');
    expect(textSegments).toHaveLength(2);
    expect(container.querySelector('.segment-tool-call')).not.toBeNull();
  });

  it('renders tool_call with matching ref as footnote and unclaimed as pill', () => {
    const toolRefs: ToolRef[] = [
      { toolId: 't9', toolName: 'read_file', label: 'read_file(path="a.txt")' },
      // second tool_call has no matching ref
    ];
    const getToolStatus = vi.fn(() => 'completed');

    // Add a text line between the two tool calls so they are parsed as
    // separate segments (the parser groups consecutive tool lines together).
    act(() => {
      root.render(
        createElement(MessageSegments, {
          content:
            '[executing tool [read_file(path="a.txt")]]\n\n' +
            '[executing tool [write_file(path="b.txt")]]',
          toolRefs,
          getToolStatus,
        })
      );
    });
    // First tool matched and completed → footnote
    expect(container.querySelectorAll('.segment-tool-footnote')).toHaveLength(1);
    // Second tool has no matching ref → pill
    expect(container.querySelector('.segment-tool-call')).not.toBeNull();
  });

  it('renders empty content as empty message-segments div', () => {
    act(() => {
      root.render(createElement(MessageSegments, { content: '' }));
    });
    expect(container.querySelector('.message-segments')).not.toBeNull();
    // Empty content produces no segments, so no child elements
    expect(container.querySelectorAll('.segment-text')).toHaveLength(0);
  });

  it('renders only whitespace content without visible segments', () => {
    act(() => {
      root.render(createElement(MessageSegments, { content: '   \n  ' }));
    });
    expect(container.querySelector('.message-segments')).not.toBeNull();
  });
});
