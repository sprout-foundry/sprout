import React, { useMemo, useCallback, useState, useEffect } from 'react';
import { ShieldCheck, Loader2, Wrench } from 'lucide-react';
import MessageSegments from './MessageSegments';
import MessageBubble from './MessageBubble';
import MessageContent from './MessageContent';
import InlinePillRow, { type InlinePillItem } from './InlinePillRow';
import { parseReviewGuidance, reviewGuidanceToMarkdown } from '../utils/reviewFormatting';
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
  onFixFromReview: (options?: { fixPrompt?: string; selectedItems?: string[] }) => void;
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
  const [checkedItems, setCheckedItems] = useState<Set<string>>(new Set());
  const [fixPrompt, setFixPrompt] = useState('');

  const parsedDetailedGuidance = useMemo(
    () => parseReviewGuidance(review?.detailed_guidance || ''),
    [review?.detailed_guidance]
  );
  const detailedGuidanceMarkdown = useMemo(
    () => reviewGuidanceToMarkdown(parsedDetailedGuidance),
    [parsedDetailedGuidance]
  );
  const reviewMetaItems = useMemo<InlinePillItem[]>(() => {
    if (!review) {
      return [];
    }
    const items: InlinePillItem[] = [
      {
        id: 'status',
        label: review.status,
        tone: review.status === 'approved' ? 'success' : review.status === 'rejected' ? 'danger' : 'warning',
      },
    ];
    if (review.model) {
      items.push({ id: 'model', label: review.model, mono: true });
    }
    if (review.provider) {
      items.push({ id: 'provider', label: review.provider, mono: true });
    }
    return items;
  }, [review]);
  const fixLogPreview = useMemo(() => {
    return compactReviewFixLogs(reviewFixLogs).slice(-8);
  }, [reviewFixLogs]);
  const formattedFixLogs = useMemo(() => compactReviewFixLogs(reviewFixLogs), [reviewFixLogs]);

  // Reset selection state when a new review is generated so stale
  // checkboxes from a previous review don't carry over.
  useEffect(() => {
    setCheckedItems(new Set());
    setFixPrompt('');
  }, [review]);

  const handleToggleItem = useCallback((key: string) => {
    setCheckedItems((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }, []);

  const handleSelectAll = useCallback(() => {
    const allKeys: string[] = [];
    parsedDetailedGuidance.sections.forEach((section) => {
      section.entries.forEach((_, index) => {
        allKeys.push(`${section.id}:${index}`);
      });
    });
    if (allKeys.length === 0) return;
    const allChecked = allKeys.every((key) => checkedItems.has(key));
    if (allChecked) {
      setCheckedItems(new Set());
    } else {
      setCheckedItems(new Set(allKeys));
    }
  }, [parsedDetailedGuidance.sections, checkedItems]);

  const totalItems = useMemo(
    () => parsedDetailedGuidance.sections.reduce((acc, s) => acc + s.entries.length, 0),
    [parsedDetailedGuidance.sections]
  );
  const selectedCount = checkedItems.size;

  const collectSelectedItems = useCallback((): string[] => {
    const items: string[] = [];
    parsedDetailedGuidance.sections.forEach((section) => {
      section.entries.forEach((entry, index) => {
        const key = `${section.id}:${index}`;
        if (checkedItems.has(key)) {
          const parts = [`**[${section.title}]** ${entry.issue}`];
          if (entry.file) parts.push(`  File: ${entry.file}`);
          if (entry.evidence) parts.push(`  Evidence: ${entry.evidence}`);
          if (entry.suggestion) parts.push(`  Next step: ${entry.suggestion}`);
          items.push(parts.join('\n'));
        }
      });
    });
    return items;
  }, [parsedDetailedGuidance.sections, checkedItems]);

  const handleFixClick = useCallback(() => {
    const opts: { fixPrompt?: string; selectedItems?: string[] } = {};
    const trimmedPrompt = fixPrompt.trim();
    if (trimmedPrompt) opts.fixPrompt = trimmedPrompt;
    const items = collectSelectedItems();
    if (items.length > 0) opts.selectedItems = items;
    onFixFromReview(opts);
  }, [fixPrompt, collectSelectedItems, onFixFromReview]);

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
                <InlinePillRow ariaLabel="Review metadata" items={reviewMetaItems} className="review-pill-row" />
                <MessageSegments content={review.feedback || review.review_output} />
              </MessageBubble>

              {review.warnings && review.warnings.length > 0 ? (
                <MessageBubble
                  type="assistant"
                  ariaLabel="Review warnings"
                  copyText={review.warnings.join('\n')}
                >
                  <InlinePillRow
                    ariaLabel="Review warning metadata"
                    items={[{ id: 'warnings', label: 'Warnings', tone: 'warning' }]}
                    className="review-pill-row review-warning-strip"
                  />
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
                                <div className="review-guidance-card-top">
                                  <label className="review-guidance-checkbox-label">
                                    <input
                                      type="checkbox"
                                      checked={checkedItems.has(`${section.id}:${index}`)}
                                      onChange={() => handleToggleItem(`${section.id}:${index}`)}
                                      disabled={isReviewFixing}
                                    />
                                  </label>
                                  <div className="review-guidance-card-body">
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
                                        <div className="review-guidance-copy">
                                          <MessageContent content={entry.evidence} />
                                        </div>
                                      </div>
                                    ) : null}
                                    {entry.suggestion ? (
                                      <div className="review-guidance-row">
                                        <span className="review-guidance-label">Next Step</span>
                                        <div className="review-guidance-copy">
                                          <MessageContent content={entry.suggestion} />
                                        </div>
                                      </div>
                                    ) : null}
                                    {Object.entries(entry)
                                      .filter(([key, value]) => !['issue', 'file', 'evidence', 'suggestion'].includes(key) && typeof value === 'string' && value.trim())
                                      .map(([key, value]) => (
                                        <div key={key} className="review-guidance-row">
                                          <span className="review-guidance-label">
                                            {key.replace(/_/g, ' ').replace(/\b\w/g, (char) => char.toUpperCase())}
                                          </span>
                                          <div className="review-guidance-copy">
                                            <MessageContent content={String(value)} />
                                          </div>
                                        </div>
                                      ))}
                                  </div>
                                </div>
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
                  <InlinePillRow
                    ariaLabel="Fix workflow metadata"
                    items={[
                      { id: 'fix-session', label: 'Fix session' },
                      ...(reviewFixSessionID ? [{ id: 'fix-session-id', label: reviewFixSessionID, mono: true as const }] : []),
                    ]}
                    className="review-pill-row"
                  />
                  <details className="reasoning-block review-disclosure">
                    <summary className="reasoning-summary review-details-summary">
                      <span>Fix workflow logs</span>
                      <span>{reviewFixLogs.length} entries</span>
                    </summary>
                    <div className="reasoning-content review-details-content">
                      <div className="review-fix-log-list">
                        {formattedFixLogs.map((log, index) => (
                          <div key={`${index}-${log.slice(0, 24)}`} className="review-fix-log-item">
                            {log}
                          </div>
                        ))}
                      </div>
                    </div>
                  </details>
                  <div className="review-fix-preview">
                    <div className="review-fix-log-list review-fix-log-preview">
                      {fixLogPreview.map((log, index) => (
                        <div key={`${index}-${log.slice(0, 24)}`} className="review-fix-log-item">
                          {log}
                        </div>
                      ))}
                    </div>
                  </div>
                </MessageBubble>
              ) : null}

              {reviewFixResult ? (
                <MessageBubble
                  type="assistant"
                  ariaLabel="Review fix result"
                  copyText={reviewFixResult}
                >
                  <details className="reasoning-block review-disclosure" open>
                    <summary className="reasoning-summary review-details-summary">
                      <span>Fix result</span>
                    </summary>
                    <div className="reasoning-content review-details-content">
                      <MessageSegments content={reviewFixResult} />
                    </div>
                  </details>
                </MessageBubble>
              ) : null}
            </>
          ) : null}
        </div>
      </div>

      {review && (
        <div className="review-fix-prompt-section">
          <details className="review-fix-prompt-details">
            <summary className="review-fix-prompt-summary">
              <span>Add fix instructions (optional)</span>
            </summary>
            <textarea
              className="review-fix-prompt-textarea"
              placeholder="E.g. Focus only on the error handling issues, skip the naming suggestions..."
              value={fixPrompt}
              onChange={(e) => setFixPrompt(e.target.value)}
              disabled={isReviewFixing}
              rows={3}
            />
          </details>
        </div>
      )}
      <div className="input-container review-actions-bar">
        {totalItems > 0 && (
          <button
            className="review-fix-btn review-fix-select-all-btn"
            onClick={handleSelectAll}
            disabled={isReviewFixing || isReviewLoading}
            title={selectedCount === totalItems ? 'Deselect all items' : 'Select all items'}
          >
            {selectedCount === totalItems ? 'Deselect All' : 'Select All'}
          </button>
        )}
        {selectedCount > 0 && (
          <span className="review-fix-selection-count">
            {selectedCount} of {totalItems} selected
          </span>
        )}
        <button
          className="review-fix-btn"
          onClick={handleFixClick}
          disabled={!review?.review_output || isReviewFixing || isReviewLoading}
        >
          <Wrench size={14} />
          {isReviewFixing ? 'Applying fixes…' : (selectedCount > 0 ? `Fix ${selectedCount} Item${selectedCount > 1 ? 's' : ''}` : 'Fix From Review')}
        </button>
      </div>
    </div>
  );
};

const HTMLISH_LINE_PATTERN = /^(<!DOCTYPE|<\/?(html|head|body|div|span|title|meta|link|script|header|section|p|ul|li)\b|<!--|\*\/|\/\*|<\w+)/i;

const compactReviewFixLogs = (logs: string[]): string[] => {
  const compacted: string[] = [];
  let pendingHtmlCount = 0;

  const flushHtml = () => {
    if (pendingHtmlCount > 0) {
      compacted.push(`HTML error payload suppressed (${pendingHtmlCount} lines)`);
      pendingHtmlCount = 0;
    }
  };

  logs.forEach((raw) => {
    const cleaned = stripAnsiCodes(String(raw || '')).replace(/\s+/g, ' ').trim();
    if (!cleaned) {
      return;
    }

    if (HTMLISH_LINE_PATTERN.test(cleaned)) {
      pendingHtmlCount += 1;
      return;
    }

    flushHtml();
    compacted.push(cleaned);
  });

  flushHtml();
  return compacted;
};

export default ReviewWorkspaceTab;
