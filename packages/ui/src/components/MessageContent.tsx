import { memo, useDeferredValue } from 'react';
import type { AnchorHTMLAttributes, HTMLAttributes, ReactNode } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkBreaks from 'remark-breaks';
import remarkGfm from 'remark-gfm';
import { stripAnsiCodes } from '../utils/ansi';
import { flattenMarkdownText, isMarkdownCodeBlock, isLocalFilePath } from '../utils/markdownCode';
import CodeBlock from './CodeBlock';

interface MessageContentProps {
  content: string;
}

/**
 * Renderer overrides for the markdown tree. Module scope so its identity is
 * stable: a fresh object per render made react-markdown treat every node as
 * new, and it closes over nothing.
 */
const MARKDOWN_COMPONENTS = {
  code({ className, children, ...props }: HTMLAttributes<HTMLElement> & { children?: ReactNode }) {
    const languageMatch = /language-(\w+)/.exec(className || '');
    const language = languageMatch ? languageMatch[1] : '';
    const codeText = flattenMarkdownText(children);
    const isBlockCode = isMarkdownCodeBlock(className, codeText);

    if (!isBlockCode) {
      return (
        <code className="inline-code" {...props}>
          {children}
        </code>
      );
    }

    return (
      <CodeBlock language={language || 'text'} codeText={codeText} className={className}>
        {children}
      </CodeBlock>
    );
  },
  a({ href, children, ...props }: AnchorHTMLAttributes<HTMLAnchorElement>) {
    if (isLocalFilePath(href)) {
      return (
        <a
          href={href}
          {...props}
          onClick={(e) => {
            e.preventDefault();
            window.dispatchEvent(new CustomEvent('sprout:open-in-editor', { detail: { path: href } }));
          }}
        >
          {children}
        </a>
      );
    }
    return (
      <a href={href} target="_blank" rel="noreferrer" {...props}>
        {children}
      </a>
    );
  },
};

/**
 * The markdown tree itself, memoized on the text it parses.
 *
 * Splitting this out matters: the deferred value keeps its previous identity
 * on the urgent render that follows a content change, so this component bails
 * there instead of re-parsing text it already rendered.
 */
const MarkdownBody = memo(function MarkdownBody({ content }: { content: string }): JSX.Element {
  return (
    <ReactMarkdown
      // A single newline is a line break: chat answers use it for lists,
      // steps and "one per line" output, which plain markdown folded into
      // one paragraph.
      remarkPlugins={[remarkGfm, remarkBreaks]}
      components={MARKDOWN_COMPONENTS}
    >
      {stripAnsiCodes(content)}
    </ReactMarkdown>
  );
});

/**
 * Memoized on `content`: the chat re-renders on every stream flush and tool
 * event, and without this bail-out the full ReactMarkdown + remark-gfm parse
 * re-ran on every render for every visible message — the dominant CPU cost
 * while watching an active agent session.
 *
 * The in-flight message is the exception: its content changes on every flush,
 * so the memo cannot help it. `useDeferredValue` lets React drop intermediate
 * values while the main thread is busy — the parse then runs on the latest
 * text once the urgent work is done, instead of once per flush. That is also
 * what keeps typing (in the composer, the editor, the terminal) responsive
 * while an answer streams.
 */
const MessageContent = memo(function MessageContent({ content }: MessageContentProps): JSX.Element {
  const deferredContent = useDeferredValue(content);
  return <MarkdownBody content={deferredContent} />;
});

export default MessageContent;
