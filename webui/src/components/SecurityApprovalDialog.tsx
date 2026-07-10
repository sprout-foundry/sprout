import { useEffect, useCallback, useRef } from 'react';
import { Check, TriangleAlert, X } from 'lucide-react';
import type { SecurityApprovalAction } from '../hooks/useSecurityApproval';
import './ThemedDialog.css';

export interface SecurityApprovalDialogProps {
  requestId: string;
  toolName: string;
  riskLevel: 'SAFE' | 'CAUTION' | 'DANGEROUS';
  reasoning: string;
  command?: string;
  riskType?: string;
  target?: string;
  // SP-058: when true, render the 4-option dialog (Approve / Deny / Always
  // Approve / Elevate). Default false renders the classic Allow / Block.
  // Only shell_command opts in today; other tools stay on the legacy UI.
  allowOptions?: boolean;
  // Filesystem dialog mode. When set, overrides allowOptions and renders
  // the path-tier-aware layout:
  //   - fs_external: Allow once / Allow folder this session / Deny
  //   - fs_sensitive: Allow once / Deny (with a note that the path can
  //     not be added to the session allowlist).
  fsKind?: 'fs_external' | 'fs_sensitive';
  fsFolder?: string;
  fsPath?: string;
  onRespond: (requestId: string, approved: boolean, action?: SecurityApprovalAction) => void;
}

type RiskKey = 'safe' | 'caution' | 'dangerous';

const RISK_ICON: Record<RiskKey, JSX.Element> = {
  safe: <Check size={16} />,
  caution: <TriangleAlert size={16} />,
  dangerous: <X size={16} />,
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
  allowOptions,
  fsKind,
  fsFolder,
  fsPath,
  onRespond,
}: SecurityApprovalDialogProps): JSX.Element {
  const risk = toRiskKey(riskLevel);
  const blockBtnRef = useRef<HTMLButtonElement>(null);
  const allowBtnRef = useRef<HTMLButtonElement>(null);
  // SP-058: in 4-option mode the "Approve once" button is the safe
  // default-focus target. Auto-focusing Elevate (which silently bumps
  // the session to permissive) would let a fast keyboard user widen
  // their gates by hitting Space — exactly the wrong default.
  const approveOnceBtnRef = useRef<HTMLButtonElement>(null);
  const isFilesystem = fsKind === 'fs_external' || fsKind === 'fs_sensitive';

  const sendOnce = useCallback(() => {
    onRespond(requestId, true, allowOptions || isFilesystem ? 'approve_once' : undefined);
  }, [requestId, onRespond, allowOptions, isFilesystem]);

  const sendDeny = useCallback(() => {
    onRespond(requestId, false, allowOptions || isFilesystem ? 'deny' : undefined);
  }, [requestId, onRespond, allowOptions, isFilesystem]);

  const handleAllow = sendOnce;
  const handleBlock = sendDeny;

  const handleAlways = useCallback(() => {
    onRespond(requestId, true, 'approve_always');
  }, [requestId, onRespond]);

  const handleElevate = useCallback(() => {
    onRespond(requestId, true, 'elevate');
  }, [requestId, onRespond]);

  const handleAllowFolder = useCallback(() => {
    onRespond(requestId, true, 'allow_folder_session');
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
      } else if (allowOptions || isFilesystem) {
        // Multi-option dialog: focus the "Allow once" / "Approve once"
        // button so a stray Space keystroke doesn't widen scope.
        approveOnceBtnRef.current?.focus();
      } else {
        allowBtnRef.current?.focus();
      }
    }, 60);
    return () => clearTimeout(timer);
  }, [risk, allowOptions, isFilesystem]);

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
          {target && !command && !isFilesystem && (
            <div className="security-approval-target-wrapper">
              <div className="security-approval-target-label">Target</div>
              <div className="security-approval-target-box">{target}</div>
            </div>
          )}

          {/* Filesystem mode: show path + folder-to-allowlist */}
          {isFilesystem && fsPath && (
            <div className="security-approval-target-wrapper">
              <div className="security-approval-target-label">Path</div>
              <div className="security-approval-target-box">{fsPath}</div>
            </div>
          )}
          {fsKind === 'fs_external' && fsFolder && (
            <div className="security-approval-target-wrapper">
              <div className="security-approval-target-label">
                Folder to allowlist if you pick &ldquo;Allow folder this session&rdquo;
              </div>
              <div className="security-approval-target-box">{fsFolder}</div>
            </div>
          )}
        </div>

        {/* SP-058: disclaimer for the Elevate action, shown only in 4-option mode */}
        {allowOptions && !isFilesystem && (
          <div className="security-approval-elevate-note" role="note">
            <strong>Elevate</strong> bumps this session to the <code>permissive</code> risk profile — you won&apos;t see
            high-risk prompts again until restart. Critical operations (rm&nbsp;-rf&nbsp;/, fork bombs) still block. Run{' '}
            <code>/risk-profile&nbsp;permissive</code> to make this persistent.
          </div>
        )}
        {/* Filesystem sensitive-tier note: explain why "Allow folder this session" is missing */}
        {fsKind === 'fs_sensitive' && (
          <div className="security-approval-elevate-note" role="note">
            This is a <strong>sensitive</strong> path (system directory, or a home-directory path while your working
            directory is outside <code>$HOME</code>). It can&apos;t be added to the session allowlist — every access
            will prompt.
          </div>
        )}

        {/* Footer - Cannot be dismissed, must choose */}
        <div
          className={`security-approval-footer${
            allowOptions || fsKind === 'fs_external' ? ' security-approval-footer--4opt' : ''
          }`}
        >
          {fsKind === 'fs_external' ? (
            <>
              <button
                ref={blockBtnRef}
                type="button"
                className="security-approval-btn security-approval-btn--block"
                onClick={handleBlock}
              >
                Deny
              </button>
              <button
                ref={approveOnceBtnRef}
                type="button"
                className="security-approval-btn security-approval-btn--allow"
                onClick={handleAllow}
              >
                Allow once
              </button>
              <button
                type="button"
                className="security-approval-btn security-approval-btn--allow security-approval-btn--allow--caution"
                onClick={handleAllowFolder}
                title="Auto-approve every file under this folder for the rest of this session"
              >
                Allow folder this session
              </button>
            </>
          ) : fsKind === 'fs_sensitive' ? (
            <>
              <button
                ref={blockBtnRef}
                type="button"
                className="security-approval-btn security-approval-btn--block"
                onClick={handleBlock}
              >
                Deny
              </button>
              <button
                ref={approveOnceBtnRef}
                type="button"
                className="security-approval-btn security-approval-btn--allow"
                onClick={handleAllow}
              >
                Allow once
              </button>
            </>
          ) : allowOptions ? (
            <>
              <button
                ref={blockBtnRef}
                type="button"
                className="security-approval-btn security-approval-btn--block"
                onClick={handleBlock}
              >
                Deny
              </button>
              <button
                ref={approveOnceBtnRef}
                type="button"
                className="security-approval-btn security-approval-btn--allow"
                onClick={handleAllow}
              >
                Approve once
              </button>
              <button
                type="button"
                className="security-approval-btn security-approval-btn--allow"
                onClick={handleAlways}
                title="Persist this exact command to your allowlist so it won't prompt again"
              >
                Always approve
              </button>
              <button
                type="button"
                className="security-approval-btn security-approval-btn--allow security-approval-btn--allow--caution"
                onClick={handleElevate}
                title="Bump this session to 'permissive' — no more high-risk prompts until restart"
              >
                Elevate (session)
              </button>
            </>
          ) : risk === 'dangerous' ? (
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
