import type { MouseEvent } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';
import type { SearchResult, ResultRowCallbacks } from './types';
import { getRelativePath } from './useSearchState';

interface SearchResultsProps extends ResultRowCallbacks {
  results: SearchResult[];
}

/**
 * Renders text search results as expandable file groups with match rows.
 */
function SearchResults({
  results,
  onFileClick,
  onRowContextMenu,
  onFileHeaderContextMenu,
  toggleFile,
  highlightMatch,
  expandedFiles,
}: SearchResultsProps): JSX.Element {
  return (
    <>
      {results.map((result) => {
        const isExpanded = expandedFiles.has(result.file);
        const relativePath = getRelativePath(result.file);

        return (
          <div key={result.file} className="search-file-group">
            <div
              className="search-file-header"
              onClick={() => toggleFile(result.file)}
              onContextMenu={(e) => onFileHeaderContextMenu(e, result.file)}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  toggleFile(result.file);
                }
              }}
            >
              <span className="search-expand-icon">
                {isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
              </span>
              <span className="search-file-path">{relativePath}</span>
              <span className="search-file-badge">{result.match_count}</span>
            </div>

            {isExpanded && (
              <div className="search-file-matches">
                {result.matches.map((match, idx) => (
                  <SearchMatchGroup
                    key={idx}
                    file={result.file}
                    match={match}
                    onFileClick={onFileClick}
                    onRowContextMenu={onRowContextMenu}
                    highlightMatch={highlightMatch}
                  />
                ))}
              </div>
            )}
          </div>
        );
      })}
    </>
  );
}

// ── Internal: single match group (context_before + hit + context_after) ──

interface SearchMatchGroupProps {
  file: string;
  match: SearchResult['matches'][number];
  onFileClick: (filePath: string, lineNumber?: number) => void;
  onRowContextMenu: (e: MouseEvent, filePath: string, lineNumber: number, lineText: string) => void;
  highlightMatch: (line: string, colStart: number, colEnd: number) => React.ReactNode;
}

function SearchMatchGroup({
  file,
  match,
  onFileClick,
  onRowContextMenu,
  highlightMatch,
}: SearchMatchGroupProps): JSX.Element {
  return (
    <div className="search-match">
      {match.context_before.map((ctx, i) => {
        const contextLineNumber = match.line_number - (match.context_before.length - i);
        return (
          <SearchMatchRow
            key={`before-${i}`}
            filePath={file}
            lineNumber={contextLineNumber}
            text={ctx}
            onFileClick={onFileClick}
            onContextMenu={onRowContextMenu}
          />
        );
      })}

      {/* Hit row */}
      <div
        className="search-match-row search-match-row--hit search-match-row--clickable"
        role="button"
        tabIndex={0}
        onClick={() => onFileClick(file, match.line_number)}
        onContextMenu={(e) => onRowContextMenu(e, file, match.line_number, match.line)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            onFileClick(file, match.line_number);
          }
        }}
      >
        <span className="search-match-line-number">{match.line_number}</span>
        <div className="search-match-line">
          {highlightMatch(match.line, match.column_start, match.column_end)}
        </div>
      </div>

      {match.context_after.map((ctx, i) => {
        const afterLineNumber = match.line_number + i + 1;
        return (
          <SearchMatchRow
            key={`after-${i}`}
            filePath={file}
            lineNumber={afterLineNumber}
            text={ctx}
            onFileClick={onFileClick}
            onContextMenu={onRowContextMenu}
          />
        );
      })}
    </div>
  );
}

// ── Internal: single context / hit row ──────────────────────────────

interface SearchMatchRowProps {
  filePath: string;
  lineNumber: number;
  text: string;
  onFileClick: (filePath: string, lineNumber?: number) => void;
  onContextMenu: (e: MouseEvent, filePath: string, lineNumber: number, lineText: string) => void;
}

function SearchMatchRow({
  filePath,
  lineNumber,
  text,
  onFileClick,
  onContextMenu,
}: SearchMatchRowProps): JSX.Element {
  return (
    <div
      className="search-match-row search-match-row--context search-match-row--clickable"
      role="button"
      tabIndex={0}
      onClick={() => onFileClick(filePath, lineNumber)}
      onContextMenu={(e) => onContextMenu(e, filePath, lineNumber, text)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onFileClick(filePath, lineNumber);
        }
      }}
    >
      <span className="search-match-line-number">{lineNumber}</span>
      <div className="search-match-line">{text}</div>
    </div>
  );
}

export default SearchResults;
