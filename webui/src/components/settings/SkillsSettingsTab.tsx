import { useEffect, useState } from 'react';
import type { SproutSettings } from '../../services/api';
import { ApiService } from '../../services/api';
import type {
  SkillInstallResult,
  SkillRegistryEntry,
} from '../../services/api/types/settings';
import ListFilter from './ListFilter';

const SKILL_FILTER_THRESHOLD = 6;

interface SkillsSettingsTabProps {
  settings: SproutSettings;
  toggleSkill: (skillName: string, enabled: boolean) => Promise<void>;
}

interface InstalledSkill {
  id: string;
  origin?: { type?: string; url?: string; path?: string; registry_id?: string };
  installed_at?: string;
  updated_at?: string;
}

export default function SkillsSettingsTab({ settings, toggleSkill }: SkillsSettingsTabProps) {
  const api = ApiService.getInstance();
  const skills = settings.skills || {};
  const skillEntries = Object.entries(skills);
  const [skillFilter, setSkillFilter] = useState('');
  const normalizedSkillFilter = skillFilter.trim().toLowerCase();
  const filteredEntries = normalizedSkillFilter
    ? skillEntries.filter(([name]) => name.toLowerCase().includes(normalizedSkillFilter))
    : skillEntries;
  const enabledCount = skillEntries.filter(([, cfg]) => cfg.enabled).length;

  // ── Install panel state ──────────────────────────────────────────────
  const [installed, setInstalled] = useState<InstalledSkill[]>([]);
  const [registry, setRegistry] = useState<SkillRegistryEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const [installSource, setInstallSource] = useState('');
  const [installRef, setInstallRef] = useState('');
  const [installForce, setInstallForce] = useState(false);

  async function refresh() {
    setLoading(true);
    setError(null);
    try {
      const [list, reg] = await Promise.all([
        api.listInstalledSkills(),
        api.listSkillRegistry(),
      ]);
      setInstalled(list);
      setRegistry(reg);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function handleInstall(e: React.FormEvent) {
    e.preventDefault();
    if (!installSource.trim()) return;
    setBusy(true);
    setError(null);
    try {
      await api.installSkill(installSource.trim(), {
        ref: installRef.trim() || undefined,
        force: installForce,
      });
      setInstallSource('');
      setInstallRef('');
      setInstallForce(false);
      await refresh();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function handleUpdate(id: string) {
    setBusy(true);
    setError(null);
    try {
      await api.updateSkill(id);
      await refresh();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function handleRemove(id: string) {
    if (!window.confirm(`Remove skill "${id}"?`)) return;
    setBusy(true);
    setError(null);
    try {
      await api.removeSkill(id);
      await refresh();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  function handleRegistrySelect(entryId: string) {
    setInstallSource(entryId);
  }

  return (
    <div className="section">
      <h4>Skills</h4>

      {/* ── Install new skill panel ─────────────────────── */}
      <div className="skills-install-panel">
        <h5>Install new skill</h5>
        {error && (
          <div className="settings-error" role="alert">
            {error}
          </div>
        )}
        <form onSubmit={handleInstall}>
          <label>
            Source (path, git URL, or registry ID):
            <input
              type="text"
              value={installSource}
              onChange={(e) => setInstallSource(e.target.value)}
              data-testid="skills-install-source"
              placeholder="./my-skill or https://... or registry-id"
              disabled={busy}
            />
          </label>
          <label>
            Ref (optional, git branch/tag):
            <input
              type="text"
              value={installRef}
              onChange={(e) => setInstallRef(e.target.value)}
              data-testid="skills-install-ref"
              disabled={busy}
            />
          </label>
          <label>
            <input
              type="checkbox"
              checked={installForce}
              onChange={(e) => setInstallForce(e.target.checked)}
              data-testid="skills-install-force"
              disabled={busy}
            />
            Force overwrite if already installed
          </label>
          {registry.length > 0 && (
            <label>
              Registry starters:
              <select
                value=""
                onChange={(e) => {
                  if (e.target.value) handleRegistrySelect(e.target.value);
                }}
                data-testid="skills-registry-dropdown"
                disabled={busy}
              >
                <option value="">— pick a starter skill —</option>
                {registry.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.name} ({r.id})
                  </option>
                ))}
              </select>
            </label>
          )}
          <button
            type="submit"
            data-testid="skills-install-button"
            disabled={busy || !installSource.trim()}
          >
            Install
          </button>
        </form>
      </div>

      {/* ── Installed skills list (from API) ────────────── */}
      <div className="skills-installed-list">
        <h5>Installed ({installed.length})</h5>
        {loading ? (
          <div className="settings-empty">Loading…</div>
        ) : installed.length === 0 ? (
          <div className="settings-empty">No skills installed yet.</div>
        ) : (
          installed.map((s) => {
            const origin = s.origin;
            const summary = origin
              ? origin.type === 'git'
                ? `git: ${origin.url ?? ''}`
                : origin.type === 'path'
                  ? `path: ${origin.path ?? ''}`
                  : origin.type === 'registry'
                    ? `registry: ${origin.registry_id ?? ''}`
                    : origin.type ?? 'unknown'
              : '';
            return (
              <div
                key={s.id}
                className="skill-installed-row"
                data-testid={`skills-list-item-${s.id}`}
              >
                <div className="skill-installed-meta">
                  <strong>{s.id}</strong>
                  {summary && <span className="skill-installed-origin"> — {summary}</span>}
                </div>
                <div className="skill-installed-actions">
                  <button
                    type="button"
                    data-testid={`skills-update-button-${s.id}`}
                    onClick={() => handleUpdate(s.id)}
                    disabled={busy}
                  >
                    Update
                  </button>
                  <button
                    type="button"
                    data-testid={`skills-remove-button-${s.id}`}
                    onClick={() => handleRemove(s.id)}
                    disabled={busy}
                  >
                    Remove
                  </button>
                </div>
              </div>
            );
          })
        )}
      </div>

      {/* ── Existing toggle UI (preserve original behavior) ── */}
      <h5>
        Toggle enabled ({enabledCount}/{skillEntries.length} enabled)
      </h5>
      {skillEntries.length >= SKILL_FILTER_THRESHOLD && (
        <ListFilter
          value={skillFilter}
          onChange={setSkillFilter}
          placeholder={`Filter ${skillEntries.length} skills…`}
          ariaLabel="Filter skills"
        />
      )}
      {skillEntries.length === 0 ? (
        <div className="settings-empty">No skills configured</div>
      ) : normalizedSkillFilter && filteredEntries.length === 0 ? (
        <div className="settings-empty">No skills match "{skillFilter}"</div>
      ) : (
        <div className="skills-list">
          {filteredEntries.map(([name, cfg]) => {
            const enabled = cfg.enabled;
            return (
              <div key={name} className="skill-item">
                <span className="skill-item-name">{name}</span>
                <label className="styled-toggle">
                  <input
                    type="checkbox"
                    checked={enabled}
                    onChange={() => toggleSkill(name, !enabled)}
                  />
                  <span className="toggle-track" />
                </label>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
