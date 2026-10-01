import './ApprovalOriginNotice.css';

/** Names the chat an approval dialog belongs to when it isn't the one on screen. */
export function ApprovalOriginNotice({ chatName }: { chatName?: string }) {
  if (!chatName) return null;
  return (
    <div className="approval-origin-notice" role="status">
      Approval requested in chat “{chatName}”
    </div>
  );
}
