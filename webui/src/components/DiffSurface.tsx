import { classifyDiffLine } from '../utils/format';
import './DiffSurface.css';

function DiffSurface({ diffText }: { diffText: string }) {
  if (!diffText) return null;
  const diffLines = diffText.split('\n');
  return (
    <div className="commit-detail-diff-surface">
      {diffLines.map((line, index) => {
        const lineClass = classifyDiffLine(line);
        return (
          <div key={`${index}-${line}`} className={`commit-detail-diff-line ${lineClass}`}>
            <span className="commit-detail-diff-line-number">{index + 1}</span>
            <span className="commit-detail-diff-line-text">{line || ' '}</span>
          </div>
        );
      })}
    </div>
  );
}

export default DiffSurface;
