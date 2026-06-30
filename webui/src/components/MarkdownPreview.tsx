import type { AnchorHTMLAttributes, HTMLAttributes, ReactNode } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkBreaks from 'remark-breaks';
import remarkGfm from 'remark-gfm';
import { stripAnsiCodes } from '../utils/ansi';
import { flattenMarkdownText, isMarkdownCodeBlock, isLocalFilePath } from '../utils/markdownCode';
import './MarkdownPreview.css';

interface MarkdownPreviewProps {
  content: string;
  scrollRef?: React.RefObject<HTMLDivElement>;
}

function MarkdownPreview({ content, scrollRef }: MarkdownPreviewProps): JSX.Element {
  return (
    <div className="markdown-preview" data-testid="markdown-preview">
      <div className="markdown-preview-body" ref={scrollRef}>
        <ReactMarkdown
          remarkPlugins={[remarkGfm, remarkBreaks]}
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
          }}
        >
          {stripAnsiCodes(content)}
        </ReactMarkdown>
      </div>
    </div>
  );
}

export default MarkdownPreview;
