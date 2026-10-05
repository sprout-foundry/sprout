import { Collapsible } from '@sprout/ui';
import { useState } from 'react';
import type { SproutSettings } from '../../services/api';

// Built-in role names (SP-150 §150a). Mirrors configuration.BuiltInRoles() in
// pkg/configuration/config_roles.go. The set is fixed for SP-150 — keep this
// list in sync with the Go side if roles are added there.
const BUILTIN_ROLES = ['planner', 'coder', 'summarizer', 'reviewer', 'commit'] as const;

type RoleName = (typeof BUILTIN_ROLES)[number];
type RoleSelection = { provider?: string; model?: string };
type RoleDraft = Record<RoleName, { provider: string; model: string }>;

interface RoleModelsSectionProps {
  settings: SproutSettings;
  updateSetting?: (keyOrPath: string, value: unknown) => Promise<void>;
}

// Seed the local draft from the persisted roles map, defaulting every
// built-in role to empty fields (absent = fall back to the conversation's
// provider/model).
function draftFromRoles(roles: Record<string, RoleSelection> | undefined): RoleDraft {
  const draft = {} as RoleDraft;
  for (const role of BUILTIN_ROLES) {
    const entry = roles?.[role];
    draft[role] = { provider: entry?.provider ?? '', model: entry?.model ?? '' };
  }
  return draft;
}

/**
 * Role models section (SP-150 §150d). Pin a specific provider or model to a
 * built-in role (planner, coder, summarizer, reviewer, commit). Collapsed by
 * default so it stays out of the way of the primary provider settings.
 *
 * Typing updates local draft state only; each row's Save button commits the
 * whole roles map via updateSetting('roles', next), dropping rows whose
 * provider+model are both empty so an all-empty result stores nothing (the
 * backend stores nothing for an empty map too).
 */
export default function RoleModelsSection({ settings, updateSetting }: RoleModelsSectionProps) {
  const [draft, setDraft] = useState<RoleDraft>(() => draftFromRoles(settings.roles));

  // Hide the section entirely if there's no way to persist (no updateSetting),
  // mirroring ProviderPrioritySection.
  if (!updateSetting) return null;

  const setField = (role: RoleName, field: 'provider' | 'model', value: string) => {
    setDraft((prev) => ({ ...prev, [role]: { ...prev[role], [field]: value } }));
  };

  // Build the full roles map from the current draft, dropping roles whose
  // provider+model are both empty so an all-empty result stores nothing.
  const buildRolesMap = (): Record<string, RoleSelection> => {
    const next: Record<string, RoleSelection> = {};
    for (const role of BUILTIN_ROLES) {
      const p = draft[role].provider.trim();
      const m = draft[role].model.trim();
      if (p === '' && m === '') continue;
      const entry: RoleSelection = {};
      if (p !== '') entry.provider = p;
      if (m !== '') entry.model = m;
      next[role] = entry;
    }
    return next;
  };

  const commit = () => {
    void updateSetting('roles', buildRolesMap());
  };

  return (
    <Collapsible title="Role Models" className="settings-block-spaced" data-testid="role-models-section">
      <div className="config-help settings-help-spaced">
        Pin a provider or model to a built-in role. Leave a field empty to fall back to the conversation&#39;s
        provider/model.
      </div>
      {BUILTIN_ROLES.map((role) => (
        <div key={role} className="config-item" data-testid={`role-model-row-${role}`}>
          <label>{role}</label>
          <div className="settings-inline-row">
            <input
              type="text"
              className="styled-input"
              placeholder="Model (optional)"
              value={draft[role].model}
              onChange={(e) => setField(role, 'model', e.target.value)}
              data-testid={`role-model-input-${role}`}
            />
            <input
              type="text"
              className="styled-input"
              placeholder="Provider (optional)"
              value={draft[role].provider}
              onChange={(e) => setField(role, 'provider', e.target.value)}
              data-testid={`role-provider-input-${role}`}
            />
            <button
              type="button"
              className="settings-action-btn"
              onClick={commit}
              data-testid={`role-model-save-${role}`}
            >
              Save
            </button>
          </div>
        </div>
      ))}
    </Collapsible>
  );
}
