/**
 * AdvancedSettingsTab — collapsible Advanced section combining the thin
 * Performance / Commit & Review / OCR-fallback tabs into one (per SP-017).
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
        Low-touch settings: performance knobs, commit/review provider selection, and PDF OCR. Open what you need; the
        rest stays collapsed.
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
            Configure which provider and model to use for generating commit messages. Leave empty to use the default
            (LastUsedProvider).
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
              Configure which provider and model to use for code review commands (/review, /review-deep). Leave empty to
              use the default (LastUsedProvider).
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

      {/* OCR fallback (SP-137: provider-neutral) */}
      <Collapsible title="OCR" variant="flush">
        <div className="settings-section-spaced">
          <h4>OCR Fallback</h4>
          {renderTextInput(
            'ocr_fallback_model',
            'OCR fallback model',
            'provider/model (e.g. openai/gpt-4o). Leave empty for native OCR only.',
          )}
          <div className="settings-section-spaced-bordered">
            <h4>PDF OCR Routing</h4>
            <div className="config-help settings-help-spaced">
              Route PDF text extraction to a specific provider and model instead of the session default. Leave empty to
              inherit.
            </div>
            {renderTextInput('pdf_ocr_provider', 'PDF OCR provider', 'e.g. openai — leave empty to inherit')}
            {renderTextInput('pdf_ocr_model', 'PDF OCR model', 'e.g. openai/gpt-4o — leave empty to inherit')}
          </div>
        </div>
      </Collapsible>

      {/* Context engine + display + update + git-safety toggles */}
      <Collapsible title="Context & Display" variant="flush">
        <div className="settings-section-spaced">
          <h4>Context Engine</h4>
          <div className="config-help settings-help-spaced">
            The context preset takes effect next session. &ldquo;Low context&rdquo; swaps in a curated tool allowlist
            and a lite system prompt; &ldquo;full&rdquo; is the default experience.
          </div>
          <div className="config-item">
            <label htmlFor="context-mode-select-advanced">Context mode</label>
            <select
              id="context-mode-select-advanced"
              className="styled-select"
              value={String(getNestedValue(settings, 'context_mode') || 'full')}
              onChange={(e) => updateSetting('context_mode', e.target.value === 'full' ? '' : e.target.value)}
            >
              <option value="full">Full (default)</option>
              <option value="low_context">Low context</option>
            </select>
          </div>
          {renderToggle(
            'refresh_system_prompt_on_model_change',
            'Refresh system prompt on model change',
            'Regenerate the embedded system prompt whenever the model changes.',
          )}
          <div className="settings-section-spaced-bordered">
            <h4>Tool Output</h4>
            {renderToggle(
              'show_tool_invocations',
              'Show tool invocations in chat',
              'Display each tool call as an inline badge while the agent works. Same setting as the /tools command.',
            )}
          </div>
        </div>
      </Collapsible>

      <Collapsible title="Completions" variant="flush">
        <div className="settings-section-spaced">
          <h4>Code Completions</h4>
          <div className="config-help settings-help-spaced">
            Provider and model used for inline code completions. Leave empty to use the default (LastUsedProvider).
          </div>
          <div className="config-item">
            <label htmlFor="completion-provider-select-advanced">Provider</label>
            <select
              id="completion-provider-select-advanced"
              className="styled-select"
              value={String(getNestedValue(settings, 'completion_provider') || '')}
              onChange={(e) => updateSetting('completion_provider', e.target.value)}
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
            <label htmlFor="completion-model-select-advanced">Model</label>
            <select
              id="completion-model-select-advanced"
              className="styled-select"
              value={String(getNestedValue(settings, 'completion_model') || '')}
              onChange={(e) => updateSetting('completion_model', e.target.value)}
            >
              <option value="">Default (use provider&apos;s default model)</option>
              {(
                commitReviewProviders.find((p) => p.id === getNestedValue(settings, 'completion_provider'))?.models ||
                []
              ).map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
          </div>
        </div>
      </Collapsible>

      <Collapsible title="Git Safety" variant="flush">
        <div className="settings-section-spaced">
          {renderToggle(
            'allow_git_history_rewrite',
            'Allow git history rewrite',
            'Permits the agent to run history-rewriting git commands (reset --hard, rebase, filter-branch) without prompting. Off by default — enabling risks losing committed work.',
          )}
        </div>
      </Collapsible>

      <Collapsible title="Updates" variant="flush">
        <div className="settings-section-spaced">
          {renderToggle(
            'disable_update_check',
            'Disable update check',
            'Stops the CLI/WebUI from checking GitHub for newer releases. The update banner disappears.',
          )}
        </div>
      </Collapsible>
    </div>
  );
}
