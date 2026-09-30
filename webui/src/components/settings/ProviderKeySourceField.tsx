import './ProviderKeySourceField.css';

export type ProviderKeySource = 'api_key' | 'env_var';

interface ProviderKeySourceFieldProps {
  keySource: ProviderKeySource;
  onKeySourceChange: (source: ProviderKeySource) => void;
  apiKey: string;
  onApiKeyChange: (value: string) => void;
  envVar: string;
  onEnvVarChange: (value: string) => void;
  isEdit: boolean;
  envVarError?: string;
}

const OPTIONS: { value: ProviderKeySource; label: string; badge?: string }[] = [
  { value: 'api_key', label: 'API key', badge: 'Recommended' },
  { value: 'env_var', label: 'Environment variable' },
];

/** Chooses how a custom provider's API key is supplied. A pasted key is the
 *  default: it lives in sprout's credential store instead of the environment
 *  of every process the user's shell starts. */
export default function ProviderKeySourceField({
  keySource,
  onKeySourceChange,
  apiKey,
  onApiKeyChange,
  envVar,
  onEnvVarChange,
  isEdit,
  envVarError,
}: ProviderKeySourceFieldProps) {
  return (
    <fieldset className="provider-key-source">
      <legend>API key source</legend>
      <div className="provider-key-source-options" role="radiogroup" aria-label="API key source">
        {OPTIONS.map((opt) => (
          <label key={opt.value} className={`provider-key-source-option${keySource === opt.value ? ' selected' : ''}`}>
            <input
              type="radio"
              name="provider-key-source"
              value={opt.value}
              checked={keySource === opt.value}
              onChange={() => onKeySourceChange(opt.value)}
            />
            <span>{opt.label}</span>
            {opt.badge && <span className="provider-key-source-badge">{opt.badge}</span>}
          </label>
        ))}
      </div>

      {keySource === 'api_key' ? (
        <div className="form-row">
          <label htmlFor="provider-api-key">API key</label>
          <input
            id="provider-api-key"
            type="password"
            className="styled-input"
            value={apiKey}
            onChange={(e) => onApiKeyChange(e.target.value)}
            placeholder={isEdit ? 'Leave blank to keep the saved key' : 'Paste API key'}
            autoComplete="off"
          />
          <small className="config-help">
            Saved to sprout&apos;s credential store, not to the provider&apos;s config file.
          </small>
        </div>
      ) : (
        <div className="form-row">
          <label htmlFor="provider-env-var">Environment variable name</label>
          <input
            id="provider-env-var"
            type="text"
            className="styled-input"
            value={envVar}
            onChange={(e) => onEnvVarChange(e.target.value)}
            placeholder="MY_PROVIDER_API_KEY"
            aria-invalid={envVarError ? true : undefined}
            aria-describedby={envVarError ? 'provider-env-var-error' : undefined}
          />
          <small className="config-help">Read from sprout&apos;s environment each time the provider is used.</small>
          {envVarError && (
            <small id="provider-env-var-error" className="provider-key-source-error" role="alert">
              {envVarError}
            </small>
          )}
        </div>
      )}
    </fieldset>
  );
}
