import { useCallback, useEffect, useRef, useState } from 'react';
import { AlertTriangle, Loader2, Camera, FolderOpen } from 'lucide-react';
import type { SproutSettings } from '../../services/api';
import { clientFetch } from '../../services/clientSession';
import { getNestedValue } from './settingsHelpers';
import type { FieldRenderers } from './useSettingsFieldRenderers';

interface ComputerUseSettingsTabProps {
  settings: SproutSettings | null;
  renderToggle: FieldRenderers['renderToggle'];
  renderNumberInput: FieldRenderers['renderNumberInput'];
  renderLocalToggle: FieldRenderers['renderLocalToggle'];
  updateSetting: (keyOrPath: string, value: unknown) => Promise<void>;
}

/** Response shape for POST /api/computer-use/test (SP-063 Phase 7). */
interface ComputerUseTestResponse {
  status: 'ok' | 'disabled' | 'error';
  message: string;
}

type TestPhase = 'idle' | 'testing' | 'ok' | 'disabled' | 'error';

/** Default config values matching Go's ComputerUseConfig.Resolve(). */
const DEFAULTS = {
  maxActionsPerMinute: 60,
} as const;

export default function ComputerUseSettingsTab({
  settings,
  renderToggle,
  renderNumberInput,
  renderLocalToggle,
  updateSetting,
}: ComputerUseSettingsTabProps) {
  // ── Audit log dir (read-only display) ──────────────────────
  const auditLogDir = String(getNestedValue(settings ?? {}, 'computer_use.audit_log_dir') ?? '');

  // ── Workspace allowlist (textarea, newline-separated) ──────
  const persistedAllowlist: string[] = settings
    ? ((getNestedValue(settings, 'computer_use.workspace_allowlist') as string[] | undefined) ?? [])
    : [];
  const [allowlistDraft, setAllowlistDraft] = useState<string>(persistedAllowlist.join('\n'));
  const allowlistTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastPersistedAllowlist = useRef<string>(persistedAllowlist.join('\n'));

  // Sync local draft when server settings refresh (e.g. scope switch).
  useEffect(() => {
    const joined = persistedAllowlist.join('\n');
    if (joined !== lastPersistedAllowlist.current) {
      lastPersistedAllowlist.current = joined;
      setAllowlistDraft(joined);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [persistedAllowlist.join('\n')]);

  const commitAllowlist = useCallback(
    (raw: string) => {
      const list = raw
        .split('\n')
        .map((s) => s.trim())
        .filter((s) => s.length > 0);
      lastPersistedAllowlist.current = list.join('\n');
      void updateSetting('computer_use.workspace_allowlist', list);
    },
    [updateSetting],
  );

  // ── Test connection ────────────────────────────────────────
  const [testPhase, setTestPhase] = useState<TestPhase>('idle');
  const [testMessage, setTestMessage] = useState<string>('');

  const handleTestConnection = useCallback(async () => {
    setTestPhase('testing');
    setTestMessage('');
    try {
      const res = await clientFetch('/api/computer-use/test', { method: 'POST' });
      if (!res.ok) {
        setTestPhase('error');
        setTestMessage(`HTTP ${res.status}: ${await res.text()}`);
        return;
      }
      const data = (await res.json()) as ComputerUseTestResponse;
      switch (data.status) {
        case 'ok':
          setTestPhase('ok');
          setTestMessage(data.message || 'Backend ready');
          break;
        case 'disabled':
          setTestPhase('disabled');
          setTestMessage(data.message || 'Computer use is not enabled');
          break;
        default:
          setTestPhase('error');
          setTestMessage(data.message || 'Unknown error');
      }
    } catch (err) {
      setTestPhase('error');
      setTestMessage(err instanceof Error ? err.message : 'Request failed');
    }
  }, []);

  // ── Local toggle: enable/disable ───────────────────────────
  const computerUseEnabled = Boolean(getNestedValue(settings ?? {}, 'computer_use.enabled'));

  const handleToggleEnabled = useCallback(
    (next: boolean) => {
      void updateSetting('computer_use.enabled', next);
    },
    [updateSetting],
  );

  // ── Toggle fallback for renderLocalToggle (matches Notifications tab) ─
  const toggle =
    renderLocalToggle ??
    ((checked: boolean, label: string, onChange: (next: boolean) => void) => (
      <label className="styled-toggle">
        <input type="checkbox" checked={checked} onChange={() => onChange(!checked)} />
        <span className="toggle-track" />
        <span className="toggle-label">{label}</span>
      </label>
    ));

  return (
    <div className="section">
      <h4>Computer Use</h4>

      {/* ── Warning banner ─────────────────────────────────── */}
      <div
        className="config-item"
        style={{
          background: 'var(--bg-warning, color-mix(in srgb, var(--accent-warning) 12%, transparent))',
          border: `1px solid color-mix(in srgb, var(--accent-warning) 40%, transparent)`,
          borderRadius: '6px',
          padding: '10px 12px',
          marginBottom: '12px',
          display: 'flex',
          gap: '8px',
          alignItems: 'flex-start',
        }}
      >
        <AlertTriangle
          size={16}
          style={{
            color: 'var(--accent-warning)',
            flexShrink: 0,
            marginTop: '2px',
          }}
        />
        <span
          style={{
            fontSize: '13px',
            color: 'var(--text-primary, inherit)',
          }}
        >
          <strong style={{ color: 'var(--accent-warning)' }}>Experimental.</strong> Computer Use gives the AI direct
          control of your mouse, keyboard, and screen. Use with caution.
        </span>
      </div>

      {/* ── Master toggle ──────────────────────────────────── */}
      {toggle(
        computerUseEnabled,
        'Enable computer use tools',
        handleToggleEnabled,
        'When enabled, the computer_user persona can drive mouse, keyboard, and screenshot tools. Off by default.',
      )}

      {/* ── Action rate limit ──────────────────────────────── */}
      {renderNumberInput(
        'computer_use.max_actions_per_minute',
        'Max actions per minute',
        0, // min — 0 disables the cap
        600,
        1,
        'Cap on actions per minute as a runaway-loop backstop. Default: 60. Set to 0 to disable (not recommended).',
      )}

      {/* ── Audit log location (read-only) ─────────────────── */}
      <div className="config-item">
        <label>Audit log directory</label>
        <div
          style={{
            display: 'flex',
            gap: '8px',
            alignItems: 'center',
            marginTop: '4px',
          }}
        >
          <input
            type="text"
            className="styled-input"
            readOnly
            value={auditLogDir || '(default: ~/.config/sprout/computer_use_log)'}
            style={{
              flex: 1,
              opacity: 0.7,
              cursor: 'default',
            }}
          />
          <button
            type="button"
            className="btn-secondary"
            disabled={!auditLogDir}
            title={auditLogDir ? 'Open log folder' : 'No custom directory set'}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: '4px',
              whiteSpace: 'nowrap',
            }}
            onClick={() => {
              // Don't implement folder opening — just show the path.
              // The button is a visual affordance for future wiring.
            }}
          >
            <FolderOpen size={14} />
            Open
          </button>
        </div>
        <div className="config-help">
          Where per-session JSONL action logs are written. Defaults to <code>~/.config/sprout/computer_use_log</code>{' '}
          when empty.
        </div>
      </div>

      {/* ── Workspace allowlist ────────────────────────────── */}
      <div className="config-item">
        <label htmlFor="setting-computer-use-allowlist">Workspace allowlist</label>
        <textarea
          id="setting-computer-use-allowlist"
          className="styled-input styled-textarea"
          rows={6}
          value={allowlistDraft}
          placeholder={'/home/user/project-a\n/home/user/project-b'}
          onChange={(e) => {
            const next = e.target.value;
            setAllowlistDraft(next);
            if (allowlistTimer.current) clearTimeout(allowlistTimer.current);
            allowlistTimer.current = setTimeout(() => {
              allowlistTimer.current = null;
              commitAllowlist(next);
            }, 500);
          }}
          onBlur={() => {
            if (allowlistTimer.current) {
              clearTimeout(allowlistTimer.current);
              allowlistTimer.current = null;
            }
            if (allowlistDraft !== lastPersistedAllowlist.current) {
              commitAllowlist(allowlistDraft);
            }
          }}
        />
        <div className="config-help">
          One path per line. Workspace roots listed here auto-approve computer use for the session without the
          per-session opt-in prompt. Lines are trimmed; blank lines are ignored.
        </div>
      </div>

      {/* ── Test connection ────────────────────────────────── */}
      <div className="config-item">
        <button
          type="button"
          className="btn-secondary"
          onClick={handleTestConnection}
          disabled={testPhase === 'testing'}
          style={{
            marginTop: '8px',
            display: 'inline-flex',
            alignItems: 'center',
            gap: '6px',
          }}
        >
          {testPhase === 'testing' ? <Loader2 size={14} className="spinning" /> : <Camera size={14} />}
          {testPhase === 'testing'
            ? 'Testing...'
            : testPhase === 'ok'
              ? '✓ Backend ready'
              : testPhase === 'disabled'
                ? '✗ Disabled'
                : testPhase === 'error'
                  ? '✗ Failed'
                  : 'Test connection'}
        </button>
        {testMessage && (
          <div
            className="config-help"
            style={{
              marginTop: '6px',
              color:
                testPhase === 'ok'
                  ? 'var(--accent-success)'
                  : testPhase === 'error'
                    ? 'var(--accent-error)'
                    : testPhase === 'disabled'
                      ? 'var(--accent-warning)'
                      : undefined,
            }}
          >
            {testMessage}
          </div>
        )}
      </div>
    </div>
  );
}
