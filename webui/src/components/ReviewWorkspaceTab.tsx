import React, { useMemo } from 'react';
import { ShieldCheck, Loader2, Wrench, AlertTriangle } from 'lucide-react';
import MessageSegments from './MessageSegments';
import MessageBubble from './MessageBubble';
import { parseReviewGuidance, reviewGuidanceToMarkdown } from '../utils/reviewFormatting';

interface DeepReviewResult {
  message: string;
  status: string;
  feedback: string;
  detailed_guidance?: string;
  suggested_new_prompt?: string;
  review_output: string;
  provider?: string;
  model?: string;
  warnings?: string[];
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
  const parsedDetailedGuidance = useMemo(
    () => parseReviewGuidance(review?.detailed_guidance || ''),
    [review?.detailed_guidance]
  );
  const detailedGuidanceMarkdown = useMemo(
    () => reviewGuidanceToMarkdown(parsedDetailedGuidance),
    [parsedDetailedGuidance]
  );

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
              <MessageBubble
                type="assistant"
                ariaLabel="Review summary"
                copyText={review.feedback || review.review_output}
              >
                <div className="review-meta-strip">
                  <span className={`review-status-pill status-${review.status}`}>{review.status}</span>
                  {review.model ? <span>{review.model}</span> : null}
                </div>
                <MessageSegments content={review.feedback || review.review_output} />
              </MessageBubble>

              {review.warnings && review.warnings.length > 0 ? (
                <MessageBubble
                  type="assistant"
                  ariaLabel="Review warnings"
                  copyText={review.warnings.join('\n')}
                >
                  <div className="review-meta-strip review-warning-strip">
                    <span><AlertTriangle size={14} /></span>
                    <span>Warnings</span>
                  </div>
                  <MessageSegments content={review.warnings.map((warning) => `- ${warning}`).join('\n')} />
                </MessageBubble>
              ) : null}

              {review.detailed_guidance ? (
                <MessageBubble
                  type="assistant"
                  ariaLabel="Detailed guidance"
                  copyText={detailedGuidanceMarkdown}
                >
                  <MessageSegments content={detailedGuidanceMarkdown} />
                </MessageBubble>
              ) : null}

              {review.suggested_new_prompt ? (
                <MessageBubble
                  type="assistant"
                  ariaLabel="Suggested prompt"
                  copyText={review.suggested_new_prompt}
                >
                  <MessageSegments content={review.suggested_new_prompt} />
                </MessageBubble>
              ) : null}

              {reviewFixLogs.length > 0 ? (
                <MessageBubble
                  type="assistant"
                  ariaLabel="Review fix logs"
                  copyText={reviewFixLogs.join('\n')}
                >
                  <div className="review-meta-strip">
                    <span>Fix session</span>
                    {reviewFixSessionID ? <span>{reviewFixSessionID}</span> : null}
                  </div>
                  <MessageSegments content={reviewFixLogs.join('\n')} />
                </MessageBubble>
              ) : null}

              {reviewFixResult ? (
                <MessageBubble
                  type="assistant"
                  ariaLabel="Review fix result"
                  copyText={reviewFixResult}
                >
                  <MessageSegments content={reviewFixResult} />
                </MessageBubble>
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
