/** Determine the display label and CSS class for a git file status. */
export function getStatusInfo(status: string): { label: string; className: string } {
  const s = status.charAt(0).toUpperCase();
  switch (s) {
    case 'A': return { label: 'A', className: 'status-a' };
    case 'M': return { label: 'M', className: 'status-m' };
    case 'D': return { label: 'D', className: 'status-d' };
    case 'R': return { label: 'R', className: 'status-r' };
    case 'C': return { label: 'C', className: 'status-c' };
    default: return { label: status || '?', className: 'status-unknown' };
  }
}
