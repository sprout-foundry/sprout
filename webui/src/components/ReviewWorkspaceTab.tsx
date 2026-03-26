import React from 'react';
import { ShieldCheck, Loader2, Wrench } from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { stripAnsiCodes } from '../utils/ansi';

interface DeepReviewResult {
  message: string;
  status: string;
  feedback: string;
  detailed_guidance?: string;
  suggested_new_prompt?: string;
  review_output: string;
  provider?: string;
  model?: string;
}

interface ReviewWorkspaceTabProps {
  review: DeepReviewResult | null;
  reviewError: string | null;
  reviewFixResult: string | null;
  reviewFixLogs: string[];
  reviewFixSessionID: string | null;
  isReviewLoading: boolean;
  isReviewFixing: boolean;
  onFixFromReview: () => void;
}

const ReviewWorkspaceTab: React.FC<ReviewWorkspaceTabProps> = ({
  review,
  reviewError,
  reviewFixResult,
  reviewFixLogs,
  reviewFixSessionID,
  isReviewLoading,
  isReviewFixing,
  onFixFromReview,
}) => {
  return (
    <div className="chat-shell review-workspace-shell">
      <div className="chat-main review-workspace-main">
        <div className="chat-container">
          {!review && !isReviewLoading && !reviewError ? (
            <div className="welcome-message">
              <div className="welcome-icon"><ShieldCheck size={32} /></div>
              <div className="welcome-text">No review generated yet.</div>
              <div className="welcome-hint">Stage files, then run review from the git sidebar.</div>
            </div>
          ) : null}

          {isReviewLoading ? (
            <div className="processing-indicator">
              <div className="processing-content">
                <div className="processing-spinner"><Loader2 size={14} /></div>
                <div className="processing-text">Running deep review…</div>
              </div>
            </div>
          ) : null}

          {reviewError ? (
            <div className="error-indicator">
              <div className="error-content">
                <div className="error-text">{reviewError}</div>
              </div>
            </div>
          ) : null}

          {review ? (
            <>
              <div className="message assistant" role="article" aria-label="Review summary">
                <div className="message-bubble">
                  <div className="message-content">
                    <div className="review-meta-strip">
                      <span className={`review-status-pill status-${review.status}`}>{review.status}</span>
                      {review.model ? <span>{review.model}</span> : null}
                    </div>
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>
                      {stripAnsiCodes(review.feedback || review.review_output)}
                    </ReactMarkdown>
                  </div>
                </div>
              </div>

              {review.detailed_guidance ? (
                <div className="message assistant" role="article" aria-label="Detailed guidance">
                  <div className="message-bubble">
                    <div className="message-content">
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>
                        {stripAnsiCodes(review.detailed_guidance)}
                      </ReactMarkdown>
                    </div>
                  </div>
                </div>
              ) : null}

              {review.suggested_new_prompt ? (
                <div className="message assistant" role="article" aria-label="Suggested prompt">
                  <div className="message-bubble">
                    <div className="message-content">
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>
                        {stripAnsiCodes(review.suggested_new_prompt)}
                      </ReactMarkdown>
                    </div>
                  </div>
                </div>
              ) : null}

              {reviewFixLogs.length > 0 ? (
                <div className="message assistant" role="article" aria-label="Review fix logs">
                  <div className="message-bubble">
                    <div className="message-content">
                      <div className="review-meta-strip">
                        <span>Fix session</span>
                        {reviewFixSessionID ? <span>{reviewFixSessionID}</span> : null}
                      </div>
                      <pre className="workspace-diff-pre">{reviewFixLogs.join('\n')}</pre>
                    </div>
                  </div>
                </div>
              ) : null}

              {reviewFixResult ? (
                <div className="message assistant" role="article" aria-label="Review fix result">
                  <div className="message-bubble">
                    <div className="message-content">
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>
                        {stripAnsiCodes(reviewFixResult)}
                      </ReactMarkdown>
                    </div>
                  </div>
                </div>
              ) : null}
            </>
          ) : null}
        </div>
      </div>

      <div className="input-container review-actions-bar">
        <button
          className="review-fix-btn"
          onClick={onFixFromReview}
          disabled={!review?.review_output || isReviewFixing || isReviewLoading}
        >
          <Wrench size={14} />
          {isReviewFixing ? 'Applying fixes…' : 'Fix From Review'}
        </button>
      </div>
    </div>
  );
};

export default ReviewWorkspaceTab;
