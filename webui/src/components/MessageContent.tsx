import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { stripAnsiCodes } from '../utils/ansi';

interface MessageContentProps {
  content: string;
}

const MessageContent: React.FC<MessageContentProps> = ({ content }) => (
  <ReactMarkdown
    remarkPlugins={[remarkGfm]}
    components={{
      code({ inline, className, children, ...props }: any) {
        const languageMatch = /language-(\w+)/.exec(className || '');
        const language = languageMatch ? languageMatch[1] : '';

        if (inline) {
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
      a({ href, children, ...props }: any) {
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

export default MessageContent;
