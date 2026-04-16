import type { AnchorHTMLAttributes, HTMLAttributes, ReactNode } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { stripAnsiCodes } from '../utils/ansi';
import { flattenMarkdownText, isMarkdownCodeBlock } from '../utils/markdownCode';

interface MessageContentProps {
  content: string;
}

/** Returns true if href looks like a local file path rather than a URL. */
function isLocalFilePath(href: string | undefined): boolean {
  if (!href) return false;
  if (
    href.startsWith('http://') ||
    href.startsWith('https://') ||
    href.startsWith('//') ||
    href.startsWith('mailto:') ||
    href.startsWith('#') ||
    href.startsWith('javascript:')
  ) {
    return false;
  }
  // Must look like a path: contains a slash, or is a bare filename with a code extension
  return href.includes('/') || /\.\w{1,10}$/.test(href);
}

function MessageContent({ content }: MessageContentProps): JSX.Element {
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      components={{
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
            <pre className="code-block">
              <span className="code-language">{language || 'text'}</span>
              <code className={className} {...props}>
                {children}
              </code>
            </pre>
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
                  window.dispatchEvent(new CustomEvent('ledit:open-in-editor', { detail: { path: href } }));
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
      }}
    >
      {stripAnsiCodes(content)}
    </ReactMarkdown>
  );
}

export default MessageContent;
