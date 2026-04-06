import type { AnchorHTMLAttributes, HTMLAttributes, ReactNode } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { stripAnsiCodes } from '../utils/ansi';
import { flattenMarkdownText, isMarkdownCodeBlock } from '../utils/markdownCode';

interface MessageContentProps {
  content: string;
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
