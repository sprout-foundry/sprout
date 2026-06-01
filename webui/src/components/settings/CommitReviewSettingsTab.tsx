import type { SproutSettings, ProviderOption } from '../../services/api';
import { getNestedValue } from './settingsHelpers';

interface CommitReviewSettingsTabProps {
  settings: SproutSettings;
  commitReviewProviders: ProviderOption[];
  updateSetting: (keyOrPath: string, value: unknown) => Promise<void>;
}

export default function CommitReviewSettingsTab({
  settings,
  commitReviewProviders,
  updateSetting,
}: CommitReviewSettingsTabProps) {
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
      <h4>Commit Message Generation</h4>
      <div className="config-help settings-help-spaced">
        Configure which provider and model to use for generating commit messages. Leave empty to use the default
        (LastUsedProvider).
      </div>

      <div className="config-item">
        <label htmlFor="commit-provider-select">Provider</label>
        <select
          id="commit-provider-select"
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
        <label htmlFor="commit-model-select">Model</label>
        <select
          id="commit-model-select"
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
          Configure which provider and model to use for code review commands (/review, /review-deep). Leave empty to use
          the default (LastUsedProvider).
        </div>

        <div className="config-item">
          <label htmlFor="review-provider-select">Provider</label>
          <select
            id="review-provider-select"
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
          <label htmlFor="review-model-select">Model</label>
          <select
            id="review-model-select"
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
  );
}
