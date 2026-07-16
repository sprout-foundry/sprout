import { Trash2 } from 'lucide-react';
import { useState } from 'react';
import type { SproutSettings } from '../../services/api';
import { getNestedValue } from './settingsHelpers';

interface CommandPolicyEditorProps {
  settings: SproutSettings | null;
  updateSetting: (keyOrPath: string, value: unknown) => Promise<void>;
}

interface CommandRule {
  pattern: string;
  action: 'allow' | 'ask' | 'deny';
  reason?: string;
}

interface PolicySection {
  action: 'allow' | 'ask' | 'deny';
  label: string;
  placeholder: string;
  accent: string;
}

const SECTIONS: PolicySection[] = [
  { action: 'allow', label: 'Always Allow', placeholder: 'e.g. npm test', accent: 'green' },
  { action: 'ask', label: 'Always Ask', placeholder: 'e.g. git push*', accent: 'amber' },
  { action: 'deny', label: 'Never Allow', placeholder: 'e.g. kubectl delete*', accent: 'red' },
];

function PolicyList({
  section,
  rules,
  onUpdate,
}: {
  section: PolicySection;
  rules: CommandRule[];
  onUpdate: (rules: CommandRule[]) => void;
}) {
  const [draft, setDraft] = useState('');
  const filtered = rules.filter((r) => r.action === section.action);

  const addRule = () => {
    const pattern = draft.trim();
    if (!pattern) return;
    onUpdate([...rules, { pattern, action: section.action }]);
    setDraft('');
  };

  const removeRule = (pattern: string) => {
    onUpdate(rules.filter((r) => r.pattern !== pattern || r.action !== section.action));
  };

  return (
    <div>
      <h5 className={`policy-section-header accent-${section.accent}`}>{section.label}</h5>
      <div className="settings-inline-row">
        <input
          type="text"
          className="styled-input"
          placeholder={section.placeholder}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              addRule();
            }
          }}
        />
        <button
          type="button"
          className="settings-action-btn"
          onClick={addRule}
          disabled={draft.trim().length === 0}
        >
          Add
        </button>
      </div>
      {filtered.length === 0 ? (
        <div className="settings-empty">No rules yet.</div>
      ) : (
        <ul className="settings-list">
          {filtered.map((r) => (
            <li key={r.pattern} className="settings-list-row">
              <code className="settings-list-row-code">{r.pattern}</code>
              {r.reason && <span className="settings-rule-reason">{r.reason}</span>}
              <button
                type="button"
                className="settings-icon-btn danger"
                aria-label={`Remove ${r.pattern}`}
                title="Remove"
                onClick={() => removeRule(r.pattern)}
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

export default function CommandPolicyEditor({ settings, updateSetting }: CommandPolicyEditorProps) {
  const rawRules = settings ? getNestedValue(settings, 'command_policies.rules') : [];
  const rules: CommandRule[] = Array.isArray(rawRules) ? rawRules : [];

  const updateRules = async (next: CommandRule[]) => {
    await updateSetting('command_policies', { rules: next });
  };

  return (
    <div className="settings-section-spaced">
      <h4>Command Policies</h4>
      <div className="config-help settings-help-spaced">
        Rules are checked first-match-wins. Patterns are glob (e.g. <code>git push*</code>). Critical operations like{' '}
        <code>rm -rf /</code> are never overridable.
      </div>
      <div className="policy-grid">
        {SECTIONS.map((section) => (
          <PolicyList key={section.action} section={section} rules={rules} onUpdate={updateRules} />
        ))}
      </div>
    </div>
  );
}
