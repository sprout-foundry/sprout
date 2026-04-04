import React, { useState, useCallback, useRef, useEffect } from 'react';
import './SettingsPanel.css';
import { ApiService, type LeditSettings, type ProviderOption } from '../services/api';
import { Pencil, Plus, Trash2 } from 'lucide-react';

/* ─── Types ──────────────────────────────────────────────────── */

interface SubagentTypeEntry {
  id: string;
  name: string;
  description: string;
  provider: string;
  model: string;
  system_prompt: string;
  system_prompt_text?: string;
  allowed_tools: string[];
  aliases: string[];
  enabled: boolean;
}

type SettingsSubTab = 'general' | 'security' | 'performance' | 'subagents' | 'pdf-ocr' | 'mcp' | 'providers' | 'skills';

interface SettingsPanelProps {
  settings: LeditSettings | null;
  onSettingsChanged: (settings: LeditSettings) => void;
}

/** Toast auto-dismisses after 2s */
interface Toast {
  id: number;
  message: string;
  type: 'success' | 'error';
}

/* ─── Sub-tab definitions ────────────────────────────────────── */

const SUB_TABS: { id: SettingsSubTab; label: string }[] = [
  { id: 'general', label: 'General' },
  { id: 'security', label: 'Security' },
  { id: 'performance', label: 'Perf' },
  { id: 'subagents', label: 'Subagents' },
  { id: 'pdf-ocr', label: 'OCR' },
  { id: 'mcp', label: 'MCP' },
  { id: 'providers', label: 'Providers' },
  { id: 'skills', label: 'Skills' },
];

/* ─── Helpers ────────────────────────────────────────────────── */

/** Get a nested value from an object using dot-notation key */
function getNestedValue(obj: Record<string, any>, key: string): any {
  return key.split('.').reduce((o: any, k: string) => (o && o[k] !== undefined ? o[k] : ''), obj);
}

/** Set a nested value in an object using dot-notation key (immutable) */
function setNestedValue(obj: Record<string, any>, key: string, value: any): Record<string, any> {
  const parts = key.split('.');
  const result = { ...obj };
  let current: any = result;
  for (let i = 0; i < parts.length - 1; i++) {
    if (current[parts[i]] === undefined || typeof current[parts[i]] !== 'object') {
      current[parts[i]] = {};
    } else {
      current[parts[i]] = { ...current[parts[i]] };
    }
    current = current[parts[i]];
  }
  current[parts[parts.length - 1]] = value;
  return result;
}

let toastCounter = 0;

/* ─── Component ──────────────────────────────────────────────── */

const SettingsPanel: React.FC<SettingsPanelProps> = ({ settings, onSettingsChanged }) => {
  const [activeSubTab, setActiveSubTab] = useState<SettingsSubTab>('general');
  const [savingKey, setSavingKey] = useState<string | null>(null);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [textDrafts, setTextDrafts] = useState<Record<string, string>>({});

  // MCP / Provider form state
  const [editingServer, setEditingServer] = useState<{ mode: 'add' | 'edit'; originalName?: string } | null>(null);
  const [serverName, setServerName] = useState('');
  const [serverCommand, setServerCommand] = useState('');
  const [serverArgs, setServerArgs] = useState('');

  const [editingProvider, setEditingProvider] = useState<{ mode: 'add' | 'edit'; originalName?: string } | null>(null);
  const [providerName, setProviderName] = useState('');
  const [providerApiBase, setProviderApiBase] = useState('');
  const [providerModelName, setProviderModelName] = useState('');
  const [providerContextSize, setProviderContextSize] = useState(32768);
  const [providerEnvVar, setProviderEnvVar] = useState('');
  const [providerSupportsVision, setProviderSupportsVision] = useState(false);
  const [providerVisionModel, setProviderVisionModel] = useState('');
  const [providerModelContextSizes, setProviderModelContextSizes] = useState<string>('');

  // Subagent providers/models for dropdowns
  const [subagentProviders, setSubagentProviders] = useState<ProviderOption[]>([]);
  const [subagentTypes, setSubagentTypes] = useState<Record<string, SubagentTypeEntry>>({});
  const [subagentSavingPersona, setSubagentSavingPersona] = useState<string | null>(null);

  const api = ApiService.getInstance();
  const toastTimerRef = useRef<ReturnType<typeof setTimeout>>();
  const textSaveTimersRef = useRef<Record<string, ReturnType<typeof setTimeout>>>({});

  // Keep a ref to settings for async mutation callbacks.
  const settingsRef = useRef(settings);
  useEffect(() => {
    settingsRef.current = settings;
  }, [settings]);

  // Cleanup toast timer on unmount
  useEffect(() => {
    const pendingTextSaveTimers = textSaveTimersRef.current;
    return () => {
      if (toastTimerRef.current) clearTimeout(toastTimerRef.current);
      Object.values(pendingTextSaveTimers).forEach(clearTimeout);
    };
  }, []);

  useEffect(() => {
    if (!settings) return;

    setTextDrafts((prev) => {
      let next = prev;
      let changed = false;

      Object.entries(prev).forEach(([key, draftValue]) => {
        const persistedValue = String(getNestedValue(settings as any, key) || '');
        if (draftValue === persistedValue) {
          if (next === prev) next = { ...prev };
          delete next[key];
          changed = true;
        }
      });

      return changed ? next : prev;
    });
  }, [settings]);

  // Fetch subagent types + providers when subagents tab is activated
  useEffect(() => {
    if (activeSubTab !== 'subagents') return;
    let cancelled = false;
    (async () => {
      try {
        const data = await api.getSubagentTypes();
        if (cancelled) return;
        setSubagentProviders((data.available_providers || []) as ProviderOption[]);
        setSubagentTypes((data.subagent_types || {}) as Record<string, SubagentTypeEntry>);
      } catch {
        // Silently fail — dropdowns will just be empty
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [activeSubTab, api]);

  /* ─── Toast helpers ──────────────────────────────────────── */

  const showToast = useCallback((message: string, type: 'success' | 'error') => {
    const id = ++toastCounter;
    setToasts((prev) => [...prev, { id, message, type }]);
    // Auto-remove after 2s
    if (toastTimerRef.current) clearTimeout(toastTimerRef.current);
    toastTimerRef.current = setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
    }, 2000);
  }, []);

  /* ─── Settings mutation helpers ──────────────────────────── */

  /**
   * Update a top-level or deeply nested setting.
   * Optimistically updates local state, then persists via API.
   */
  const updateSetting = useCallback(
    async (keyOrPath: string, value: any) => {
      const current = settingsRef.current;
      if (!current) return;

      const prev = { ...current };
      setSavingKey(keyOrPath);

      try {
        const updated = setNestedValue(current as any, keyOrPath, value) as LeditSettings;
        onSettingsChanged(updated);
        await api.updateSettings({ [keyOrPath]: value });
        showToast('Saved', 'success');
      } catch {
        onSettingsChanged(prev);
        showToast('Save failed', 'error');
      } finally {
        setSavingKey(null);
      }
    },
    [onSettingsChanged, api, showToast],
  );

  /* ─── Field render helpers ──────────────────────────────── */

  const renderToggle = (settingKey: string, label: string) => {
    if (!settings) return null;
    const checked = !!getNestedValue(settings as any, settingKey);
    return (
      <label className="styled-toggle">
        <input type="checkbox" checked={checked} onChange={() => updateSetting(settingKey, !checked)} />
        <span className="toggle-track" />
        <span className="toggle-label">{label}</span>
      </label>
    );
  };

  const renderSelect = (settingKey: string, label: string, options: string[]) => {
    if (!settings) return null;
    const value = String(getNestedValue(settings as any, settingKey) || '');
    return (
      <div className="config-item">
        <label htmlFor={`setting-${settingKey}`}>{label}</label>
        <select
          id={`setting-${settingKey}`}
          value={value}
          onChange={(e) => updateSetting(settingKey, e.target.value)}
          className="styled-select"
        >
          {options.map((opt) => (
            <option key={opt} value={opt}>
              {opt}
            </option>
          ))}
        </select>
      </div>
    );
  };

  const renderNumberInput = (settingKey: string, label: string, min?: number, max?: number, step = 1) => {
    if (!settings) return null;
    const value = getNestedValue(settings as any, settingKey);
    return (
      <div className="config-item">
        <label htmlFor={`setting-${settingKey}`}>{label}</label>
        <input
          id={`setting-${settingKey}`}
          type="number"
          className="styled-input config-row-input"
          value={value ?? ''}
          min={min}
          max={max}
          step={step}
          onChange={(e) => {
            const v = e.target.value === '' ? 0 : Number(e.target.value);
            updateSetting(settingKey, v);
          }}
        />
      </div>
    );
  };

  const renderTextInput = (settingKey: string, label: string, placeholder?: string) => {
    if (!settings) return null;
    const persistedValue = String(getNestedValue(settings as any, settingKey) || '');
    const value = textDrafts[settingKey] ?? persistedValue;
    return (
      <div className="config-item">
        <label htmlFor={`setting-${settingKey}`}>{label}</label>
        <input
          id={`setting-${settingKey}`}
          type="text"
          className="styled-input"
          value={value}
          placeholder={placeholder}
          onChange={(e) => {
            const nextValue = e.target.value;
            setTextDrafts((prev) => ({ ...prev, [settingKey]: nextValue }));

            if (textSaveTimersRef.current[settingKey]) {
              clearTimeout(textSaveTimersRef.current[settingKey]);
            }

            textSaveTimersRef.current[settingKey] = setTimeout(() => {
              delete textSaveTimersRef.current[settingKey];
              void updateSetting(settingKey, nextValue);
            }, 250);
          }}
          onBlur={() => {
            if (textSaveTimersRef.current[settingKey]) {
              clearTimeout(textSaveTimersRef.current[settingKey]);
              delete textSaveTimersRef.current[settingKey];
            }

            const draftValue = textDrafts[settingKey];
            if (draftValue !== undefined && draftValue !== persistedValue) {
              void updateSetting(settingKey, draftValue);
            }
          }}
        />
      </div>
    );
  };

  const renderTextareaInput = (
    settingKey: string,
    label: string,
    placeholder?: string,
    rows = 10,
    helpText?: string,
  ) => {
    if (!settings) return null;
    const persistedValue = String(getNestedValue(settings as any, settingKey) || '');
    const value = textDrafts[settingKey] ?? persistedValue;
    return (
      <div className="config-item">
        <label htmlFor={`setting-${settingKey}`}>{label}</label>
        <textarea
          id={`setting-${settingKey}`}
          className="styled-input styled-textarea"
          value={value}
          rows={rows}
          placeholder={placeholder}
          onChange={(e) => {
            const nextValue = e.target.value;
            setTextDrafts((prev) => ({ ...prev, [settingKey]: nextValue }));

            if (textSaveTimersRef.current[settingKey]) {
              clearTimeout(textSaveTimersRef.current[settingKey]);
            }

            textSaveTimersRef.current[settingKey] = setTimeout(() => {
              delete textSaveTimersRef.current[settingKey];
              void updateSetting(settingKey, nextValue);
            }, 400);
          }}
          onBlur={() => {
            if (textSaveTimersRef.current[settingKey]) {
              clearTimeout(textSaveTimersRef.current[settingKey]);
              delete textSaveTimersRef.current[settingKey];
            }

            const draftValue = textDrafts[settingKey];
            if (draftValue !== undefined && draftValue !== persistedValue) {
              void updateSetting(settingKey, draftValue);
            }
          }}
        />
        {helpText && <div className="config-help">{helpText}</div>}
      </div>
    );
  };

  /* ─── Saving indicator ─────────────────────────────────── */

  const renderSaving = () => {
    if (!savingKey) return null;
    return (
      <span className="settings-saving">
        <span className="saving-dot" />
        Saving…
      </span>
    );
  };

  /* ─── MCP server CRUD ──────────────────────────────────── */

  const resetServerForm = () => {
    setEditingServer(null);
    setServerName('');
    setServerCommand('');
    setServerArgs('');
  };

  const handleAddServer = async () => {
    if (!serverName.trim()) return;
    const server: Record<string, any> = { command: serverCommand };
    if (serverArgs.trim()) {
      server.args = serverArgs.split(/\s+/).filter(Boolean);
    }
    setSavingKey('mcp-server-add');
    try {
      await api.addMCPServer({ name: serverName.trim(), ...server });
      // Refresh settings
      const fresh = await api.getSettings();
      onSettingsChanged(fresh);
      showToast('Server added', 'success');
      resetServerForm();
    } catch {
      showToast('Failed to add server', 'error');
    } finally {
      setSavingKey(null);
    }
  };

  const handleUpdateServer = async () => {
    if (!editingServer?.originalName || !serverName.trim()) return;
    const server: Record<string, any> = { command: serverCommand };
    if (serverArgs.trim()) {
      server.args = serverArgs.split(/\s+/).filter(Boolean);
    }
    setSavingKey('mcp-server-update');
    try {
      await api.updateMCPServer(editingServer.originalName, { name: serverName.trim(), ...server });
      const fresh = await api.getSettings();
      onSettingsChanged(fresh);
      showToast('Server updated', 'success');
      resetServerForm();
    } catch {
      showToast('Failed to update server', 'error');
    } finally {
      setSavingKey(null);
    }
  };

  const handleDeleteServer = async (name: string) => {
    setSavingKey('mcp-server-delete');
    try {
      await api.deleteMCPServer(name);
      const fresh = await api.getSettings();
      onSettingsChanged(fresh);
      showToast('Server deleted', 'success');
      if (editingServer?.originalName === name) resetServerForm();
    } catch {
      showToast('Failed to delete server', 'error');
    } finally {
      setSavingKey(null);
    }
  };

  /* ─── Custom Provider CRUD ─────────────────────────────── */

  const resetProviderForm = () => {
    setEditingProvider(null);
    setProviderName('');
    setProviderApiBase('');
    setProviderModelName('');
    setProviderContextSize(32768);
    setProviderEnvVar('');
    setProviderSupportsVision(false);
    setProviderVisionModel('');
    setProviderModelContextSizes('');
  };

  const handleAddProvider = async () => {
    if (!providerName.trim()) return;
    const modelName = providerModelName.trim();
    const supportsVision = providerSupportsVision;
    const visionModel = providerVisionModel.trim() || modelName;
    const envVar = providerEnvVar.trim();

    // Parse model context sizes from format "model1:8192,model2:131072"
    const modelContextSizes: Record<string, number> = {};
    if (providerModelContextSizes.trim()) {
      const pairs = providerModelContextSizes
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
      for (const pair of pairs) {
        const [model, size] = pair.split(':');
        if (model && size) {
          const sizeNum = parseInt(size, 10);
          if (!isNaN(sizeNum) && sizeNum > 0) {
            modelContextSizes[model.trim()] = sizeNum;
          }
        }
      }
    }

    const provider: Record<string, any> = {
      endpoint: providerApiBase.trim(),
      model_name: modelName,
      context_size: providerContextSize,
      model_context_sizes: Object.keys(modelContextSizes).length > 0 ? modelContextSizes : undefined,
      env_var: envVar,
      requires_api_key: envVar.length > 0,
      supports_vision: supportsVision,
      vision_model: supportsVision ? visionModel : '',
    };
    setSavingKey('provider-add');
    try {
      await api.addCustomProvider({ name: providerName.trim(), ...provider });
      const fresh = await api.getSettings();
      onSettingsChanged(fresh);
      showToast('Provider added', 'success');
      resetProviderForm();
    } catch {
      showToast('Failed to add provider', 'error');
    } finally {
      setSavingKey(null);
    }
  };

  const handleUpdateProvider = async () => {
    if (!editingProvider?.originalName || !providerName.trim()) return;
    const modelName = providerModelName.trim();
    const supportsVision = providerSupportsVision;
    const visionModel = providerVisionModel.trim() || modelName;
    const envVar = providerEnvVar.trim();

    // Parse model context sizes from format "model1:8192,model2:131072"
    const modelContextSizes: Record<string, number> = {};
    if (providerModelContextSizes.trim()) {
      const pairs = providerModelContextSizes
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
      for (const pair of pairs) {
        const [model, size] = pair.split(':');
        if (model && size) {
          const sizeNum = parseInt(size, 10);
          if (!isNaN(sizeNum) && sizeNum > 0) {
            modelContextSizes[model.trim()] = sizeNum;
          }
        }
      }
    }

    const provider: Record<string, any> = {
      endpoint: providerApiBase.trim(),
      model_name: modelName,
      context_size: providerContextSize,
      model_context_sizes: Object.keys(modelContextSizes).length > 0 ? modelContextSizes : undefined,
      env_var: envVar,
      requires_api_key: envVar.length > 0,
      supports_vision: supportsVision,
      vision_model: supportsVision ? visionModel : '',
    };
    setSavingKey('provider-update');
    try {
      await api.updateCustomProvider(editingProvider.originalName, { name: providerName.trim(), ...provider });
      const fresh = await api.getSettings();
      onSettingsChanged(fresh);
      showToast('Provider updated', 'success');
      resetProviderForm();
    } catch {
      showToast('Failed to update provider', 'error');
    } finally {
      setSavingKey(null);
    }
  };

  const handleDeleteProvider = async (name: string) => {
    setSavingKey('provider-delete');
    try {
      await api.deleteCustomProvider(name);
      const fresh = await api.getSettings();
      onSettingsChanged(fresh);
      showToast('Provider deleted', 'success');
      if (editingProvider?.originalName === name) resetProviderForm();
    } catch {
      showToast('Failed to delete provider', 'error');
    } finally {
      setSavingKey(null);
    }
  };

  /* ─── Skills toggle ────────────────────────────────────── */

  const toggleSkill = async (skillName: string, enabled: boolean) => {
    if (!settings) return;
    setSavingKey(`skill-${skillName}`);
    try {
      const updatedSkills = {
        ...settings.skills,
        [skillName]: {
          ...(settings.skills?.[skillName] || {}),
          enabled,
        },
      };
      await api.updateSkills(updatedSkills);
      onSettingsChanged({ ...settings, skills: updatedSkills });
      showToast(`${skillName} ${enabled ? 'enabled' : 'disabled'}`, 'success');
    } catch {
      showToast('Failed to update skill', 'error');
    } finally {
      setSavingKey(null);
    }
  };

  /* ─── Render sub-tab content ───────────────────────────── */

  const renderContent = () => {
    if (!settings) {
      return <div className="settings-empty">Loading settings…</div>;
    }

    switch (activeSubTab) {
      /* ── General ─────────────────────────────────────────── */
      case 'general':
        return (
          <div className="section">
            <h4>Behavior</h4>
            {renderSelect('reasoning_effort', 'Reasoning effort', ['low', 'medium', 'high'])}
            {renderToggle('skip_prompt', 'Skip confirmation prompt')}
            {renderToggle('enable_pre_write_validation', 'Pre-write validation')}
            {renderSelect('history_scope', 'History scope', ['session', 'project', 'global'])}
            {renderTextareaInput(
              'system_prompt_text',
              'System prompt',
              'Leave blank to use the embedded default system prompt.',
              12,
              'Applies to the main agent. Leave blank to use the built-in default prompt.',
            )}
          </div>
        );

      /* ── Security ────────────────────────────────────────── */
      case 'security':
        return (
          <div className="section">
            <h4>Security</h4>
            {renderNumberInput('security_validation.threshold', 'Validation threshold (0-2)', 0, 2)}
            {renderSelect('self_review_gate_mode', 'Self-review gate', ['off', 'code', 'always'])}
            <div style={{ marginTop: 'var(--space-5)' }}>
              <h4>Git Permissions</h4>
              {renderToggle('allow_orchestrator_git_write', 'Allow orchestrator git write')}
            </div>
          </div>
        );

      /* ── Performance ─────────────────────────────────────── */
      case 'performance':
        return (
          <div className="section">
            <h4>API Timeouts</h4>
            {renderNumberInput('api_timeouts.connection_timeout_sec', 'Connection timeout (s)', 1, 300)}
            {renderNumberInput('api_timeouts.first_chunk_timeout_sec', 'First chunk timeout (s)', 1, 600)}
            {renderNumberInput('api_timeouts.chunk_timeout_sec', 'Chunk timeout (s)', 1, 600)}
            {renderNumberInput('api_timeouts.overall_timeout_sec', 'Overall timeout (s)', 1, 3600)}
          </div>
        );

      /* ── Subagents ──────────────────────────────────────── */
      case 'subagents': {
        const currentSubProvider = String(getNestedValue(settings as any, 'subagent_provider') || '');
        const currentSubModel = String(getNestedValue(settings as any, 'subagent_model') || '');

        // Get models for the currently selected provider
        const selectedProvider = subagentProviders.find((p) => p.id === currentSubProvider);
        const availableModels = selectedProvider?.models || [];

        // Sort personas for display
        const personaEntries = Object.entries(subagentTypes)
          .filter(([, v]) => v.enabled)
          .sort(([a], [b]) => a.localeCompare(b));

        return (
          <div className="section">
            <h4>Default Subagent</h4>

            {/* Provider dropdown */}
            <div className="config-item">
              <label htmlFor="subagent-provider-select">Provider</label>
              <select
                id="subagent-provider-select"
                className="styled-select"
                value={currentSubProvider}
                onChange={(e) => updateSetting('subagent_provider', e.target.value)}
              >
                <option value="">Default (inherit from main agent)</option>
                {subagentProviders.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>

            {/* Model dropdown */}
            <div className="config-item">
              <label htmlFor="subagent-model-select">Model</label>
              <select
                id="subagent-model-select"
                className="styled-select"
                value={currentSubModel}
                onChange={(e) => updateSetting('subagent_model', e.target.value)}
              >
                <option value="">Default (use provider's default model)</option>
                {availableModels.map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </select>
            </div>

            {/* Default persona dropdown */}
            {renderSelect('default_subagent_persona', 'Default Persona', [
              'general',
              'coder',
              'refactor',
              'debugger',
              'tester',
              'code_reviewer',
              'researcher',
              'web_scraper',
              'orchestrator',
              'computer_user',
            ])}

            {/* ── Per-persona model mapping ──────────────── */}
            <div style={{ marginTop: 'var(--space-5)' }}>
              <h4>Per-Persona Overrides</h4>
              <div className="config-help" style={{ marginBottom: 'var(--space-4)' }}>
                Set a specific provider and/or model for individual personas. Empty values inherit from the default
                subagent settings above.
              </div>

              {personaEntries.length === 0 && <div className="settings-empty">No personas available</div>}

              <div className="persona-mapping-list">
                {personaEntries.map(([personaId, persona]) => {
                  const isSaving = subagentSavingPersona === personaId;
                  const personaProvider = persona.provider || '';
                  const personaModelsForProvider =
                    subagentProviders.find((p) => p.id === personaProvider)?.models || [];

                  return (
                    <div key={personaId} className="persona-mapping-row">
                      <span className="persona-mapping-name" title={persona.description}>
                        {persona.name}
                      </span>
                      <select
                        className="styled-select persona-mapping-select"
                        value={personaProvider}
                        onChange={async (e) => {
                          setSubagentSavingPersona(personaId);
                          try {
                            await api.updateSubagentType(personaId, {
                              provider: e.target.value,
                              model: '', // clear model when provider changes
                            });
                            setSubagentTypes((prev) => ({
                              ...prev,
                              [personaId]: { ...prev[personaId], provider: e.target.value, model: '' },
                            }));
                            showToast(`${persona.name}: provider updated`, 'success');
                          } catch {
                            showToast(`Failed to update ${persona.name}`, 'error');
                          } finally {
                            setSubagentSavingPersona(null);
                          }
                        }}
                        disabled={isSaving}
                      >
                        <option value="">Default</option>
                        {subagentProviders.map((p) => (
                          <option key={p.id} value={p.id}>
                            {p.name}
                          </option>
                        ))}
                      </select>
                      <select
                        className="styled-select persona-mapping-select"
                        value={persona.model || ''}
                        onChange={async (e) => {
                          setSubagentSavingPersona(personaId);
                          try {
                            await api.updateSubagentType(personaId, {
                              model: e.target.value,
                            });
                            setSubagentTypes((prev) => ({
                              ...prev,
                              [personaId]: { ...prev[personaId], model: e.target.value },
                            }));
                            showToast(`${persona.name}: model updated`, 'success');
                          } catch {
                            showToast(`Failed to update ${persona.name}`, 'error');
                          } finally {
                            setSubagentSavingPersona(null);
                          }
                        }}
                        disabled={isSaving || personaProvider === ''}
                      >
                        <option value="">Default</option>
                        {personaModelsForProvider.map((m) => (
                          <option key={m} value={m}>
                            {m}
                          </option>
                        ))}
                      </select>
                    </div>
                  );
                })}
              </div>
            </div>
          </div>
        );
      }

      /* ── PDF OCR ─────────────────────────────────────────── */
      case 'pdf-ocr':
        return (
          <div className="section">
            <h4>PDF OCR</h4>
            {renderToggle('pdf_ocr_enabled', 'Enable PDF OCR')}
            {renderTextInput('pdf_ocr_provider', 'Provider', 'zai, minimax, openrouter…')}
            {renderTextInput('pdf_ocr_model', 'Model', 'GLM-4.6V, MiniMax-VL, qwen-vl…')}
          </div>
        );

      /* ── MCP ─────────────────────────────────────────────── */
      case 'mcp': {
        const mcpSettings = settings.mcp || {};
        const servers = mcpSettings.servers || {};
        const serverEntries = Object.entries(servers);

        return (
          <div className="section">
            <h4>MCP Configuration</h4>

            {/* MCP toggles */}
            {renderToggle('mcp.enabled', 'MCP enabled')}
            {renderToggle('mcp.auto_start', 'Auto-start servers')}
            {renderToggle('mcp.auto_discover', 'Auto-discover servers')}
            {renderTextInput('mcp.timeout', 'Timeout (e.g. 30s)', '30s')}

            {/* Server list */}
            <div style={{ marginTop: 'var(--space-5)' }}>
              <h4>Servers ({serverEntries.length})</h4>

              {serverEntries.length === 0 && !editingServer && (
                <div className="settings-empty">No MCP servers configured</div>
              )}

              <div className="crud-list">
                {serverEntries.map(([name, cfg]: [string, any]) => (
                  <div key={name} className="crud-item">
                    <span className="crud-item-name">{name}</span>
                    <span className="crud-item-detail">{cfg?.command || ''}</span>
                    <button
                      type="button"
                      className="crud-btn"
                      title="Edit server"
                      onClick={() => {
                        setEditingServer({ mode: 'edit', originalName: name });
                        setServerName(name);
                        setServerCommand(cfg?.command || '');
                        setServerArgs(Array.isArray(cfg?.args) ? cfg.args.join(' ') : '');
                      }}
                    >
                      <Pencil size={12} />
                    </button>
                    <button
                      type="button"
                      className="crud-btn danger"
                      title="Delete server"
                      onClick={() => handleDeleteServer(name)}
                    >
                      <Trash2 size={12} />
                    </button>
                  </div>
                ))}

                {/* Inline form (Add / Edit) */}
                {editingServer && (
                  <div className="crud-inline-form">
                    <div className="form-row">
                      <label>Name</label>
                      <input
                        type="text"
                        className="styled-input"
                        value={serverName}
                        onChange={(e) => setServerName(e.target.value)}
                        placeholder="server-name"
                        disabled={editingServer.mode === 'edit'}
                      />
                    </div>
                    <div className="form-row">
                      <label>Command</label>
                      <input
                        type="text"
                        className="styled-input"
                        value={serverCommand}
                        onChange={(e) => setServerCommand(e.target.value)}
                        placeholder="npx or path/to/binary"
                      />
                    </div>
                    <div className="form-row">
                      <label>Args (space-separated)</label>
                      <input
                        type="text"
                        className="styled-input"
                        value={serverArgs}
                        onChange={(e) => setServerArgs(e.target.value)}
                        placeholder="--flag value"
                      />
                    </div>
                    <div className="form-actions">
                      <button
                        type="button"
                        className="form-btn primary"
                        onClick={editingServer.mode === 'edit' ? handleUpdateServer : handleAddServer}
                      >
                        {editingServer.mode === 'edit' ? 'Update' : 'Add'}
                      </button>
                      <button type="button" className="form-btn cancel" onClick={resetServerForm}>
                        Cancel
                      </button>
                    </div>
                  </div>
                )}

                {!editingServer && (
                  <button
                    type="button"
                    className="crud-add-btn"
                    onClick={() => {
                      setEditingServer({ mode: 'add' });
                      setServerName('');
                      setServerCommand('');
                      setServerArgs('');
                    }}
                  >
                    <Plus size={14} /> Add server
                  </button>
                )}
              </div>
            </div>
          </div>
        );
      }

      /* ── Custom Providers ────────────────────────────────── */
      case 'providers': {
        const customProviders = settings.custom_providers || {};
        const providerEntries = Object.entries(customProviders);

        return (
          <div className="section">
            <h4>Custom Providers ({providerEntries.length})</h4>

            {providerEntries.length === 0 && !editingProvider && (
              <div className="settings-empty">No custom providers configured</div>
            )}

            <div className="crud-list">
              {providerEntries.map(([name, cfg]: [string, any]) => (
                <div key={name} className="crud-item">
                  <span className="crud-item-name">{name}</span>
                  <span className="crud-item-detail">{cfg?.endpoint || cfg?.api_base || ''}</span>
                  <button
                    type="button"
                    className="crud-btn"
                    title="Edit provider"
                    onClick={() => {
                      setEditingProvider({ mode: 'edit', originalName: name });
                      setProviderName(name);
                      setProviderApiBase(cfg?.endpoint || cfg?.api_base || '');
                      setProviderModelName(
                        cfg?.model_name || (Array.isArray(cfg?.models) && cfg.models.length > 0 ? cfg.models[0] : ''),
                      );
                      setProviderContextSize(cfg?.context_size || 32768);
                      setProviderEnvVar(cfg?.env_var || '');
                      setProviderSupportsVision(!!cfg?.supports_vision);
                      setProviderVisionModel(cfg?.vision_model || '');
                      // Format model_context_sizes as "model1:8192,model2:131072"
                      if (cfg?.model_context_sizes && typeof cfg.model_context_sizes === 'object') {
                        const pairs = Object.entries(cfg.model_context_sizes)
                          .map(([model, size]) => `${model}:${size}`)
                          .join(',');
                        setProviderModelContextSizes(pairs);
                      } else {
                        setProviderModelContextSizes('');
                      }
                    }}
                  >
                    <Pencil size={12} />
                  </button>
                  <button
                    type="button"
                    className="crud-btn danger"
                    title="Delete provider"
                    onClick={() => handleDeleteProvider(name)}
                  >
                    <Trash2 size={12} />
                  </button>
                </div>
              ))}

              {/* Inline form */}
              {editingProvider && (
                <div className="crud-inline-form">
                  <div className="form-row">
                    <label>Name</label>
                    <input
                      type="text"
                      className="styled-input"
                      value={providerName}
                      onChange={(e) => setProviderName(e.target.value)}
                      placeholder="provider-name"
                      disabled={editingProvider.mode === 'edit'}
                    />
                  </div>
                  <div className="form-row">
                    <label>API Base URL</label>
                    <input
                      type="text"
                      className="styled-input"
                      value={providerApiBase}
                      onChange={(e) => setProviderApiBase(e.target.value)}
                      placeholder="https://api.example.com/v1"
                    />
                  </div>
                  <div className="form-row">
                    <label>Default Model</label>
                    <input
                      type="text"
                      className="styled-input"
                      value={providerModelName}
                      onChange={(e) => setProviderModelName(e.target.value)}
                      placeholder="gpt-4o-mini"
                    />
                  </div>
                  <div className="form-row">
                    <label>Default Context Size (tokens)</label>
                    <input
                      type="number"
                      className="styled-input config-row-input"
                      value={providerContextSize}
                      onChange={(e) => setProviderContextSize(parseInt(e.target.value) || 32768)}
                      placeholder="32768"
                      min="0"
                    />
                  </div>
                  <div className="form-row">
                    <label>Per-Model Context Sizes (optional)</label>
                    <input
                      type="text"
                      className="styled-input"
                      value={providerModelContextSizes}
                      onChange={(e) => setProviderModelContextSizes(e.target.value)}
                      placeholder="model1:8192,model2:131072,model3:2097152"
                    />
                    <small style={{ color: '#888', fontSize: '12px', marginTop: '4px', display: 'block' }}>
                      Format: model_name:context_size, separated by commas
                    </small>
                  </div>
                  <div className="form-row">
                    <label>API Key Env Var (optional)</label>
                    <input
                      type="text"
                      className="styled-input"
                      value={providerEnvVar}
                      onChange={(e) => setProviderEnvVar(e.target.value)}
                      placeholder="OPENAI_API_KEY"
                    />
                  </div>
                  <label className="styled-toggle">
                    <input
                      type="checkbox"
                      checked={providerSupportsVision}
                      onChange={(e) => setProviderSupportsVision(e.target.checked)}
                    />
                    <span className="toggle-track" />
                    <span className="toggle-label">Supports Vision</span>
                  </label>
                  {providerSupportsVision && (
                    <div className="form-row">
                      <label>Vision Model (optional)</label>
                      <input
                        type="text"
                        className="styled-input"
                        value={providerVisionModel}
                        onChange={(e) => setProviderVisionModel(e.target.value)}
                        placeholder="Leave empty to use default model"
                      />
                    </div>
                  )}
                  <div className="form-actions">
                    <button
                      type="button"
                      className="form-btn primary"
                      onClick={editingProvider.mode === 'edit' ? handleUpdateProvider : handleAddProvider}
                    >
                      {editingProvider.mode === 'edit' ? 'Update' : 'Add'}
                    </button>
                    <button type="button" className="form-btn cancel" onClick={resetProviderForm}>
                      Cancel
                    </button>
                  </div>
                </div>
              )}

              {!editingProvider && (
                <button
                  type="button"
                  className="crud-add-btn"
                  onClick={() => {
                    setEditingProvider({ mode: 'add' });
                    setProviderName('');
                    setProviderApiBase('');
                    setProviderModelName('');
                    setProviderContextSize(32768);
                    setProviderEnvVar('');
                    setProviderSupportsVision(false);
                    setProviderVisionModel('');
                    setProviderModelContextSizes('');
                  }}
                >
                  <Plus size={14} /> Add provider
                </button>
              )}
            </div>
          </div>
        );
      }

      /* ── Skills ───────────────────────────────────────────── */
      case 'skills': {
        const skills = settings.skills || {};
        const skillEntries = Object.entries(skills);

        if (skillEntries.length === 0) {
          return <div className="settings-empty">No skills available</div>;
        }

        return (
          <div className="section">
            <h4>Skills ({skillEntries.length})</h4>
            <div className="skills-list">
              {skillEntries.map(([name, cfg]: [string, any]) => {
                const enabled = !!cfg?.enabled;
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

      default:
        return null;
    }
  };

  /* ─── Main render ───────────────────────────────────────── */

  return (
    <div className="settings-panel">
      {/* Sub-tab bar */}
      <div className="settings-subtab-bar">
        {SUB_TABS.map((tab) => (
          <button
            key={tab.id}
            type="button"
            className={`settings-subtab ${activeSubTab === tab.id ? 'active' : ''}`}
            onClick={() => setActiveSubTab(tab.id)}
          >
            {tab.label}
          </button>
        ))}
        {renderSaving()}
      </div>

      {/* Content */}
      {renderContent()}

      {/* Toasts */}
      {toasts.map((toast) => (
        <div key={toast.id} className={`settings-toast ${toast.type}`}>
          {toast.message}
        </div>
      ))}
    </div>
  );
};

export default SettingsPanel;
