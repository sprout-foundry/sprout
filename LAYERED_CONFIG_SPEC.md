# Layered Configuration Architecture - Specification

## Overview

This document specifies a three-tier layered configuration system for sprout, replacing the current global-only config model. The layered approach provides:
- Session-level isolation (ephemeral, client-specific overrides)
- Workspace-level configuration (project-specific defaults)
- Global configuration (user-level defaults)

## Problem Statement

Current issues:
1. All clients share `~/.ledit/config.json` - concurrent writes cause race conditions
2. No workspace-level config support
3. No session-level isolation
4. Settings changes from one client affect all other clients

## Proposed Architecture

### Three-Tier Config Resolution Order

```
┌─────────────────────────────────────────────────────────────────────┐
│  SESSION CONFIG (Highest Priority)                                  │
│  ~/.ledit/clients/{clientID}/config.json                           │
│  - Only stores DELTA/overrides from workspace                      │
│  - Ephemeral: exists only while session is active                  │
│  - Stores: provider, model, subagent overrides                     │
└─────────────────────────────────────────────────────────────────────┘
                              ↑
                              │ (inherits from)
                              │
┌─────────────────────────────────────────────────────────────────────┐
│  WORKSPACE CONFIG (Middle Priority)                                 │
│  {workspaceRoot}/.ledit/config.json                                │
│  - Project-specific overrides                                       │
│  - Stored in the project directory                                 │
│  - Version controlled (optional)                                   │
└─────────────────────────────────────────────────────────────────────┘
                              ↑
                              │ (inherits from)
                              │
┌─────────────────────────────────────────────────────────────────────┐
│  GLOBAL CONFIG (Base/Fallback)                                     │
│  ~/.ledit/config.json                                              │
│  - User's default configuration                                    │
│  - Created on first run                                            │
└─────────────────────────────────────────────────────────────────────┘
```

### Config Loading Strategy

1. **Load global config** from `~/.ledit/config.json`
2. **Merge workspace config** from `{workspace}/.ledit/config.json` (if exists)
   - Workspace config only contains fields that differ from global
3. **Merge session config** from `~/.ledit/clients/{clientID}/config.json` (if exists)
   - Session config only contains fields that differ from workspace/global

### Config Storage Format

The session and workspace configs store **only overrides**, not full copies:

```json
// ~/.ledit/clients/worker_1/config.json (session override)
{
  "last_used_provider": "deepinfra",
  "provider_models": {
    "deepinfra": "meta-llama/Meta-Llama-3-8B-Instruct"
  }
}
```

This is equivalent to the current global config but with only the fields that were changed.

## Implementation Tasks

### Phase 1: Core Infrastructure

#### T1.1 - Create Config Layer Resolution Logic
**Files:** `pkg/configuration/config.go`, `pkg/configuration/manager.go`

**Tasks:**
- Add `ConfigLayer` type with fields: Global, Workspace, Session
- Add `LoadWithLayers(globalPath, workspacePath, sessionPath)` function
- Add `MergeConfig(base, override *Config) *Config` function that applies override fields
- Modify `Manager` to hold layer paths and resolve config from layers at load time
- Add `GetEffectiveConfig()` that returns merged config

#### T1.2 - Modify Manager Construction to Accept Layer Paths  
**Files:** `pkg/configuration/manager.go`, `pkg/agent/agent.go`

**Tasks:**
- Add `NewManagerWithLayers(globalDir, workspaceDir, sessionDir string)` constructor
- Keep existing constructors for backward compatibility (global-only)
- Update `NewManagerWithDir` to use the layered approach internally

#### T1.3 - Create Workspace Config Path Resolver
**Files:** `pkg/configuration/config.go`

**Tasks:**
- Add `GetWorkspaceConfigPath(workspaceRoot string) string` 
  - Returns `{workspaceRoot}/.ledit/config.json`
- Add `IsWorkspaceConfigPresent(workspaceRoot string) bool`

### Phase 2: Session Integration

#### T2.1 - Update WebUI Client Context for Layered Config
**Files:** `pkg/webui/client_context.go`

**Tasks:**
- Modify `getClientAgent` to compute layer paths:
  - Global: `~/.ledit/config.json`
  - Workspace: `{ctx.WorkspaceRoot}/.ledit/config.json`
  - Session: `~/.ledit/clients/{clientID}/config.json`
- Pass all three paths to `configuration.NewManagerWithLayers()`
- Create session config directory on first access (like B1 does now)

#### T2.2 - Update Settings API for Layered Config
**Files:** `pkg/webui/settings_api_general.go`, `pkg/webui/settings_api_helpers.go`

**Tasks:**
- Modify `getConfigManager` to use layered config
- Settings writes should write to SESSION layer (most specific)
- Add `GET /api/settings?layer=session|workspace|global` to read specific layer
- Add UI hint in response indicating which layer was modified

#### T2.3 - Update WebSocket Fallback Paths
**Files:** `pkg/webui/websocket.go`, `pkg/webui/settings_api_helpers.go`

**Tasks:**
- Modify fallback in `handleProviderChangeMessage` to use session config dir
- Modify fallback in `getConfigManager` to use session config dir
- Ensure session config dir is computed the same way as in T2.1

### Phase 3: Session Persistence Integration

#### T3.1 - Store Config Overrides with Session State
**Files:** `pkg/agent/persistence.go`, `pkg/agent/session_info.go`

**Tasks:**
- Modify session state to include a `ConfigOverrides` field
- This stores the session's config delta at save time
- When restoring a session, apply these overrides to the config

#### T3.2 - Auto-save Config on Session Close
**Files:** `pkg/webui/chat_sessions.go`, `pkg/webui/client_context.go`

**Tasks:**
- On session/chat close, save current config overrides to session storage
- On session restore, merge overrides with current config layers

### Phase 4: WebUI Changes

#### T4.1 - Add Settings Tabs UI
**Files:** `webui/src/components/Settings.tsx` (or similar)

**Tasks:**
- Add tab bar: "Session" | "Workspace" | "Global"
- Each tab shows config for that layer (read-only for workspace/global if in session)
- Session tab allows full editing
- Show which layer each setting comes from (effective value vs layer value)

#### T4.2 - Settings API Updates for Layer Reading
**Files:** `pkg/webui/settings_api_general.go`

**Tasks:**
- Add `layer` query param to GET/PUT endpoints
- GET `/api/settings?layer=session` returns only session overrides
- PUT with `?layer=session` writes to session layer
- Return metadata indicating which layer was read/written

### Phase 5: Cleanup and Migration

#### T5.1 - Remove B1 Full-Copy Implementation
**Files:** `pkg/agent/agent.go`, `pkg/webui/client_context.go`

**Tasks:**
- Remove `NewAgentWithConfigDir` (replaced by layered approach)
- Verify all paths use layered config
- Test that workspace config takes precedence over global

#### T5.2 - Add Migration Path for Existing Configs
**Files:** `pkg/configuration/config.go`

**Tasks:**
- If `~/.ledit/clients/{clientID}/config.json` exists from B1, migrate to new format
- Log migration for debugging

## API Changes Summary

### GET /api/settings
- Query param `?layer=session|workspace|global` (default: effective)
- Returns config from specified layer

### PUT /api/settings
- Body: `{"provider": "...", "model": "...", ...}`
- Writes to session layer by default
- Query param `?layer=session|workspace|global` to override

### GET /api/settings/effective
- Returns fully merged effective config (current behavior)

### POST /api/settings/reset-session
- Clears session overrides, falls back to workspace/global

## Migration Strategy

1. **Phase 1-3**: Implement layered logic, existing global-only calls continue working
2. **Phase 4**: Add UI, users can opt-in to session config
3. **Phase 5**: Remove old full-copy paths, clean up

## Backward Compatibility

- Global config (`~/.ledit/config.json`) remains the system of record
- Workspace config is optional - if not present, falls back to global
- Session config is optional - if not present, falls back to workspace → global
- All existing APIs work without changes (default to global or effective)

## Testing Plan

1. Unit test `MergeConfig` function
2. Integration test: Set global config → workspace override → session override → verify resolution order
3. Integration test: Session isolation - two clients with different session configs don't interfere
4. Integration test: Session restore - verify config overrides are restored with session
5. WebUI test: Verify tab switching shows correct layer values

## Open Questions (to discuss)

1. Should workspace config be auto-created when editing settings in workspace context?
2. Should we allow "promoting" session overrides to workspace/global?
3. How do we handle API key isolation - are they global or per-layer?
4. What's the max size for session config overrides?