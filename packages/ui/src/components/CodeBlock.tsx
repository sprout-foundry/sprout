import { useCallback, useEffect, useRef, useState } from 'react';
import { Copy, Check } from 'lucide-react';
import type { ReactNode } from 'react';
import { copyToClipboard } from '../utils/clipboard';

interface CodeBlockProps {
  language: string;
  codeText: string;
  className?: string;
  children: ReactNode;
}

const COPIED_DURATION_MS = 1500;

function CodeBlock({ language, codeText, className, children }: CodeBlockProps): JSX.Element {
  const [copied, setCopied] = useState(false);
  const timeoutRef = useRef<ReturnType<typeof setTimeout>>();

  useEffect(() => () => {
    if (timeoutRef.current) clearTimeout(timeoutRef.current);
  }, []);

  const handleCopy = useCallback(async () => {
    if (copied) return;
    await copyToClipboard(codeText);
    setCopied(true);
    timeoutRef.current = setTimeout(() => setCopied(false), COPIED_DURATION_MS);
  }, [copied, codeText]);

  return (
    <pre className="code-block">
      {language && <span className="code-language">{language}</span>}
      <button
        className={`code-copy-button${copied ? ' copied' : ''}`}
        type="button"
        title="Copy code"
        aria-label="Copy code"
        onClick={handleCopy}
      >
        {copied ? <Check size={13} /> : <Copy size={13} />}
      </button>
      <code className={className}>{children}</code>
    </pre>
  );
}

export default CodeBlock;
