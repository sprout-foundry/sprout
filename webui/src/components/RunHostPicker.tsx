/**
 * RunHostPicker — "Run on: <runner> · <mode> | Cloud", the host choice shown
 * by the escalation prompt and the terminal escalation toast (SP-159 §159d).
 *
 * Offline runners stay visible but disabled so the user sees why their
 * machine isn't offered; bare-metal runners carry a warning style because
 * commands run directly on the machine with no sandbox.
 */

import { AlertTriangle } from 'lucide-react';
import { CLOUD_HOST, hostForRunner, sameHost, type EscalationHost } from '../services/escalationHost';
import { isBareMetal, isRunnerSelectable, runnerModeLabel, type Runner } from '../services/runners';
import './RunHostPicker.css';

interface RunHostPickerProps {
  runners: Runner[];
  value: EscalationHost;
  onChange: (host: EscalationHost) => void;
  disabled?: boolean;
}

function runnerDetail(runner: Runner): string {
  const parts = [runnerModeLabel(runner)];
  if (runner.status === 'offline') parts.push('offline');
  else if (runner.status === 'busy') parts.push('busy');
  return parts.filter(Boolean).join(' · ');
}

export function RunHostPicker({ runners, value, onChange, disabled }: RunHostPickerProps) {
  const selectedRunner = value.kind === 'runner' ? runners.find((r) => r.runner_id === value.runnerId) : undefined;

  return (
    <div className="run-host-picker" data-testid="run-host-picker">
      <div className="run-host-picker-row" role="radiogroup" aria-label="Run on">
        <span className="run-host-picker-label">Run on:</span>
        {runners.map((runner) => {
          const host = hostForRunner(runner);
          const selected = sameHost(value, host);
          const detail = runnerDetail(runner);
          const bareMetal = isBareMetal(runner);
          const className = [
            'run-host-option',
            selected ? 'is-selected' : '',
            bareMetal ? 'run-host-option--warning' : '',
          ]
            .filter(Boolean)
            .join(' ');
          return (
            <button
              key={runner.runner_id}
              type="button"
              role="radio"
              aria-checked={selected}
              className={className}
              disabled={disabled || !isRunnerSelectable(runner)}
              onClick={() => onChange(host)}
              title={bareMetal ? 'Bare metal: commands run directly on this machine, with no sandbox.' : undefined}
              data-testid="run-host-option"
              data-runner-id={runner.runner_id}
            >
              {bareMetal ? <AlertTriangle size={12} aria-hidden="true" /> : null}
              <span className="run-host-option-name">{runner.name}</span>
              {detail ? <span className="run-host-option-detail">· {detail}</span> : null}
            </button>
          );
        })}
        <button
          type="button"
          role="radio"
          aria-checked={value.kind === 'cloud'}
          className={value.kind === 'cloud' ? 'run-host-option is-selected' : 'run-host-option'}
          disabled={disabled}
          onClick={() => onChange(CLOUD_HOST)}
          data-testid="run-host-option-cloud"
        >
          <span className="run-host-option-name">Cloud</span>
        </button>
      </div>
      {selectedRunner && isBareMetal(selectedRunner) ? (
        <p className="run-host-picker-warning" data-testid="run-host-bare-metal-warning">
          <AlertTriangle size={12} aria-hidden="true" />
          {selectedRunner.name} runs commands directly on the machine, with no sandbox.
        </p>
      ) : null}
    </div>
  );
}
