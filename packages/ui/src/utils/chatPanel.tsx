/**
 * Escape HTML special characters to prevent XSS.
 */
export function escapeHtml(text: string): string {
  const map: Record<string, string> = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  };
  return text.replace(/[&<>"']/g, (char) => map[char]);
}

/**
 * Format timestamp for display.
 */
export function formatTimestamp(timestamp?: Date | number): string {
  if (!timestamp) return '';
  const date = typeof timestamp === 'number' ? new Date(timestamp) : timestamp;
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);

  if (diffMins < 1) return 'Just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffMins < 1440) return `${Math.floor(diffMins / 60)}h ago`;
  return date.toLocaleDateString();
}

/**
 * Simple markdown-like parser for basic formatting.
 * Handles code blocks, bold, and italic.
 */
export function parseMarkdown(text: string): React.ReactNode {
  const lines = text.split('\n');
  const result: React.ReactNode[] = [];
  let inCodeBlock = false;
  let codeBlockContent: string[] = [];
  let blockStartLine = 0;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];

    if (line.trim().startsWith('```')) {
      if (inCodeBlock) {
        result.push(
          <pre key={`code-${blockStartLine}`} className="chatpanel-code-block">
            <code>{escapeHtml(codeBlockContent.join('\n'))}</code>
          </pre>,
        );
        codeBlockContent = [];
        inCodeBlock = false;
      } else {
        inCodeBlock = true;
        blockStartLine = i;
      }
      continue;
    }

    if (inCodeBlock) {
      codeBlockContent.push(line);
      continue;
    }

    let formattedLine = escapeHtml(line);

    formattedLine = formattedLine.replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>');
    formattedLine = formattedLine.replace(/__(.*?)__/g, '<strong>$1</strong>');
    formattedLine = formattedLine.replace(/\*(.*?)\*/g, '<em>$1</em>');
    formattedLine = formattedLine.replace(/_(.*?)_/g, '<em>$1</em>');
    formattedLine = formattedLine.replace(/`(.*?)`/g, '<code class="chatpanel-inline-code">$1</code>');

    result.push(
      <p key={`line-${i}`} dangerouslySetInnerHTML={{ __html: formattedLine || '&nbsp;' }} />,
    );
  }

  if (inCodeBlock) {
    result.push(
      <pre key={`code-${blockStartLine}`} className="chatpanel-code-block">
        <code>{escapeHtml(codeBlockContent.join('\n'))}</code>
      </pre>,
    );
  }

  return <>{result}</>;
}
