import { memo, useEffect, type CSSProperties } from 'react';
import './Skeleton.css';

export interface SkeletonProps {
  /** Width - CSS value (e.g. '100%', '200px', '60%') */
  width?: string | number;
  /** Height - CSS value (e.g. '20px', '1em') */
  height?: string | number;
  /** Border radius */
  radius?: string;
  /** Additional CSS class */
  className?: string;
  /** Inline styles */
  style?: CSSProperties;
}

const shimmer = `
@keyframes skeleton-shimmer {
  0% { background-position: -200% 0; }
  100% { background-position: 200% 0; }
}`;

let styleInjected = false;

export const Skeleton = memo(function Skeleton({
  width = '100%',
  height = '20px',
  radius = 'var(--radius-sm, 4px)',
  className,
  style,
}: SkeletonProps) {
  useEffect(() => {
    if (styleInjected || typeof document === 'undefined') return;
    if (document.getElementById('skeleton-keyframes')) return;
    const el = document.createElement('style');
    el.id = 'skeleton-keyframes';
    el.textContent = shimmer;
    document.head.appendChild(el);
    styleInjected = true;
  }, []);

  return (
    <div
      className={`skeleton ${className || ''}`}
      style={{
        width: typeof width === 'number' ? `${width}px` : width,
        height: typeof height === 'number' ? `${height}px` : height,
        borderRadius: radius,
        ...style,
      }}
    />
  );
});

/** A row of skeleton lines simulating text content */
export interface SkeletonTextProps {
  /** Number of lines */
  lines?: number;
  /** Gap between lines */
  gap?: string;
  /** Line height */
  lineHeight?: string;
  /** Width of last line (percentage) - defaults to 60% */
  lastLineWidth?: string;
  /** Additional className on wrapper */
  className?: string;
}

export const SkeletonText = memo(function SkeletonText({
  lines = 3,
  gap = '8px',
  lineHeight = '14px',
  lastLineWidth = '60%',
  className,
}: SkeletonTextProps) {
  return (
    <div className={`skeleton-text ${className || ''}`} style={{ display: 'flex', flexDirection: 'column', gap }}>
      {Array.from({ length: lines }, (_, i) => (
        <Skeleton
          key={i}
          width={i === lines - 1 ? lastLineWidth : '100%'}
          height={lineHeight}
        />
      ))}
    </div>
  );
});

export default Skeleton;
