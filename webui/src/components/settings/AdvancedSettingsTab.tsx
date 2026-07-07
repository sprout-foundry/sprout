/**
 * AdvancedSettingsTab — collapsible Advanced section combining the thin
 * Performance / Commit & Review / OCR tabs into one (per SP-017).
 *
 * Each subsection is wrapped in <Collapsible variant="flush"> so users
 * expand what they need. The same renderer hooks and config keys are
 * used as the original tabs, so all field writes go to the exact same
 * config paths and no behavior changes. AUDIT-GAP-1: replaced the
 * native <details> elements with the shared Collapsible primitive.
 */
import { Collapsible } from '@sprout/ui';
import type { SproutSettings, ProviderOption } from '../../services/api';
import { getNestedValue } from './settingsHelpers';
import type { FieldRenderers } from './useSettingsFieldRenderers';

export interface AdvancedSettingsTabProps {
  settings: SproutSettings;
  renderNumberInput: FieldRenderers['renderNumberInput'];
  renderTextInput: FieldRenderers['renderTextInput'];
  renderToggle: FieldRenderers['renderToggle'];
  commitReviewProviders: ProviderOption[];
  updateSetting: (keyOrPath: string, value: unknown) => Promise<void>;
}

export default function AdvancedSettingsTab({
  settings,
  renderNumberInput,
  renderTextInput,
  renderToggle,
  commitReviewProviders,
  updateSetting,
}: AdvancedSettingsTabProps) {
  const currentCommitProvider = String(getNestedValue(settings, 'commit_provider') || '');
  const currentCommitModel = String(getNestedValue(settings, 'commit_model') || '');
  const currentReviewProvider = String(getNestedValue(settings, 'review_provider') || '');
  const currentReviewModel = String(getNestedValue(settings, 'review_model') || '');

  const selectedCommitProvider = commitReviewProviders.find((p) => p.id === currentCommitProvider);
  const commitAvailableModels = selectedCommitProvider?.models || [];

  const selectedReviewProvider = commitReviewProviders.find((p) => p.id === currentReviewProvider);
  const reviewAvailableModels = selectedReviewProvider?.models || [];

  return (
    <div className="section">
      <div className="config-help settings-help-spaced">
        Low-touch settings: performance knobs, commit/review provider selection, and PDF OCR. Open what
        you need; the rest stays collapsed.
      </div>

      {/* Performance */}
      <Collapsible title="Performance" variant="flush">
        <div className="settings-section-spaced">
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
      </Collapsible>

      {/* Commit & Review */}
      <Collapsible title="Commit & Review" variant="flush">
        <div className="settings-section-spaced">
          <h4>Commit Message Generation</h4>
          <div className="config-help settings-help-spaced">
            Configure which provider and model to use for generating commit messages. Leave empty to use
            the default (LastUsedProvider).
          </div>

          <div className="config-item">
            <label htmlFor="commit-provider-select-advanced">Provider</label>
            <select
              id="commit-provider-select-advanced"
              className="styled-select"
              value={currentCommitProvider}
              onChange={(e) => updateSetting('commit_provider', e.target.value)}
            >
              <option value="">Default (inherit from main agent)</option>
              {commitReviewProviders.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </div>

          <div className="config-item">
            <label htmlFor="commit-model-select-advanced">Model</label>
            <select
              id="commit-model-select-advanced"
              className="styled-select"
              value={currentCommitModel}
              onChange={(e) => updateSetting('commit_model', e.target.value)}
            >
              <option value="">Default (use provider&apos;s default model)</option>
              {commitAvailableModels.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
          </div>

          <div className="settings-section-spaced-bordered">
            <h4>Code Review</h4>
            <div className="config-help settings-help-spaced">
              Configure which provider and model to use for code review commands (/review, /review-deep).
              Leave empty to use the default (LastUsedProvider).
            </div>

            <div className="config-item">
              <label htmlFor="review-provider-select-advanced">Provider</label>
              <select
                id="review-provider-select-advanced"
                className="styled-select"
                value={currentReviewProvider}
                onChange={(e) => updateSetting('review_provider', e.target.value)}
              >
                <option value="">Default (inherit from main agent)</option>
                {commitReviewProviders.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>

            <div className="config-item">
              <label htmlFor="review-model-select-advanced">Model</label>
              <select
                id="review-model-select-advanced"
                className="styled-select"
                value={currentReviewModel}
                onChange={(e) => updateSetting('review_model', e.target.value)}
              >
                <option value="">Default (use provider&apos;s default model)</option>
                {reviewAvailableModels.map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </select>
            </div>
          </div>
        </div>
      </Collapsible>

      {/* PDF OCR */}
      <Collapsible title="PDF OCR" variant="flush">
        <div className="settings-section-spaced">
          <h4>PDF OCR</h4>
          {renderToggle('pdf_ocr_enabled', 'Enable PDF OCR')}
          {renderTextInput('pdf_ocr_provider', 'Provider', 'zai, minimax, openrouter…')}
          {renderTextInput('pdf_ocr_model', 'Model', 'GLM-4.6V, MiniMax-VL, qwen-vl…')}
        </div>
      </Collapsible>
    </div>
  );
}
