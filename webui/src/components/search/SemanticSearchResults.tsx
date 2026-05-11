import type { MouseEvent } from 'react';
import type { SemanticSearchResult, SemanticResultCallbacks } from './types';
import { getRelativePath } from './useSearchState';

interface SemanticSearchResultsProps extends SemanticResultCallbacks {
  results: SemanticSearchResult[];
}

/**
 * Renders semantic search results: file-level cards and code-unit cards
 * with similarity bars and optional cluster badges.
 */
function SemanticSearchResults({
  results,
  onFileClick,
  onMouseEnter,
  onMouseLeave,
}: SemanticSearchResultsProps): JSX.Element {
  return (
    <>
      {results.map((result) => {
        if (result.type === 'file') {
          return <SemanticFileResult key={`file-${result.file}`} result={result} onFileClick={onFileClick} />;
        }

        return (
          <SemanticCodeUnitResult
            key={`${result.file}-${result.start_line}`}
            result={result}
            onFileClick={onFileClick}
            onMouseEnter={onMouseEnter}
            onMouseLeave={onMouseLeave}
          />
        );
      })}
    </>
  );
}

// ── Internal: file-level result card ──────────────────────────

interface SemanticFileResultProps {
  result: SemanticSearchResult;
  onFileClick: (filePath: string, lineNumber?: number) => void;
}

function SemanticFileResult({ result, onFileClick }: SemanticFileResultProps): JSX.Element {
  const hasCluster = result.cluster_id && result.cluster_id > 0;

  return (
    <div
      className={`search-semantic-result search-semantic-result--file search-match-row search-match-row--clickable ${hasCluster ? 'search-semantic-result--clustered' : ''}`}
      role="button"
      tabIndex={0}
      onClick={() => onFileClick(result.file, 1)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onFileClick(result.file, 1);
        }
      }}
    >
      <div className="search-semantic-result-main">
        <span className="search-semantic-result-name">File</span>
      </div>
      <div className="search-semantic-result-meta">
        <span className="search-semantic-result-file">{getRelativePath(result.file)}</span>
        <SimilarityBar similarity={result.similarity} />
        <span className="search-semantic-result-similarity">{(result.similarity * 100).toFixed(0)}%</span>
        {hasCluster && (
          <span className="search-semantic-result-cluster-badge" title={`Cluster ${result.cluster_id}`}>
            {result.cluster_id}
          </span>
        )}
      </div>
    </div>
  );
}

// ── Internal: code-unit result card ──────────────────────────

interface SemanticCodeUnitResultProps {
  result: SemanticSearchResult;
  onFileClick: (filePath: string, lineNumber?: number) => void;
  onMouseEnter: (e: MouseEvent<HTMLDivElement>, result: SemanticSearchResult) => void;
  onMouseLeave: () => void;
}

function SemanticCodeUnitResult({
  result,
  onFileClick,
  onMouseEnter,
  onMouseLeave,
}: SemanticCodeUnitResultProps): JSX.Element {
  const hasCluster = result.cluster_id && result.cluster_id > 0;

  return (
    <div
      className={`search-semantic-result search-match-row search-match-row--clickable ${hasCluster ? 'search-semantic-result--clustered' : ''}`}
      role="button"
      tabIndex={0}
      onClick={() => onFileClick(result.file, result.start_line)}
      onMouseEnter={(e) => onMouseEnter(e, result)}
      onMouseLeave={onMouseLeave}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onFileClick(result.file, result.start_line);
        }
      }}
    >
      <div className="search-semantic-result-main">
        <span className="search-semantic-result-name">{result.name}</span>
        {result.signature && <span className="search-semantic-result-signature">{result.signature}</span>}
      </div>
      <div className="search-semantic-result-meta">
        <span className="search-semantic-result-file">{getRelativePath(result.file)}</span>
        <span className="search-semantic-result-lines">
          {result.start_line}–{result.end_line}
        </span>
        {result.language && <span className="search-semantic-result-lang">{result.language}</span>}
        <SimilarityBar similarity={result.similarity} />
        <span className="search-semantic-result-similarity">{(result.similarity * 100).toFixed(0)}%</span>
        {hasCluster && (
          <span className="search-semantic-result-cluster-badge" title={`Cluster ${result.cluster_id}`}>
            {result.cluster_id}
          </span>
        )}
      </div>
    </div>
  );
}

// ── Internal: similarity progress bar ────────────────────────

function SimilarityBar({ similarity }: { similarity: number }): JSX.Element {
  return (
    <div className="search-semantic-result-similarity-bar">
      <div
        className="search-semantic-result-similarity-fill"
        style={{
          width: `${similarity * 100}%`,
          backgroundColor: similarity > 0.85 ? 'var(--accent-success)' : 'var(--accent-primary)',
        }}
      />
    </div>
  );
}

export default SemanticSearchResults;
