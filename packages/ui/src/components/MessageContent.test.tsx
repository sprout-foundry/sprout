import { vi } from 'vitest';

// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { act, createElement, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { isMarkdownCodeBlock, isLocalFilePath } from '../utils/markdownCode';

// ---------------------------------------------------------------------------
// Mock react-markdown (ESM-only, ts-jest can't transform it)
// ---------------------------------------------------------------------------
//
// This mock is intentionally lightweight. It does NOT implement a full
// markdown parser — that is react-markdown's responsibility. Instead it
// handles just enough (code blocks, inline code, links, plain text, and
// a handful of block elements) so that MessageContent's own component
// overrides (code / a) can be exercised.

function parseMarkdownMinimal(
  content: string,
  components?: Record<string, (props: any) => ReactNode>
): ReactNode[] {
  const results: ReactNode[] = [];
  let remaining = content;

  while (remaining.length > 0) {
    // --- fenced code block: ```lang\n...\n``` ---
    const codeBlockMatch = remaining.match(/^```(\w*)\n([\s\S]*?)```/);
    // --- same-line fenced code: ```code``` (no newline → inline) ---
    const inlineFencedMatch = !codeBlockMatch ? remaining.match(/^```([^`\n]+)```/) : null;
    if (codeBlockMatch) {
      const lang = codeBlockMatch[1];
      const codeText = codeBlockMatch[2];
      const className = lang ? `language-${lang}` : '';
      const isBlock = isMarkdownCodeBlock(className, codeText);

      if (components && components.code) {
        results.push(
          createElement(components.code, {
            key: `codeblock-${results.length}`,
            className,
          }, codeText)
        );
      } else if (isBlock) {
        results.push(
          createElement(
            'pre',
            { key: `pre-${results.length}`, className: 'code-block' },
            createElement('span', { className: 'code-language', key: 'lang' }, lang || 'text'),
            createElement('code', { className, key: 'code' }, codeText)
          )
        );
      } else {
        results.push(
          createElement(
            'code',
            { key: `code-${results.length}`, className: 'inline-code' },
            codeText
          )
        );
      }
      remaining = remaining.slice(codeBlockMatch[0].length);
      continue;
    }

    // --- same-line fenced code: ```code``` (treated as inline code) ---
    if (inlineFencedMatch) {
      const codeText = inlineFencedMatch[1];
      if (components && components.code) {
        results.push(
          createElement(components.code, {
            key: `icode-inline-${results.length}`,
            className: '',
          }, codeText)
        );
      } else {
        results.push(
          createElement(
            'code',
            { key: `icode-inline-${results.length}`, className: 'inline-code' },
            codeText
          )
        );
      }
      remaining = remaining.slice(inlineFencedMatch[0].length);
      continue;
    }

    // --- inline code: `code` ---
    const inlineCodeMatch = remaining.match(/^`([^`]+)`/);
    if (inlineCodeMatch) {
      const codeText = inlineCodeMatch[1];
      if (components && components.code) {
        results.push(
          createElement(components.code, {
            key: `icode-${results.length}`,
            className: '',
          }, codeText)
        );
      } else {
        results.push(
          createElement(
            'code',
            { key: `icode-${results.length}`, className: 'inline-code' },
            codeText
          )
        );
      }
      remaining = remaining.slice(inlineCodeMatch[0].length);
      continue;
    }

    // --- links: [text](url) ---
    const linkMatch = remaining.match(/^\[([^\]]+)\]\(([^)]+)\)/);
    if (linkMatch) {
      const linkText = linkMatch[1];
      const href = linkMatch[2];
      if (components && components.a) {
        results.push(
          createElement(components.a, {
            key: `link-${results.length}`,
            href,
            children: linkText,
          })
        );
      } else {
        const local = isLocalFilePath(href);
        if (local) {
          results.push(
            createElement(
              'a',
              {
                key: `link-${results.length}`,
                href,
                onClick: (e: any) => {
                  e.preventDefault();
                  window.dispatchEvent(
                    new CustomEvent('sprout:open-in-editor', { detail: { path: href } })
                  );
                },
              },
              linkText
            )
          );
        } else {
          results.push(
            createElement(
              'a',
              {
                key: `link-${results.length}`,
                href,
                target: '_blank',
                rel: 'noreferrer',
              },
              linkText
            )
          );
        }
      }
      remaining = remaining.slice(linkMatch[0].length);
      continue;
    }

    // --- headings: #..# text ---
    const headingMatch = remaining.match(/^(#{1,6})\s+(.*?)(\n|$)/);
    if (headingMatch) {
      const level = headingMatch[1].length;
      results.push(
        createElement(`h${level}`, { key: `h-${results.length}` }, headingMatch[2])
      );
      remaining = remaining.slice(headingMatch[0].length);
      continue;
    }

    // --- blockquote: > text ---
    const bqMatch = remaining.match(/^> (.*?)(\n|$)/);
    if (bqMatch) {
      results.push(
        createElement('blockquote', { key: `bq-${results.length}` }, bqMatch[1])
      );
      remaining = remaining.slice(bqMatch[0].length);
      continue;
    }

    // --- horizontal rule: --- ---
    const hrMatch = remaining.match(/^(---|\*\*\*)(\n|$)/);
    if (hrMatch) {
      results.push(createElement('hr', { key: `hr-${results.length}` }));
      remaining = remaining.slice(hrMatch[0].length);
      continue;
    }

    // --- unordered list item: - text ---
    const ulMatch = remaining.match(/^-\s+(.*?)(\n|$)/);
    if (ulMatch) {
      results.push(
        createElement('ul', { key: `ul-${results.length}` },
          createElement('li', { key: `li-${results.length}` }, ulMatch[1])
        )
      );
      remaining = remaining.slice(ulMatch[0].length);
      continue;
    }

    // --- ordered list item: 1. text ---
    const olMatch = remaining.match(/^(\d+)\.\s+(.*?)(\n|$)/);
    if (olMatch) {
      results.push(
        createElement('ol', { key: `ol-${results.length}` },
          createElement('li', { key: `li-${results.length}` }, olMatch[2])
        )
      );
      remaining = remaining.slice(olMatch[0].length);
      continue;
    }

    // --- table rows: collect consecutive | ... | lines into a single <table> ---
    const tableRowRegex = /^\|.*\|(\n|$)/;
    if (tableRowRegex.test(remaining)) {
      // Collect all consecutive table rows
      const allRows: string[] = [];
      let tableRemaining = remaining;
      while (tableRowRegex.test(tableRemaining)) {
        const rowMatch = tableRemaining.match(/^(\|.*\|)(\n|$)/);
        if (rowMatch) {
          allRows.push(rowMatch[1]);
          tableRemaining = tableRemaining.slice(rowMatch[0].length);
        } else {
          break;
        }
      }
      // Build a single table element
      const rows = allRows
        .filter(r => !r.includes('---')) // skip separator rows
        .map(r => r.split('|').filter(c => c.trim() !== '').map(c => c.trim()));
      if (rows.length > 0) {
        results.push(
          createElement('table', { key: `table-${results.length}` },
            ...rows.map((cells, ri) =>
              createElement('tr', { key: `tr-${ri}` },
                ...cells.map((cell: string, ci: number) =>
                  createElement(ri === 0 ? 'th' : 'td', { key: `td-${ri}-${ci}` }, cell)
                )
              )
            )
          )
        );
      }
      remaining = tableRemaining;
      continue;
    }

    // --- bold: **text** ---
    const boldMatch = remaining.match(/^\*\*([^*]+)\*\*/);
    if (boldMatch) {
      results.push(
        createElement('strong', { key: `b-${results.length}` }, boldMatch[1])
      );
      remaining = remaining.slice(boldMatch[0].length);
      continue;
    }

    // --- italic: *text* ---
    const italicMatch = remaining.match(/^\*([^*\s][^*]*?)\*/);
    if (italicMatch) {
      results.push(
        createElement('em', { key: `i-${results.length}` }, italicMatch[1])
      );
      remaining = remaining.slice(italicMatch[0].length);
      continue;
    }

    // --- strikethrough: ~~text~~ ---
    const strikeMatch = remaining.match(/^~~([^~]+)~~/);
    if (strikeMatch) {
      results.push(
        createElement('del', { key: `s-${results.length}` }, strikeMatch[1])
      );
      remaining = remaining.slice(strikeMatch[0].length);
      continue;
    }

    // --- plain text (consume until newline or next special char) ---
    // Also consume a trailing newline to avoid infinite loops on bare \n.
    const plainMatch = remaining.match(/^([^\n]*)\n?/);
    if (plainMatch) {
      const text = plainMatch[1];
      if (text.trim() !== '') {
        // Split text on inline backtick code spans and interleave
        const parts = text.split(/(`[^`]+`)/g);
        const children: ReactNode[] = [];
        for (const part of parts) {
          if (part.startsWith('`') && part.endsWith('`') && part.length >= 2) {
            const codeText = part.slice(1, -1);
            if (components && components.code) {
              children.push(
                createElement(components.code, {
                  key: `icode-${results.length}-${children.length}`,
                  className: '',
                }, codeText)
              );
            } else {
              children.push(
                createElement('code', {
                  key: `icode-${results.length}-${children.length}`,
                  className: 'inline-code',
                }, codeText)
              );
            }
          } else if (part) {
            children.push(part);
          }
        }
        results.push(
          createElement('p', { key: `p-${results.length}` }, ...children)
        );
      }
      remaining = remaining.slice(plainMatch[0].length);
      continue;
    }

    // Fallback: consume one character to prevent infinite loop
    remaining = remaining.slice(1);
  }

  return results;
}

function MockMarkdown({
  children,
  components,
}: {
  children: string;
  components?: Record<string, (props: any) => ReactNode>;
}) {
  const content = typeof children === 'string' ? children : '';
  const rendered = parseMarkdownMinimal(content, components);
  return createElement('div', { 'data-testid': 'mock-markdown' }, rendered);
}

vi.mock('react-markdown', () => ({ __esModule: true, default: MockMarkdown }));
vi.mock('remark-gfm', () => ({ default: [] }));

import MessageContent from './MessageContent';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
  container?.remove();
});

// ---------------------------------------------------------------------------
// Tests: MessageContent rendering
// ---------------------------------------------------------------------------
//
// These tests focus on what MessageContent actually does:
//   1. ANSI code stripping before passing to ReactMarkdown
//   2. Component overrides for `code` (inline vs block code)
//   3. Component overrides for `a` (local file vs external links)
//
// Markdown parsing correctness is NOT tested here — that is
// react-markdown's responsibility.

describe('MessageContent', () => {
  // ------------------------------------------------------------------
  // Plain text & basic rendering
  // ------------------------------------------------------------------

  it('renders plain text content', () => {
    act(() => {
      root.render(createElement(MessageContent, { content: 'Hello world' }));
    });
    expect(container.textContent).toContain('Hello world');
  });

  it('renders empty content without crashing', () => {
    act(() => {
      root.render(createElement(MessageContent, { content: '' }));
    });
    expect(container.innerHTML).not.toBe('');
  });

  // ------------------------------------------------------------------
  // ANSI stripping (MessageContent's own logic)
  // ------------------------------------------------------------------

  it('strips ANSI escape codes from content', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '\x1B[31mRed text\x1B[0m',
      }));
    });
    expect(container.textContent).toContain('Red text');
    expect(container.textContent).not.toContain('\x1B');
  });

  it('strips multiple ANSI codes from content', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '\x1B[32mGreen\x1B[0m and \x1B[31mRed\x1B[0m',
      }));
    });
    expect(container.textContent).toContain('Green');
    expect(container.textContent).toContain('Red');
    expect(container.textContent).not.toContain('\x1B');
  });

  it('preserves content without ANSI codes', () => {
    act(() => {
      root.render(createElement(MessageContent, { content: 'Normal text' }));
    });
    expect(container.textContent).toBe('Normal text');
  });

  // ------------------------------------------------------------------
  // Code component override (inline vs block)
  // ------------------------------------------------------------------

  it('renders inline code with inline-code class via component override', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: 'Use `foo()` for something.',
      }));
    });
    const code = container.querySelector('.inline-code');
    expect(code).not.toBeNull();
    expect(code?.textContent).toBe('foo()');
  });

  it('renders fenced code block with code-block class and language label', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '```go\npackage main\n\nfunc main() {}\n```',
      }));
    });
    const pre = container.querySelector('.code-block');
    expect(pre).not.toBeNull();
    const lang = container.querySelector('.code-language');
    expect(lang).not.toBeNull();
    expect(lang?.textContent).toBe('go');
  });

  it('renders fenced code block with "text" as default language when none specified', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '```\nline one\nline two\n```',
      }));
    });
    const lang = container.querySelector('.code-language');
    expect(lang).not.toBeNull();
    expect(lang?.textContent).toBe('text');
  });

  it('renders code with newlines as block code even without language', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '```\na\nb\n```',
      }));
    });
    const pre = container.querySelector('.code-block');
    expect(pre).not.toBeNull();
  });

  it('renders fenced code block without language as inline code when no newlines', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '```singleLine```',
      }));
    });
    const pre = container.querySelector('.code-block');
    expect(pre).toBeNull();
    const code = container.querySelector('.inline-code');
    expect(code).not.toBeNull();
    expect(code?.textContent).toBe('singleLine');
  });

  // ------------------------------------------------------------------
  // Link component override (local file vs external)
  // ------------------------------------------------------------------

  it('renders external https links with target="_blank" and rel="noreferrer"', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '[Google](https://google.com)',
      }));
    });
    const link = container.querySelector('a');
    expect(link).not.toBeNull();
    expect(link?.getAttribute('href')).toBe('https://google.com');
    expect(link?.getAttribute('target')).toBe('_blank');
    expect(link?.getAttribute('rel')).toBe('noreferrer');
  });

  it('renders external http links with target="_blank"', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '[Link](http://example.com/page)',
      }));
    });
    const link = container.querySelector('a');
    expect(link?.getAttribute('target')).toBe('_blank');
  });

  it('renders mailto links as external (target="_blank")', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '[Email](mailto:test@example.com)',
      }));
    });
    const link = container.querySelector('a');
    expect(link?.getAttribute('target')).toBe('_blank');
  });

  it('renders anchor links as external (target="_blank")', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '[Section](#heading)',
      }));
    });
    const link = container.querySelector('a');
    expect(link?.getAttribute('target')).toBe('_blank');
  });

  it('renders local file links without target="_blank" and with editor event handler', () => {
    const handler = vi.fn((e: any) => e.preventDefault());
    window.addEventListener('sprout:open-in-editor', handler);

    act(() => {
      root.render(createElement(MessageContent, {
        content: '[Readme](./src/file.tsx)',
      }));
    });

    const link = container.querySelector('a');
    expect(link).not.toBeNull();
    expect(link?.getAttribute('href')).toBe('./src/file.tsx');
    expect(link?.getAttribute('target')).toBeNull();

    act(() => {
      link?.click();
    });

    expect(handler).toHaveBeenCalled();

    window.removeEventListener('sprout:open-in-editor', handler);
  });

  it('renders absolute local file paths without target="_blank"', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '[Path](/absolute/path/to/file.txt)',
      }));
    });
    const link = container.querySelector('a');
    expect(link?.getAttribute('target')).toBeNull();
  });

  it('preserves link text content', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: 'Visit [My Site](https://example.com)',
      }));
    });
    expect(container.textContent).toContain('My Site');
  });

  // ------------------------------------------------------------------
  // Markdown structural elements (delegated to mock; tests sanity)
  // ------------------------------------------------------------------

  it('renders bold text in a strong element', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '**bold text**',
      }));
    });
    expect(container.querySelector('strong')).not.toBeNull();
  });

  it('renders italic text in an em element', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '*italic text*',
      }));
    });
    expect(container.querySelector('em')).not.toBeNull();
  });

  it('renders strikethrough text in a del element', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '~~deleted~~',
      }));
    });
    expect(container.querySelector('del')).not.toBeNull();
  });

  it('renders headings', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '# Heading 1\n## Heading 2',
      }));
    });
    expect(container.querySelector('h1')).not.toBeNull();
    expect(container.querySelector('h2')).not.toBeNull();
  });

  it('renders list items', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '- Item 1\n- Item 2',
      }));
    });
    expect(container.querySelector('ul')).not.toBeNull();
    const items = container.querySelectorAll('li');
    expect(items).toHaveLength(2);
  });

  it('renders numbered list items', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '1. First\n2. Second',
      }));
    });
    expect(container.querySelector('ol')).not.toBeNull();
  });

  it('renders blockquotes', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '> This is a quote',
      }));
    });
    expect(container.querySelector('blockquote')).not.toBeNull();
  });

  it('renders horizontal rules', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '---',
      }));
    });
    expect(container.querySelector('hr')).not.toBeNull();
  });

  it('renders tables', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: '| Col A | Col B |\n|-------|-------|\n| 1 | 2 |',
      }));
    });
    expect(container.querySelector('table')).not.toBeNull();
  });

  it('renders multiple paragraphs for separate lines', () => {
    act(() => {
      root.render(createElement(MessageContent, {
        content: 'First paragraph.\n\nSecond paragraph.',
      }));
    });
    const paragraphs = container.querySelectorAll('p');
    expect(paragraphs).toHaveLength(2);
  });
});
