import { useState, useCallback, useEffect, useRef } from 'react';
import * as rolesApi from '../../services/api/rolesApi';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import type { RoleConfig } from '../../services/api/rolesApi';
import { Pencil, Plus, Trash2 } from 'lucide-react';
import { showThemedConfirm } from '../ThemedDialog';
import './RoleEditor.css';

export interface RolesSettingsTabProps {
  addNotification: (type: 'success' | 'error' | 'info', title: string, message: string, duration?: number) => string;
}

interface FormState {
  name: string;
  description: string;
  system_prompt: string;
  temperature: string;
  max_tokens: string;
  allowed_tools: string;
  persona: string;
}

const EMPTY_FORM: FormState = {
  name: '',
  description: '',
  system_prompt: '',
  temperature: '',
  max_tokens: '',
  allowed_tools: '',
  persona: '',
};

export function RolesSettingsTab({ addNotification }: RolesSettingsTabProps) {
  const fetchFn = useSproutFetch();
  const [roles, setRoles] = useState<RoleConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [formOpen, setFormOpen] = useState(false);
  const [editingRole, setEditingRole] = useState<RoleConfig | null>(null);
  const [deletingName, setDeletingName] = useState<string | null>(null);
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const initialLoadRef = useRef(false);

  const loadRoles = useCallback(async () => {
    try {
      setLoading(true);
      const data = await rolesApi.listRoles(fetchFn);
      setRoles(data);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      addNotification('error', 'Load roles failed', msg);
    } finally {
      setLoading(false);
    }
  }, [fetchFn, addNotification]);

  useEffect(() => {
    if (!initialLoadRef.current) {
      initialLoadRef.current = true;
      loadRoles();
    }
  }, [loadRoles]);

  const openCreate = () => {
    setEditingRole(null);
    setForm(EMPTY_FORM);
    setFormOpen(true);
  };

  const openEdit = (role: RoleConfig) => {
    setEditingRole(role);
    setForm({
      name: role.name,
      description: role.description ?? '',
      system_prompt: role.system_prompt ?? '',
      temperature: role.temperature?.toString() ?? '',
      max_tokens: role.max_tokens?.toString() ?? '',
      allowed_tools: role.allowed_tools?.join(', ') ?? '',
      persona: role.persona ?? '',
    });
    setFormOpen(true);
  };

  const cancelForm = () => {
    setFormOpen(false);
    setEditingRole(null);
    setForm(EMPTY_FORM);
  };

  const handleSubmit = async () => {
    if (!form.name.trim()) {
      addNotification('error', 'Validation Error', 'Name is required');
      return;
    }
    const cfg: RoleConfig = {
      name: form.name.trim(),
      description: form.description || undefined,
      system_prompt: form.system_prompt || undefined,
      temperature: form.temperature ? parseFloat(form.temperature) : undefined,
      max_tokens: form.max_tokens ? parseInt(form.max_tokens, 10) : undefined,
      allowed_tools: form.allowed_tools
        ? form.allowed_tools.split(',').map((s) => s.trim()).filter(Boolean)
        : undefined,
      persona: form.persona || undefined,
    };

    try {
      setSaving(true);
      if (editingRole) {
        await rolesApi.updateRole(fetchFn, editingRole.name, cfg);
        addNotification('success', 'Role updated', `"${cfg.name}" has been updated.`);
      } else {
        await rolesApi.createRole(fetchFn, cfg);
        addNotification('success', 'Role created', `"${cfg.name}" has been created.`);
      }
      cancelForm();
      await loadRoles();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      addNotification('error', editingRole ? 'Update failed' : 'Create failed', msg);
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (name: string) => {
    const confirmed = await showThemedConfirm(
      `Delete role "${name}"? This cannot be undone.`,
      { title: 'Delete role', type: 'danger', confirmLabel: 'Delete' },
    );
    if (!confirmed) return;
    try {
      setDeletingName(name);
      await rolesApi.deleteRole(fetchFn, name);
      addNotification('success', 'Role Deleted', `Role "${name}" has been deleted`);
      setRoles((prev) => prev.filter((r) => r.name !== name));
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      addNotification('error', 'Delete failed', msg);
    } finally {
      setDeletingName(null);
    }
  };

  const updateField = <K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((prev) => ({ ...prev, [key]: value }));
  };

  return (
    <div className="settings-body">
      {/* ── List ─────────────────────────────────────────────── */}
      <div className="crud-list">
        {loading && roles.length === 0 ? (
          <div className="section-body">Loading roles...</div>
        ) : roles.length === 0 && !formOpen ? (
          <div className="section-body">No roles yet</div>
        ) : (
          roles.map((role) => (
            <div key={role.name} className="crud-item">
              <div className="crud-item-left">
                <div className="crud-item-name">{role.name}</div>
                {role.description && <div className="crud-item-desc">{role.description}</div>}
              </div>
              <div className="crud-item-right">
                <button
                  className="crud-btn"
                  title={`Edit ${role.name}`}
                  onClick={() => openEdit(role)}
                >
                  <Pencil size={14} />
                </button>
                <button
                  className="crud-btn crud-btn-danger"
                  title={`Delete ${role.name}`}
                  onClick={() => handleDelete(role.name)}
                  disabled={deletingName === role.name}
                >
                  <Trash2 size={14} />
                </button>
              </div>
            </div>
          ))
        )}
      </div>

      {/* ── Inline form ──────────────────────────────────────── */}
      {formOpen && (
        <div className="crud-inline-form">
          <h3 className="form-title">{editingRole ? 'Edit Role' : 'New Role'}</h3>
          <div className="role-editor-form">
            <div className="form-row-inline">
              <div className="form-row form-row-inline-half">
                <label htmlFor="role-name">Name</label>
                <input
                  id="role-name"
                  className="styled-input"
                  type="text"
                  value={form.name}
                  disabled={!!editingRole}
                  onChange={(e) => updateField('name', e.target.value)}
                  placeholder="e.g. code-reviewer"
                />
              </div>
              <div className="form-row form-row-inline-half">
                <label htmlFor="role-persona">Persona</label>
                <input
                  id="role-persona"
                  className="styled-input"
                  type="text"
                  value={form.persona}
                  onChange={(e) => updateField('persona', e.target.value)}
                  placeholder="e.g. code_reviewer"
                />
              </div>
            </div>

            <div className="form-row">
              <label htmlFor="role-description">Description</label>
              <textarea
                id="role-description"
                className="styled-input"
                value={form.description}
                onChange={(e) => updateField('description', e.target.value)}
                rows={2}
                placeholder="Short description of this role"
              />
            </div>

            <div className="form-row">
              <label htmlFor="role-system-prompt">System Prompt</label>
              <textarea
                id="role-system-prompt"
                className="styled-input"
                value={form.system_prompt}
                onChange={(e) => updateField('system_prompt', e.target.value)}
                rows={3}
                placeholder="Custom system prompt for this role"
              />
            </div>

            <div className="form-row-inline">
              <div className="form-row form-row-inline-half">
                <label htmlFor="role-temperature">Temperature</label>
                <input
                  id="role-temperature"
                  className="styled-input"
                  type="number"
                  min={0}
                  max={2}
                  step={0.1}
                  value={form.temperature}
                  onChange={(e) => updateField('temperature', e.target.value)}
                  placeholder="e.g. 0.7"
                />
              </div>
              <div className="form-row form-row-inline-half">
                <label htmlFor="role-max-tokens">Max Tokens</label>
                <input
                  id="role-max-tokens"
                  className="styled-input"
                  type="number"
                  min={0}
                  value={form.max_tokens}
                  onChange={(e) => updateField('max_tokens', e.target.value)}
                  placeholder="e.g. 4096"
                />
              </div>
            </div>

            <div className="form-row">
                              <label htmlFor="role-allowed-tools">Allowed Tools</label>
                              <input
                id="role-allowed-tools"
                className="styled-input"
                type="text"
                value={form.allowed_tools}
                onChange={(e) => updateField('allowed_tools', e.target.value)}
                placeholder="e.g. file_read,shell_command,web_search"
              />
            </div>
          </div>

          <div className="form-actions">
            <button
              className="form-btn"
              onClick={handleSubmit}
              disabled={saving}
            >
              {saving ? 'Saving…' : editingRole ? 'Update' : 'Create'}
            </button>
            <button
              className="form-btn form-btn-secondary"
              onClick={cancelForm}
              disabled={saving}
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* ── Add role button ──────────────────────────────────── */}
      {!formOpen && (
        <button className="crud-btn" onClick={openCreate}>
          <Plus size={14} /> Add role
        </button>
      )}
    </div>
  );
}
