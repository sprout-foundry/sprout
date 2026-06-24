import type { FieldRenderers } from './useSettingsFieldRenderers';

interface PerformanceSettingsTabProps {
  renderNumberInput: FieldRenderers['renderNumberInput'];
  renderTextInput: FieldRenderers['renderTextInput'];
}

export default function PerformanceSettingsTab({ renderNumberInput, renderTextInput }: PerformanceSettingsTabProps) {
  return (
    <div className="section">
      <h4>API Timeouts</h4>
      {renderNumberInput('api_timeouts.connection_timeout_sec', 'Connection timeout (s)', 1, 300)}
      {renderNumberInput('api_timeouts.first_chunk_timeout_sec', 'First chunk timeout (s)', 1, 600)}
      {renderNumberInput('api_timeouts.chunk_timeout_sec', 'Chunk timeout (s)', 1, 600)}
      {renderNumberInput('api_timeouts.overall_timeout_sec', 'Overall timeout (s)', 1, 3600)}
      {renderNumberInput(
        'api_timeouts.commit_message_timeout_sec',
        'Commit message timeout (s)',
        1,
        1800,
        1,
        'Timeout for AI-generated commit messages. Defaults to 300s if unset.',
      )}

      <div className="settings-section-spaced">
        <h4>Cost Control</h4>
        {renderNumberInput(
          'max_context_tokens',
          'Max context tokens',
          0,
          undefined,
          1000,
          'Cap the effective context window for all models (e.g. 32000). Limits how many tokens can be claimed per request, reducing costs on large-context models. Leave blank or set to 0 for no limit.',
        )}
      </div>

      <div className="settings-section-spaced">
        <h4>Resource Storage</h4>
        {renderTextInput(
          'resource_directory',
          'Resource directory',
          '.sprout/resources',
          'Where captured web pages and vision artifacts are stored, relative to the workspace. Leave blank for the default. Override at runtime with --resource-directory.',
        )}
      </div>
    </div>
  );
}
