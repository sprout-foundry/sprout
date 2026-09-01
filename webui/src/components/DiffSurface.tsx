import React, { useEffect, useMemo, useRef, useState } from 'react';
import { displayPath, parseUnifiedDiff, type DiffLineRow, type ParsedDiffFile } from '../utils/unifiedDiff';
import type { WordDiffPart } from '../utils/wordDiff';
import './DiffSurface.css';

/** Rows rendered above/below the viewport when virtualizing. */
const OVERSCAN_ROWS = 24;
/** Row height in px — must match --diff-row-height in DiffSurface.css. */
const ROW_HEIGHT = 20;
/** Render virtualized beyond this many total rows. */
const VIRTUALIZE_THRESHOLD = 600;

interface DiffSurfaceProps {
  diffText: string;
  /** Optional label for the header eyebrow. */
  title?: string;
  /** File path shown in the header (defaults to parsed path). */
  path?: string;
}

function LineNumbers({ row }: { row: DiffLineRow }): JSX.Element {
  return (
    <>
      <span className="diff-ln diff-ln-old">{row.oldNumber ?? ''}</span>
      <span className="diff-ln diff-ln-new">{row.newNumber ?? ''}</span>
    </>
  );
}

function WordSpans({ parts }: { parts: WordDiffPart[] }): JSX.Element {
  return (
    <>
      {parts.map((p, i) =>
        p.changed ? (
          <mark key={i} className="diff-word-changed">
            {p.text}
          </mark>
        ) : (
          <React.Fragment key={i}>{p.text}</React.Fragment>
        ),
      )}
    </>
  );
}

function HunkHeaderRow({
  hunk,
}: {
  hunk: { oldStart: number; oldCount: number; newStart: number; newCount: number; headerContext?: string };
}): JSX.Element {
  return (
    <div className="diff-row diff-row-hunk">
      <span className="diff-ln" />
      <span className="diff-ln" />
      <span className="diff-sign"> </span>
      <code className="diff-text diff-hunk-context">
        @@ -{hunk.oldStart},{hunk.oldCount} +{hunk.newStart},{hunk.newCount} @@
        {hunk.headerContext ? ` ${hunk.headerContext}` : ''}
      </code>
    </div>
  );
}

function DiffRow({ row }: { row: DiffLineRow }): JSX.Element {
  return (
    <div className={`diff-row diff-row-${row.type}`}>
      <LineNumbers row={row} />
      <span className="diff-sign">{row.type === 'add' ? '+' : row.type === 'del' ? '−' : ' '}</span>
      <code className="diff-text">{row.wordDiff ? <WordSpans parts={row.wordDiff} /> : row.text || ' '}</code>
    </div>
  );
}

function StatsBar({ additions, deletions }: { additions: number; deletions: number }): JSX.Element {
  const total = additions + deletions;
  // GitHub-style 5-block bar: proportional split, minimum one block per
  // nonzero side so "+1 −0" and "+0 −1" still show a colored block.
  let addBlocks = 0;
  let delBlocks = 0;
  if (total > 0) {
    addBlocks = Math.round((additions / total) * 5);
    delBlocks = 5 - addBlocks;
    if (additions > 0 && addBlocks === 0) addBlocks = 1;
    if (deletions > 0 && delBlocks === 0) {
      delBlocks = 1;
      addBlocks = Math.max(0, addBlocks - 1);
    }
  }
  const blocks = Array.from({ length: 5 }, (_, i) => (i < addBlocks ? 'add' : i >= 5 - delBlocks ? 'del' : 'empty'));
  return (
    <span className="diff-stats" aria-label={`${additions} additions, ${deletions} deletions`}>
      <span className="diff-stats-num diff-stats-add">+{additions}</span>{' '}
      <span className="diff-stats-num diff-stats-del">−{deletions}</span>
      <span className="diff-stats-blocks" aria-hidden="true">
        {blocks.map((b, i) => (
          <span key={i} className={`diff-stats-block diff-stats-block-${b}`} />
        ))}
      </span>
    </span>
  );
}

/** One file section in a multi-file diff. */
function FileSection({ file }: { file: ParsedDiffFile }): JSX.Element {
  return (
    <section className="diff-file">
      <header className="diff-file-header">
        <span className="diff-file-path">{displayPath(file)}</span>
        <StatsBar additions={file.additions} deletions={file.deletions} />
      </header>
      {file.binary ? (
        <div className="diff-binary-note">Binary file (no textual diff)</div>
      ) : (
        file.hunks.map((hunk, hi) => (
          <div key={hi} className="diff-hunk">
            <HunkHeaderRow hunk={hunk} />
            {hunk.rows.map((row, ri) => (
              <DiffRow key={ri} row={row} />
            ))}
          </div>
        ))
      )}
    </section>
  );
}

/**
 * DiffSurface — GitHub-style unified diff renderer.
 *
 * Replaces the raw-text view: real old/new line numbers from @@ headers,
 * word-level intraline highlights on paired -/+ runs, +N/−M stats, no-wrap
 * rows with horizontal scrolling, and windowed rendering for large diffs.
 * Fills its container via flex (no viewport-height math), so it works in
 * any split layout.
 */
export function DiffSurface({ diffText, title, path }: DiffSurfaceProps): JSX.Element | null {
  const parsed = useMemo(() => parseUnifiedDiff(diffText), [diffText]);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [viewportH, setViewportH] = useState(600);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const measure = () => {
      setViewportH(el.clientHeight);
      setScrollTop(el.scrollTop);
    };
    measure();
    el.addEventListener('scroll', measure, { passive: true });
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => {
      el.removeEventListener('scroll', measure);
      ro.disconnect();
    };
  }, [diffText]);

  const files = useMemo(() => parsed.files.filter((f) => f.hunks.length > 0 || f.binary), [parsed]);
  const singleFile = files.length === 1 ? files[0] : null;

  // Single-file layout: one flat row list (hunk header rows interleaved).
  const flatRows = useMemo<DiffLineRow[]>(() => {
    if (!singleFile || singleFile.binary) return [];
    const rows: DiffLineRow[] = [];
    for (const hunk of singleFile.hunks) {
      rows.push({
        type: 'hunk-header',
        text: `@@ -${hunk.oldStart},${hunk.oldCount} +${hunk.newStart},${hunk.newCount} @@${
          hunk.headerContext ? ` ${hunk.headerContext}` : ''
        }`,
        oldNumber: null,
        newNumber: null,
      });
      rows.push(...hunk.rows);
    }
    return rows;
  }, [singleFile]);

  if (!diffText) return null;

  // Virtualization window for large single-file diffs.
  const virtualize = flatRows.length > VIRTUALIZE_THRESHOLD;
  const firstIdx = virtualize ? Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN_ROWS) : 0;
  const visibleCount = virtualize ? Math.ceil(viewportH / ROW_HEIGHT) + OVERSCAN_ROWS * 2 : flatRows.length;
  const lastIdx = virtualize ? Math.min(flatRows.length, firstIdx + visibleCount) : flatRows.length;
  const visibleRows = flatRows.slice(firstIdx, lastIdx);

  return (
    <div className="diff-surface" data-testid="diff-surface">
      <div className="diff-surface-header">
        <div className="diff-surface-title">
          <span className="diff-surface-eyebrow">{title || 'Diff'}</span>
          <span className="diff-surface-path">{path || (singleFile ? displayPath(singleFile) : '')}</span>
        </div>
        {(parsed.additions > 0 || parsed.deletions > 0) && (
          <StatsBar additions={parsed.additions} deletions={parsed.deletions} />
        )}
      </div>
      <div className="diff-surface-scroll" ref={scrollRef}>
        {singleFile ? (
          singleFile.binary ? (
            <div className="diff-binary-note">Binary file (no textual diff)</div>
          ) : virtualize ? (
            <>
              <div style={{ height: firstIdx * ROW_HEIGHT }} aria-hidden="true" />
              {visibleRows.map((row, i) => (
                <DiffRow key={firstIdx + i} row={row} />
              ))}
              <div style={{ height: (flatRows.length - lastIdx) * ROW_HEIGHT }} aria-hidden="true" />
            </>
          ) : (
            flatRows.map((row, i) => <DiffRow key={i} row={row} />)
          )
        ) : files.length > 1 ? (
          files.map((file, fi) => <FileSection key={fi} file={file} />)
        ) : diffText.trim() ? (
          // No parseable hunks, but there IS text — usually a human-readable
          // message from the backend ("(no staged changes)", binary notes,
          // "file too large for inline diff") or the placeholder written by
          // CommitDetailPanel. Show it verbatim instead of hiding it behind
          // a generic empty state.
          <pre className="diff-surface-raw">{diffText}</pre>
        ) : (
          <div className="diff-surface-empty">No changes</div>
        )}
      </div>
    </div>
  );
}

export default DiffSurface;
