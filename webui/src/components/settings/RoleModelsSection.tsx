import { Collapsible } from '@sprout/ui';
import { useEffect, useRef, useState } from 'react';
import type { SproutSettings } from '../../services/api';

// Built-in role names (SP-150 §150a). Mirrors configuration.BuiltInRoles() in
// pkg/configuration/config_roles.go. The set is fixed for SP-150 — keep this
// list in sync with the Go side if roles are added there.
const BUILTIN_ROLES = ['planner', 'coder', 'summarizer', 'reviewer', 'commit'] as const;

const BUILTIN_ROLE_SET: ReadonlySet<string> = new Set(BUILTIN_ROLES);

type RoleName = (typeof BUILTIN_ROLES)[number];
type RoleSelection = { provider?: string; model?: string };
type RolesMap = Record<string, RoleSelection>;
type RoleDraft = Record<RoleName, { provider: string; model: string }>;

interface RoleModelsSectionProps {
  settings: SproutSettings;
  updateSetting?: (keyOrPath: string, value: unknown) => Promise<void>;
}

// Seed the local draft from the persisted roles map, defaulting every
// built-in role to empty fields (absent = fall back to the conversation's
// provider/model).
function draftFromRoles(roles: RolesMap | undefined): RoleDraft {
  const draft = {} as RoleDraft;
  for (const role of BUILTIN_ROLES) {
    const entry = roles?.[role];
    draft[role] = { provider: entry?.provider ?? '', model: entry?.model ?? '' };
  }
  return draft;
}

// Stable string form of a roles map, used to detect *value* changes in
// settings.roles. The settings object is re-created on many refreshes with
// unchanged content, so reference comparison would re-sync (and clobber
// in-flight typing) on every such refresh; comparing the canonical form only
// fires the re-sync when a role entry actually changed. Entries are flattened
// to sorted [key, value] pairs (outer role names and inner field names both
// sorted) so the comparison is insensitive to property insertion order.
function rolesKey(roles: RolesMap | undefined): string {
  const pairs = Object.entries(roles ?? {}).map(([role, entry]) => [
    role,
    Object.entries(entry ?? {}).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)),
  ]);
  pairs.sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
  return JSON.stringify(pairs);
}

/**
 * Role models section (SP-150 §150d). Pin a specific provider or model to a
 * built-in role (planner, coder, summarizer, reviewer, commit). Collapsed by
 * default so it stays out of the way of the primary provider settings.
 *
 * Typing updates local draft state only; each row's Save button commits the
 * whole roles map via updateSetting('roles', next), dropping built-in rows
 * whose provider+model are both empty so an all-empty result stores nothing
 * (the backend stores nothing for an empty map too). Custom role entries —
 * any persisted key outside the built-in set — have no input row here and are
 * preserved untouched through saves.
 *
 * Draft re-sync policy: the draft is reset from settings.roles whenever the
 * persisted map *changes by value* (canonical-JSON comparison, not reference —
 * the settings object is re-created on every render). This keeps the inputs
 * in step with external config changes (config refresh, another surface
 * saving roles, a settings reload) without clobbering in-flight typing on
 * unrelated refreshes: while the persisted value is unchanged, user edits win;
 * once another surface persists different roles, the inputs follow the
 * persisted state, which is the conventional controlled-adjacent behavior —
 * the last writer to the persisted map is shown.
 */
export default function RoleModelsSection({ settings, updateSetting }: RoleModelsSectionProps) {
  const [draft, setDraft] = useState<RoleDraft>(() => draftFromRoles(settings.roles));
  const rolesKeyRef = useRef(rolesKey(settings.roles));

  useEffect(() => {
    const nextKey = rolesKey(settings.roles);
    if (nextKey === rolesKeyRef.current) return; // value unchanged — keep local edits
    rolesKeyRef.current = nextKey;
    setDraft(draftFromRoles(settings.roles));
  }, [settings.roles]);

  // Hide the section entirely if there's no way to persist (no updateSetting),
  // mirroring ProviderPrioritySection.
  if (!updateSetting) return null;

  const setField = (role: RoleName, field: 'provider' | 'model', value: string) => {
    setDraft((prev) => ({ ...prev, [role]: { ...prev[role], [field]: value } }));
  };

  // Build the full roles map to persist: start from the current persisted map
  // so custom roles (persisted keys outside the built-in set — no input row
  // here) survive every save untouched, then overlay the draft's built-in
  // rows. A built-in row whose provider+model are both empty is dropped, so
  // an all-empty built-in result stores nothing; a custom entry is never
  // touched by this component, so it is kept as-is even when every built-in
  // row is empty.
  const buildRolesMap = (): RolesMap => {
    const next: RolesMap = {};
    for (const [role, entry] of Object.entries(settings.roles ?? {})) {
      if (!BUILTIN_ROLE_SET.has(role)) next[role] = entry;
    }
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
