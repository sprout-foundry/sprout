import { useEffect, useCallback, useRef } from 'react';
import './ThemedDialog.css';

export interface SecurityApprovalDialogProps {
  requestId: string;
  toolName: string;
  riskLevel: 'SAFE' | 'CAUTION' | 'DANGEROUS';
  reasoning: string;
  command?: string;
  riskType?: string;
  target?: string;
  onRespond: (requestId: string, approved: boolean) => void;
}

type RiskKey = 'safe' | 'caution' | 'dangerous';

const RISK_ICON: Record<RiskKey, string> = {
  safe: '✓',
  caution: '⚠',
  dangerous: '✕',
};

const RISK_LABEL: Record<RiskKey, string> = {
  safe: 'Safe',
  caution: 'Caution',
  dangerous: 'Dangerous',
};

const toRiskKey = (level: string): RiskKey => {
  const normalized = (level || '').toUpperCase();
  if (normalized === 'SAFE') return 'safe';
  if (normalized === 'CAUTION') return 'caution';
  return 'dangerous';
};

function SecurityApprovalDialog({
  requestId,
  toolName,
  riskLevel,
  reasoning,
  command,
  riskType,
  target,
  onRespond,
}: SecurityApprovalDialogProps): JSX.Element {
  const risk = toRiskKey(riskLevel);
  const blockBtnRef = useRef<HTMLButtonElement>(null);
  const allowBtnRef = useRef<HTMLButtonElement>(null);

  const handleAllow = useCallback(() => {
    onRespond(requestId, true);
  }, [requestId, onRespond]);

  const handleBlock = useCallback(() => {
    onRespond(requestId, false);
  }, [requestId, onRespond]);

  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      // Cannot dismiss via Escape — user MUST choose
      e.preventDefault();
      return;
    }
    if (e.key === 'Enter') {
      e.preventDefault();
    }
  }, []);

  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown);
    // Lock scroll while dialog is open
    document.body.style.overflow = 'hidden';
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      document.body.style.overflow = '';
    };
  }, [handleKeyDown]);

  // Auto-focus the safest default button
  useEffect(() => {
    const timer = setTimeout(() => {
      if (risk === 'dangerous') {
        blockBtnRef.current?.focus();
      } else {
        allowBtnRef.current?.focus();
      }
    }, 60);
    return () => clearTimeout(timer);
  }, [risk]);

  return (
    <div className="security-approval-overlay" role="dialog" aria-modal="true" aria-label="Security approval required">
      <div className="security-approval-card" onClick={(e) => e.stopPropagation()}>
        {/* Accent bar */}
        <div className={`security-approval-accent-bar security-approval-accent-bar--${risk}`} />

        {/* Header */}
        <div className="security-approval-header">
          <span className={`security-approval-shield security-approval-shield--${risk}`}>{RISK_ICON[risk]}</span>
          <div className="security-approval-header-row">
            <h2 className="security-approval-title">Security Approval Required</h2>
            <span className={`security-approval-risk-badge security-approval-risk-badge--${risk}`}>
              {RISK_LABEL[risk]}
            </span>
          </div>
        </div>

        {/* Body */}
        <div className="security-approval-body">
          {/* Tool name */}
          <div>
            <span className="security-approval-tool-name-label">Tool</span>
            <span className="security-approval-tool-name">{toolName}</span>
          </div>

          {/* Reasoning */}
          {reasoning && <div className="security-approval-reasoning">{reasoning}</div>}

          {/* Risk type category */}
          {riskType && <div className="security-approval-risk-type">{riskType}</div>}

          {/* Command (for shell_command) */}
          {command && (
            <div className="security-approval-command-wrapper">
              <div className="security-approval-command-label">Command</div>
              <div
                className={`security-approval-command-box${
                  risk === 'dangerous' ? ' security-approval-command-box--dangerous' : ''
                }`}
              >
                {command}
              </div>
            </div>
          )}

          {/* Target (for file write and git operations) */}
          {target && !command && (
            <div className="security-approval-target-wrapper">
              <div className="security-approval-target-label">Target</div>
              <div className="security-approval-target-box">{target}</div>
            </div>
          )}
        </div>

        {/* Footer - Cannot be dismissed, must choose */}
        <div className="security-approval-footer">
          {risk === 'dangerous' ? (
            <>
              <button
                ref={allowBtnRef}
                type="button"
                className="security-approval-btn security-approval-btn--allow security-approval-btn--allow--dangerous"
                onClick={handleAllow}
              >
                Allow
              </button>
              <button
                ref={blockBtnRef}
                type="button"
                className="security-approval-btn security-approval-btn--block security-approval-btn--block--dangerous"
                onClick={handleBlock}
              >
                Block
              </button>
            </>
          ) : (
            <>
              <button
                ref={blockBtnRef}
                type="button"
                className="security-approval-btn security-approval-btn--block"
                onClick={handleBlock}
              >
                Block
              </button>
              {risk === 'caution' ? (
                <button
                  ref={allowBtnRef}
                  type="button"
                  className="security-approval-btn security-approval-btn--allow security-approval-btn--allow--caution"
                  onClick={handleAllow}
                >
                  Allow
                </button>
              ) : (
                <button
                  ref={allowBtnRef}
                  type="button"
                  className="security-approval-btn security-approval-btn--allow"
                  onClick={handleAllow}
                >
                  Allow
                </button>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
}

export default SecurityApprovalDialog;
