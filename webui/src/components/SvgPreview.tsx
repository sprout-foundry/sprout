import { Code2, Image as ImageIcon } from 'lucide-react';
import './SvgPreview.css';

interface SvgPreviewProps {
  content: string;
  fileName: string;
  sourcePath?: string;
}

function SvgPreview({ content, fileName, sourcePath }: SvgPreviewProps): JSX.Element {
  if (!content.trim()) {
    return (
      <div className="svg-preview-empty">
        <ImageIcon size={40} />
        <div className="svg-preview-empty-title">No SVG content loaded</div>
      </div>
    );
  }

  return (
    <div className="svg-preview">
      <div className="svg-preview-header">
        <div className="svg-preview-title">
          <ImageIcon size={14} />
          <span>{fileName}</span>
        </div>
        {sourcePath ? (
          <div className="svg-preview-meta">
            <Code2 size={12} />
            <span>{sourcePath}</span>
          </div>
        ) : null}
      </div>
      <div className="svg-preview-canvas">
        <iframe className="svg-preview-frame" sandbox="" title={`${fileName} preview`} srcDoc={content} />
      </div>
    </div>
  );
}

export default SvgPreview;
