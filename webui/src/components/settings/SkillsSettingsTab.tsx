import type { SproutSettings } from '../../services/api';

interface SkillsSettingsTabProps {
  settings: SproutSettings;
  toggleSkill: (skillName: string, enabled: boolean) => Promise<void>;
}

export default function SkillsSettingsTab({ settings, toggleSkill }: SkillsSettingsTabProps) {
  const skills = settings.skills || {};
  const skillEntries = Object.entries(skills);

  if (skillEntries.length === 0) {
    return <div className="settings-empty">No skills available</div>;
  }

  return (
    <div className="section">
      <h4>Skills ({skillEntries.length})</h4>
      <div className="skills-list">
        {skillEntries.map(([name, cfg]) => {
          const enabled = cfg.enabled;
          return (
            <div key={name} className="skill-item">
              <span className="skill-item-name">{name}</span>
              <label className="styled-toggle">
                <input type="checkbox" checked={enabled} onChange={() => toggleSkill(name, !enabled)} />
                <span className="toggle-track" />
              </label>
            </div>
          );
        })}
      </div>
    </div>
  );
}
