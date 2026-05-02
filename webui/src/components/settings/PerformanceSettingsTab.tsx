import type { FieldRenderers } from './useSettingsFieldRenderers';

interface PerformanceSettingsTabProps {
  renderNumberInput: FieldRenderers['renderNumberInput'];
}

export default function PerformanceSettingsTab({
  renderNumberInput,
}: PerformanceSettingsTabProps) {
  return (
    <div className="section">
      <h4>API Timeouts</h4>
      {renderNumberInput('api_timeouts.connection_timeout_sec', 'Connection timeout (s)', 1, 300)}
      {renderNumberInput('api_timeouts.first_chunk_timeout_sec', 'First chunk timeout (s)', 1, 600)}
      {renderNumberInput('api_timeouts.chunk_timeout_sec', 'Chunk timeout (s)', 1, 600)}
      {renderNumberInput('api_timeouts.overall_timeout_sec', 'Overall timeout (s)', 1, 3600)}
    </div>
  );
}
