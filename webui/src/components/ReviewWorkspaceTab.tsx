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
  const fixLogPreview = useMemo(() => {
    if (reviewFixLogs.length === 0) {
      return '';
    }
    return reviewFixLogs.slice(-6).join('\n');
  }, [reviewFixLogs]);

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
                  {parsedDetailedGuidance.sections.length > 0 ? (
                    <div className="review-guidance-groups">
                      {parsedDetailedGuidance.sections.map((section) => (
                        <section key={section.id} className="review-guidance-section">
                          <div className="review-guidance-header">
                            <h4>{section.title}</h4>
                            <span>{section.entries.length} item{section.entries.length === 1 ? '' : 's'}</span>
                          </div>
                          <div className="review-guidance-list">
                            {section.entries.map((entry, index) => (
                              <article key={`${section.id}-${index}`} className="review-guidance-card">
                                <h5>{entry.issue}</h5>
                                {entry.file ? (
                                  <div className="review-guidance-row">
                                    <span className="review-guidance-label">File</span>
                                    <code>{entry.file}</code>
                                  </div>
                                ) : null}
                                {entry.evidence ? (
                                  <div className="review-guidance-row">
                                    <span className="review-guidance-label">Evidence</span>
                                    <p>{entry.evidence}</p>
                                  </div>
                                ) : null}
                                {entry.suggestion ? (
                                  <div className="review-guidance-row">
                                    <span className="review-guidance-label">Next Step</span>
                                    <p>{entry.suggestion}</p>
                                  </div>
                                ) : null}
                                {Object.entries(entry)
                                  .filter(([key, value]) => !['issue', 'file', 'evidence', 'suggestion'].includes(key) && typeof value === 'string' && value.trim())
                                  .map(([key, value]) => (
                                    <div key={key} className="review-guidance-row">
                                      <span className="review-guidance-label">
                                        {key.replace(/_/g, ' ').replace(/\b\w/g, (char) => char.toUpperCase())}
                                      </span>
                                      <p>{String(value)}</p>
                                    </div>
                                  ))}
                              </article>
                            ))}
                          </div>
                        </section>
                      ))}
                    </div>
                  ) : (
                    <MessageSegments content={detailedGuidanceMarkdown} />
                  )}
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
                  <details className="review-details-block">
                    <summary className="review-details-summary">
                      <span>Fix workflow logs</span>
                      <span>{reviewFixLogs.length} entries</span>
                    </summary>
                    <div className="review-details-content">
                      <MessageSegments content={reviewFixLogs.join('\n')} />
                    </div>
                  </details>
                  <div className="review-fix-preview">
                    <MessageSegments content={fixLogPreview} />
                  </div>
                </MessageBubble>
              ) : null}

              {reviewFixResult ? (
                <MessageBubble
                  type="assistant"
                  ariaLabel="Review fix result"
                  copyText={reviewFixResult}
                >
                  <details className="review-details-block" open>
                    <summary className="review-details-summary">
                      <span>Fix result</span>
                    </summary>
                    <div className="review-details-content">
                      <MessageSegments content={reviewFixResult} />
                    </div>
                  </details>
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
