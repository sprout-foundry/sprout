import { getRelativePath } from './useSearchState';

interface PreviewData {
  file: string;
  startLine: number;
  snippet: Array<{ line_number: number; content: string; is_context: boolean }>;
}

interface PreviewPosition {
  x: number;
  y: number;
}

interface SemanticPreviewTooltipProps {
  previewData: PreviewData | null;
  previewPosition: PreviewPosition | null;
  onMouseLeave: () => void;
}

/**
 * Renders the floating code preview tooltip that appears on hover
 * over semantic search results.
 */
function SemanticPreviewTooltip({
  previewData,
  previewPosition,
  onMouseLeave,
}: SemanticPreviewTooltipProps): JSX.Element | null {
  if (!previewData || !previewPosition) return null;

  return (
    <div
      className="search-semantic-preview"
      style={{
        position: 'fixed',
        left: previewPosition.x,
        top: previewPosition.y,
        zIndex: 1000,
      }}
      onMouseEnter={() => {
        /* keep visible when hovering over preview */
      }}
      onMouseLeave={onMouseLeave}
    >
      <div className="search-semantic-preview-header">{getRelativePath(previewData.file)}</div>
      <pre className="search-semantic-preview-code">
        {previewData.snippet.map((line) => (
          <div
            key={line.line_number}
            className={`search-semantic-preview-line ${line.is_context ? 'search-semantic-preview-line--context' : ''}`}
          >
            <span className="search-semantic-preview-linenum">{line.line_number}</span>
            <span className="search-semantic-preview-content">{line.content}</span>
          </div>
        ))}
      </pre>
    </div>
  );
}

export default SemanticPreviewTooltip;
export type { PreviewData, PreviewPosition };
