import { useState } from 'react';
import { Trash2 } from 'lucide-react';
import type { SproutSettings } from '../../services/api';
import { getNestedValue } from './settingsHelpers';
import type { FieldRenderers } from './useSettingsFieldRenderers';

interface SecuritySettingsTabProps {
  settings: SproutSettings | null;
  renderToggle: FieldRenderers['renderToggle'];
  renderNumberInput: FieldRenderers['renderNumberInput'];
  renderSelect: FieldRenderers['renderSelect'];
  updateSetting: (keyOrPath: string, value: unknown) => Promise<void>;
}

export default function SecuritySettingsTab({
  settings,
  renderToggle,
  renderNumberInput,
  renderSelect,
  updateSetting,
}: SecuritySettingsTabProps) {
  const approved = settings
    ? ((getNestedValue(settings, 'approved_shell_commands') as string[] | undefined) ?? [])
    : [];

  const [draft, setDraft] = useState('');

  const addApproved = async () => {
    const next = draft.trim();
    if (!next) return;
    if (approved.includes(next)) {
      setDraft('');
      return;
    }
    await updateSetting('approved_shell_commands', [...approved, next]);
    setDraft('');
  };

  const removeApproved = async (cmd: string) => {
    await updateSetting('approved_shell_commands', approved.filter((c) => c !== cmd));
  };

  return (
    <div className="section">
      <h4>Security</h4>
      {renderNumberInput('security_validation.threshold', 'Validation threshold (0-2)', 0, 2)}
      {renderSelect('self_review_gate_mode', 'Self-review gate', ['off', 'code', 'always'])}
      <div style={{ marginTop: 'var(--space-5)' }}>
        <h4>Git Permissions</h4>
        {renderToggle('allow_orchestrator_git_write', 'Allow orchestrator git write')}
      </div>
      <div style={{ marginTop: 'var(--space-5)' }}>
        <h4>Shell Command Detection</h4>
        {renderToggle(
          'enable_zsh_command_detection',
          'Enable zsh-aware command detection',
          'Parses terminal output for command-like lines the agent can act on.',
        )}
        {renderToggle(
          'auto_execute_detected_commands',
          'Auto-execute detected commands',
          'When detection is on, run matched commands without an extra confirmation prompt.',
        )}
      </div>
      <div style={{ marginTop: 'var(--space-5)' }}>
        <h4>Approved Shell Commands</h4>
        <div className="config-help" style={{ marginBottom: 'var(--space-3)' }}>
          Persistent allowlist of shell commands that bypass the per-command approval prompt. Each entry is a literal
          command string — patterns must match exactly. Approvals persist across sessions; remove with the trash icon.
        </div>

        <div className="config-item">
          <div style={{ display: 'flex', gap: 'var(--space-2)' }}>
            <input
              type="text"
              className="styled-input"
              placeholder="e.g. git push origin main --force-with-lease"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  void addApproved();
                }
              }}
              style={{ flex: 1 }}
            />
            <button
              type="button"
              className="settings-action-btn"
              onClick={() => void addApproved()}
              disabled={draft.trim().length === 0}
            >
              Add
            </button>
          </div>
        </div>

        {approved.length === 0 ? (
          <div className="settings-empty">No approved commands yet.</div>
        ) : (
          <ul style={{ listStyle: 'none', padding: 0, margin: 'var(--space-3) 0 0 0' }}>
            {approved.map((cmd) => (
              <li
                key={cmd}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 'var(--space-2)',
                  padding: 'var(--space-2) var(--space-3)',
                  background: 'var(--bg-elevated)',
                  border: '1px solid var(--border-subtle)',
                  borderRadius: 'var(--radius-sm)',
                  marginBottom: 'var(--space-2)',
                }}
              >
                <code
                  style={{
                    flex: 1,
                    fontFamily: 'var(--font-mono)',
                    fontSize: 'var(--text-xs)',
                    color: 'var(--text-primary)',
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-all',
                  }}
                >
                  {cmd}
                </code>
                <button
                  type="button"
                  className="settings-icon-btn"
                  aria-label={`Remove approval for ${cmd}`}
                  title="Remove"
                  onClick={() => void removeApproved(cmd)}
                  style={{
                    background: 'transparent',
                    border: 'none',
                    color: 'var(--text-tertiary)',
                    cursor: 'pointer',
                    padding: 'var(--space-1)',
                  }}
                >
                  <Trash2 size={14} />
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      <SecurityPolicyEditor settings={settings} updateSetting={updateSetting} />
    </div>
  );
}

interface SecurityRule {
  pattern: string;
  action: string;
  reason?: string;
}

interface SecurityPolicy {
  default_action?: string;
  max_risk_level?: string;
  allowed_paths?: string[];
  denied_paths?: string[];
  denied_commands?: string[];
  rules?: SecurityRule[];
}

interface SecurityPolicyEditorProps {
  settings: SproutSettings | null;
  updateSetting: (keyOrPath: string, value: unknown) => Promise<void>;
}

function StringListEditor({
  label,
  placeholder,
  values,
  onChange,
}: {
  label: string;
  placeholder: string;
  values: string[];
  onChange: (next: string[]) => void;
}) {
  const [draft, setDraft] = useState('');
  return (
    <div className="config-item">
      <label>{label}</label>
      <div style={{ display: 'flex', gap: 'var(--space-2)', marginBottom: 'var(--space-2)' }}>
        <input
          type="text"
          className="styled-input"
          placeholder={placeholder}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              const next = draft.trim();
              if (next && !values.includes(next)) {
                onChange([...values, next]);
              }
              setDraft('');
            }
          }}
          style={{ flex: 1 }}
        />
        <button
          type="button"
          className="settings-action-btn"
          onClick={() => {
            const next = draft.trim();
            if (next && !values.includes(next)) onChange([...values, next]);
            setDraft('');
          }}
          disabled={draft.trim().length === 0}
        >
          Add
        </button>
      </div>
      {values.length > 0 && (
        <ul style={{ listStyle: 'none', padding: 0, margin: 0 }}>
          {values.map((v) => (
            <li
              key={v}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--space-2)',
                padding: 'var(--space-1) var(--space-2)',
                background: 'var(--bg-elevated)',
                border: '1px solid var(--border-subtle)',
                borderRadius: 'var(--radius-sm)',
                marginBottom: 'var(--space-1)',
              }}
            >
              <code
                style={{
                  flex: 1,
                  fontFamily: 'var(--font-mono)',
                  fontSize: 'var(--text-xs)',
                  color: 'var(--text-primary)',
                }}
              >
                {v}
              </code>
              <button
                type="button"
                aria-label={`Remove ${v}`}
                onClick={() => onChange(values.filter((x) => x !== v))}
                style={{
                  background: 'transparent',
                  border: 'none',
                  color: 'var(--text-tertiary)',
                  cursor: 'pointer',
                  padding: 'var(--space-1)',
                }}
              >
                <Trash2 size={12} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function SecurityPolicyEditor({ settings, updateSetting }: SecurityPolicyEditorProps) {
  const [expanded, setExpanded] = useState(false);
  const policy = (settings as unknown as { security_policy?: SecurityPolicy } | null)?.security_policy ?? {};

  const update = (patch: Partial<SecurityPolicy>) => {
    void updateSetting('security_policy', { ...policy, ...patch });
  };

  const [ruleDraft, setRuleDraft] = useState<SecurityRule>({ pattern: '', action: 'prompt', reason: '' });
  const rules = policy.rules ?? [];
  const addRule = () => {
    if (!ruleDraft.pattern.trim()) return;
    update({
      rules: [...rules, { ...ruleDraft, pattern: ruleDraft.pattern.trim(), reason: ruleDraft.reason?.trim() || undefined }],
    });
    setRuleDraft({ pattern: '', action: 'prompt', reason: '' });
  };
  const removeRule = (idx: number) => {
    update({ rules: rules.filter((_, i) => i !== idx) });
  };

  return (
    <div style={{ marginTop: 'var(--space-5)' }}>
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        style={{
          background: 'transparent',
          border: 'none',
          padding: 0,
          color: 'var(--text-primary)',
          cursor: 'pointer',
          fontSize: 'var(--text-base)',
          fontWeight: 600,
        }}
        aria-expanded={expanded}
      >
        {expanded ? '▼' : '▶'} Workspace Security Policy
      </button>
      {!expanded ? (
        <div className="config-help" style={{ marginTop: 'var(--space-2)' }}>
          Advanced workspace command/path policy. Click to expand.
        </div>
      ) : (
        <div style={{ marginTop: 'var(--space-3)' }}>
          <div className="config-help" style={{ marginBottom: 'var(--space-3)' }}>
            Workspace-level security rules. Persisted to config; the workspace file
            <code> .sprout/security-policy.json </code>still takes precedence when present.
          </div>

          <div className="config-item">
            <label htmlFor="sp-default-action">Default action</label>
            <select
              id="sp-default-action"
              className="styled-select"
              value={policy.default_action ?? ''}
              onChange={(e) => update({ default_action: e.target.value })}
            >
              <option value="">— inherit —</option>
              <option value="allow">allow</option>
              <option value="deny">deny</option>
              <option value="prompt">prompt</option>
            </select>
          </div>

          <div className="config-item">
            <label htmlFor="sp-max-risk">Max risk level</label>
            <select
              id="sp-max-risk"
              className="styled-select"
              value={policy.max_risk_level ?? ''}
              onChange={(e) => update({ max_risk_level: e.target.value })}
            >
              <option value="">— inherit —</option>
              <option value="safe">safe</option>
              <option value="caution">caution</option>
              <option value="dangerous">dangerous</option>
            </select>
          </div>

          <StringListEditor
            label="Allowed paths"
            placeholder="src/, scripts/, etc."
            values={policy.allowed_paths ?? []}
            onChange={(next) => update({ allowed_paths: next })}
          />
          <StringListEditor
            label="Denied paths"
            placeholder="secrets/, .env"
            values={policy.denied_paths ?? []}
            onChange={(next) => update({ denied_paths: next })}
          />
          <StringListEditor
            label="Denied commands"
            placeholder="rm -rf, sudo rm"
            values={policy.denied_commands ?? []}
            onChange={(next) => update({ denied_commands: next })}
          />

          <div className="config-item" style={{ marginTop: 'var(--space-4)' }}>
            <label>Pattern rules</label>
            <div className="config-help" style={{ marginBottom: 'var(--space-2)' }}>
              Each rule pairs a glob/regex pattern with an action. First match wins.
            </div>

            {rules.length > 0 && (
              <ul style={{ listStyle: 'none', padding: 0, margin: '0 0 var(--space-2) 0' }}>
                {rules.map((r, idx) => (
                  <li
                    key={`${r.pattern}-${idx}`}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 'var(--space-2)',
                      padding: 'var(--space-2) var(--space-3)',
                      background: 'var(--bg-elevated)',
                      border: '1px solid var(--border-subtle)',
                      borderRadius: 'var(--radius-sm)',
                      marginBottom: 'var(--space-1)',
                    }}
                  >
                    <code style={{ flex: 1, fontFamily: 'var(--font-mono)', fontSize: 'var(--text-xs)' }}>
                      {r.pattern}
                    </code>
                    <span
                      style={{
                        fontSize: 'var(--text-xs)',
                        padding: '1px 6px',
                        borderRadius: 'var(--radius-sm)',
                        background: 'var(--bg-surface)',
                        color: 'var(--text-secondary)',
                      }}
                    >
                      {r.action}
                    </span>
                    {r.reason && (
                      <span style={{ fontSize: 'var(--text-xs)', color: 'var(--text-tertiary)' }}>{r.reason}</span>
                    )}
                    <button
                      type="button"
                      aria-label={`Remove rule ${r.pattern}`}
                      onClick={() => removeRule(idx)}
                      style={{
                        background: 'transparent',
                        border: 'none',
                        color: 'var(--text-tertiary)',
                        cursor: 'pointer',
                        padding: 'var(--space-1)',
                      }}
                    >
                      <Trash2 size={12} />
                    </button>
                  </li>
                ))}
              </ul>
            )}

            <div
              style={{
                display: 'flex',
                gap: 'var(--space-2)',
                alignItems: 'center',
                padding: 'var(--space-2)',
                background: 'var(--bg-surface)',
                border: '1px solid var(--border-subtle)',
                borderRadius: 'var(--radius-sm)',
              }}
            >
              <input
                type="text"
                className="styled-input"
                placeholder="pattern"
                value={ruleDraft.pattern}
                onChange={(e) => setRuleDraft({ ...ruleDraft, pattern: e.target.value })}
                style={{ flex: 2 }}
              />
              <select
                className="styled-select"
                value={ruleDraft.action}
                onChange={(e) => setRuleDraft({ ...ruleDraft, action: e.target.value })}
                style={{ flex: 1 }}
              >
                <option value="allow">allow</option>
                <option value="deny">deny</option>
                <option value="prompt">prompt</option>
              </select>
              <input
                type="text"
                className="styled-input"
                placeholder="reason (optional)"
                value={ruleDraft.reason ?? ''}
                onChange={(e) => setRuleDraft({ ...ruleDraft, reason: e.target.value })}
                style={{ flex: 2 }}
              />
              <button
                type="button"
                className="settings-action-btn"
                onClick={addRule}
                disabled={!ruleDraft.pattern.trim()}
              >
                Add rule
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
