import { Monitor } from 'lucide-react';
import React from 'react';
import type { SproutInstance } from '../../services/api';

export interface WorkspaceInstancesProps {
  instances: SproutInstance[];
  selectedInstancePID: number;
  isSwitching: boolean;
  isSwitchingInstance: boolean;
  onInstanceChange: ((pid: number) => void) | undefined;
}

export const WorkspaceInstances: React.FC<WorkspaceInstancesProps> = ({
  instances,
  selectedInstancePID,
  isSwitching,
  isSwitchingInstance,
  onInstanceChange,
}) => {
  if (instances.length === 0) {
    return (
      <div className="location-switcher-item location-switcher-item-empty" role="option" aria-selected={false}>
        <span className="location-switcher-item-text">No instances available</span>
      </div>
    );
  }

  return (
    <>
      <div className="location-switcher-section-header" role="presentation">
        <Monitor size={12} className="location-switcher-section-icon" />
        Instances
      </div>

      {instances.map((instance) => {
        const name = instance.working_dir.split('/').filter(Boolean).slice(-2).join('/');
        const label = `${name} · pid:${instance.pid}`;

        return (
          <button
            key={`instance-${instance.id}`}
            type="button"
            className={`location-switcher-item ${instance.pid === selectedInstancePID ? 'active' : ''}`}
            onClick={() => {
              if (onInstanceChange && instance.pid) {
                onInstanceChange(instance.pid);
              }
            }}
            role="option"
            aria-selected={instance.pid === selectedInstancePID}
            aria-label={`Switch to instance ${label}`}
            disabled={isSwitching || isSwitchingInstance || !onInstanceChange || instance.is_host}
          >
            <span className="location-switcher-item-text">{label}</span>
            {instance.pid === selectedInstancePID ? <span className="location-switcher-item-indicator"><span className="location-switcher-item-dot" /></span> : null}
          </button>
        );
      })}
    </>
  );
};
