import { useState } from 'react';
import type { SproutSettings } from '../../services/api';
import ListFilter from './ListFilter';

const SKILL_FILTER_THRESHOLD = 6;

interface SkillsSettingsTabProps {
  settings: SproutSettings;
  toggleSkill: (skillName: string, enabled: boolean) => Promise<void>;
}

export default function SkillsSettingsTab({ settings, toggleSkill }: SkillsSettingsTabProps) {
  const skills = settings.skills || {};
  const skillEntries = Object.entries(skills);
  const [skillFilter, setSkillFilter] = useState('');
  const normalizedSkillFilter = skillFilter.trim().toLowerCase();
  const filteredEntries = normalizedSkillFilter
    ? skillEntries.filter(([name]) => name.toLowerCase().includes(normalizedSkillFilter))
    : skillEntries;
  const enabledCount = skillEntries.filter(([, cfg]) => cfg.enabled).length;

  if (skillEntries.length === 0) {
    return <div className="settings-empty">No skills available</div>;
  }

  return (
    <div className="section">
      <h4>
        Skills ({enabledCount}/{skillEntries.length} enabled)
      </h4>
      {skillEntries.length >= SKILL_FILTER_THRESHOLD && (
        <ListFilter
          value={skillFilter}
          onChange={setSkillFilter}
          placeholder={`Filter ${skillEntries.length} skills…`}
          ariaLabel="Filter skills"
        />
      )}
      {normalizedSkillFilter && filteredEntries.length === 0 ? (
        <div className="settings-empty">No skills match “{skillFilter}”</div>
      ) : (
        <div className="skills-list">
          {filteredEntries.map(([name, cfg]) => {
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
      )}
    </div>
  );
}
