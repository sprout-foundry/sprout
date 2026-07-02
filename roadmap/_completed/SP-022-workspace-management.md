# SP-022: Workspace Management & Project Detection

**Status:** ✅ Implemented (WorkspacePicker + WorkspacePane + LocationSwitcher + WorkspaceBar)
**Depends on:** SP-001 (Agent Core), SP-003 (Webui), SP-019 (Multi-Chat Sessions)  
**Priority:** High  
**Effort Estimate:** ~2 weeks (2 phases)

## Problem

When `sprout agent -d` starts, the workspace root is set to `os.Getwd()` — wherever the process was launched from. There is no validation, detection, or user guidance:

| Scenario | Current behavior | Problem |
|----------|-----------------|---------|
| `cd ~/projects/myapp && sprout agent -d` | workspaceRoot = `~/projects/myapp` | ✅ Correct |
| `sprout agent -d` from `~` | workspaceRoot = `~` | ❌ Agent operates on entire home directory |
| `sprout agent -d` from `/` | workspaceRoot = `/` | ❌ Agent operates on filesystem root |
| Reconnect to daemon after restart | Fresh workspace from `os.Getwd()` | ❌ Last workspace lost |
| Multiple projects, single daemon | Must use `/api/workspace` POST manually | ❌ No UI for switching |

The user opens the web UI and the agent is pointing at wherever `sprout` was launched from. If that's `~`, the agent will happily operate on the entire home directory. There is no workspace picker, no project detection, and no session-aware workspace restore.

## Current State

### What Exists

| Component | File | Capability |
|-----------|------|-----------|
| Workspace root | `server.go:79` | Set from `os.Getwd()` at server creation |
| Daemon root | `server.go:93` | Set from `os.UserHomeDir()` |
| `GET /api/workspace` | `api_workspace.go` | Returns `workspace_root` and `daemon_root` |
| `POST /api/workspace` | `api_workspace.go` | Sets new workspace root |
| `GET /api/workspace/browse` | `api_workspace.go` | Directory browser |
| Session persistence | `persistence.go` | `ConversationState.WorkingDirectory` saved per session |
| Per-chat worktree | `chat_sessions.go` | `WorktreePath` on `chatSession` |
| WelcomeTab | `WelcomeTab.tsx` | Shown when no files open, no workspace picker |
| OnboardingDialog | `OnboardingDialog.tsx` | Provider setup only |

### What's Missing

| Feature | Impact | Effort |
|---------|--------|--------|
| Project detection (git, go.mod, package.json, etc.) | High | Low |
| Workspace validation at startup | High | Low |
| Session-aware workspace restore | High | Medium |
| Recent projects list | Medium | Medium |
| Workspace picker UI | High | Medium |
| "Running from home dir" warning | High | Low |

## Proposed Solution

### Phase 1: Backend — Detection & Restore (Week 1)

#### W1.1: Project Detection

Detect whether a directory is a "project" by looking for common markers:

```go
// pkg/webui/project_detect.go

// ProjectMarker represents a file or directory that indicates a project root.
type ProjectMarker struct {
    Name    string // e.g., ".git", "go.mod", "package.json"
    Weight  int    // Higher = stronger signal (.git > go.mod > README)
    IsDir   bool
}

// IsProjectDirectory checks if a directory appears to be a project root.
func IsProjectDirectory(dir string) (bool, []string) {
    // Returns (isProject, markersFound)
    // Markers checked: .git, go.mod, package.json, Cargo.toml, pyproject.toml,
    //                   setup.py, requirements.txt, CMakeLists.txt, .vscode,
    //                   .sprout, Makefile, justfile, Gemfile
}

// FindNearestProjectRoot walks up from a directory looking for project markers.
func FindNearestProjectRoot(startDir string) (string, []string) {
    // Returns (projectRoot, markers) or ("", nil) if none found
}

// FindProjectsInDirectory scans a directory for subdirectories that look like projects.
func FindProjectsInDirectory(dir string, maxDepth int) []ProjectInfo {
    // Returns list of nearby projects with path, name, markers
}

type ProjectInfo struct {
    Path      string
    Name      string // basename of directory
    Markers   []string
    LastUsed  time.Time // from session state, if available
}
```

**Marker weights** (higher = stronger signal):
- `.git` — 100 (definitive project marker)
- `.sprout` — 90 (sprout workspace config)
- `go.mod` — 80
- `package.json` — 80
- `Cargo.toml` — 80
- `pyproject.toml` — 80
- `setup.py` — 70
- `requirements.txt` — 60
- `CMakeLists.txt` — 70
- `Makefile` — 50
- `justfile` — 50
- `Gemfile` — 70
- `README.md` — 30 (weak, many non-projects have READMEs)

#### W1.2: Startup Workspace Validation

At server startup, check whether `workspaceRoot` is a project directory. If not, scan for nearby projects and return suggestions:

```go
// pkg/webui/server.go (NewReactWebServer)

workspaceRoot, _ := os.Getwd()
workspaceRoot, _ = filepathAbsEval(workspaceRoot)

// Validate workspace
isProject, markers := IsProjectDirectory(workspaceRoot)
if !isProject {
    // Try to find nearest project root by walking up
    nearestRoot, _ := FindNearestProjectRoot(workspaceRoot)
    if nearestRoot != "" && nearestRoot != workspaceRoot {
        workspaceRoot = nearestRoot // Auto-correct to project root
    }
}
```

#### W1.3: Session-Aware Workspace Restore

When a client connects to the daemon, check for recent sessions and auto-restore the workspace from the most recent session:

```go
// pkg/webui/client_context.go (getOrCreateClientContextLocked)

// On first connection for a new client, try to restore workspace from recent sessions.
if ctx.WorkspaceRoot == ws.workspaceRoot && !isProject(ctx.WorkspaceRoot) {
    if recent := getMostRecentWorkspace(); recent != "" {
        ctx.WorkspaceRoot = recent
    }
}
```

This uses `agent.ListSessionsWithTimestamps()` to find the most recent session and extract its `WorkingDirectory`.

#### W1.4: Recent Projects Tracking

Track recently used workspaces in `~/.sprout/recent_workspaces.json`:

```go
// pkg/webui/recent_workspaces.go

type RecentWorkspace struct {
    Path        string    `json:"path"`
    Name        string    `json:"name"`
    LastUsed    time.Time `json:"last_used"`
    Markers     []string  `json:"markers,omitempty"`
    SessionCount int      `json:"session_count"`
}

// GetRecentWorkspaces returns up to 10 recently used workspaces
func GetRecentWorkspaces() []RecentWorkspace

// AddRecentWorkspace records a workspace as recently used
func AddRecentWorkspace(path string)

// GetMostRecentWorkspace returns the most recently used project directory
func GetMostRecentWorkspace() string
```

**Storage:** `~/.sprout/recent_workspaces.json` (simple JSON array, max 10 entries, LRU eviction).

#### W1.5: Enhanced `/api/workspace` Response

Add project detection info to the workspace response:

```json
{
  "daemon_root": "/home/user",
  "workspace_root": "/home/user/projects/myapp",
  "is_project": true,
  "project_markers": [".git", "go.mod"],
  "needs_workspace_selection": false,
  "suggested_projects": [],
  "recent_workspaces": [
    {
      "path": "/home/user/projects/myapp",
      "name": "myapp",
      "last_used": "2026-05-11T10:30:00Z",
      "session_count": 5
    }
  ]
}
```

When `needs_workspace_selection` is `true`, `suggested_projects` contains nearby projects the user can select.

### Phase 2: Frontend — Workspace Picker (Week 2)

#### W2.1: Workspace Picker Component

When the workspace is not a project directory, show a workspace picker in the welcome area:

```typescript
// webui/src/components/WorkspacePicker.tsx

interface WorkspacePickerProps {
  daemonRoot: string;
  currentWorkspace: string;
  suggestedProjects: ProjectSuggestion[];
  recentWorkspaces: RecentWorkspace[];
  onSelect: (path: string) => void;
  onBrowse: () => void;
}

interface ProjectSuggestion {
  path: string;
  name: string;
  markers: string[];
}

interface RecentWorkspace {
  path: string;
  name: string;
  last_used: string;
  session_count: number;
}
```

**UI layout:**
```
┌─────────────────────────────────────────────────────┐
│  📁 No project workspace detected                   │
│                                                     │
│  Current: ~ (home directory)                        │
│                                                     │
│  Recent Projects:                                   │
│  ┌─────────────────────────────────────────────┐    │
│  │ 📂 myapp              .git, go.mod    5s ago │    │
│  │ 📂 sprout-foundry      .git, go.mod  2h ago │    │
│  │ 📂 browser-ide         .git, pkg.json 1d ago │    │
│  └─────────────────────────────────────────────┘    │
│                                                     │
│  Nearby Projects:                                   │
│  ┌─────────────────────────────────────────────┐    │
│  │ 📂 ~/projects/myapp          .git, go.mod    │    │
│  │ 📂 ~/projects/sprout         .git, go.mod    │    │
│  └─────────────────────────────────────────────┘    │
│                                                     │
│  [Browse...]                                        │
└─────────────────────────────────────────────────────┘
```

#### W2.2: Auto-Select on First Connection

When the frontend loads and `needs_workspace_selection` is `true`:

1. If there's exactly one recent workspace → auto-select it
2. If there are multiple recent workspaces → show the picker
3. If there are no recent workspaces → show the picker with nearby projects

#### W2.3: Workspace Status Bar Indicator

Add a workspace indicator to the status bar showing the current workspace:

```
[~/projects/myapp]  │  main  │  UTF-8  │  Go
```

Clicking the workspace name opens the workspace picker.

#### W2.4: LocationSwitcher Integration

The existing `LocationSwitcher.tsx` (SSH, instances, file browser) already has workspace switching capability. Wire the workspace picker into it so users can switch workspaces from the sidebar.

## API Reference (Changes)

### `GET /api/workspace` (Enhanced)

**New response fields:**

| Field | Type | Description |
|-------|------|-------------|
| `is_project` | boolean | Whether workspace_root appears to be a project |
| `project_markers` | string[] | Detected project markers in workspace |
| `needs_workspace_selection` | boolean | Whether the user should select a workspace |
| `suggested_projects` | object[] | Nearby projects to suggest |
| `recent_workspaces` | object[] | Recently used workspaces |

### `POST /api/workspace` (No change)

Existing endpoint works as-is for setting workspace.

### `GET /api/workspace/projects` (New)

Returns projects found within the daemon root:

```json
{
  "projects": [
    {
      "path": "/home/user/projects/myapp",
      "name": "myapp",
      "markers": [".git", "go.mod"],
      "last_used": "2026-05-11T10:30:00Z",
      "session_count": 5
    }
  ]
}
```

## Implementation Phases

### Phase 1: Backend (Week 1)

**New files:**
- `pkg/webui/project_detect.go` — Project detection logic
- `pkg/webui/recent_workspaces.go` — Recent workspace tracking
- `pkg/webui/project_detect_test.go` — Tests

**Modified files:**
- `pkg/webui/server.go` — Startup workspace validation
- `pkg/webui/api_workspace.go` — Enhanced response, new `/api/workspace/projects` endpoint
- `pkg/webui/client_context.go` — Session-aware workspace restore

### Phase 2: Frontend (Week 2)

**New files:**
- `webui/src/components/WorkspacePicker.tsx` — Workspace selection UI
- `webui/src/components/WorkspacePicker.test.tsx` — Tests

**Modified files:**
- `webui/src/components/WelcomeTab.tsx` — Show picker when no workspace
- `webui/src/components/StatusBar.tsx` — Workspace indicator
- `webui/src/components/LocationSwitcher.tsx` — Wire workspace picker
- `webui/src/services/api/workspaceApi.ts` — New API calls
- `webui/src/services/api/types.ts` — New types

## Design Decisions

### Auto-Correct to Project Root

**Decision:** If `os.Getwd()` is not a project directory, walk up to find the nearest project root.

**Rationale:** Most users `cd` into a subdirectory of their project (e.g., `~/projects/myapp/cmd/`). Auto-correcting to the project root is the expected behavior.

**Edge case:** If the user intentionally wants to work from a subdirectory (e.g., a monorepo workspace), they can explicitly set it via the workspace picker.

### Recent Workspaces Storage

**Decision:** Store in `~/.sprout/recent_workspaces.json` (not in workspace config).

**Rationale:** Recent workspaces are user-level, not workspace-level. They should persist across workspace changes and be shared across daemon instances.

### Marker Weights

**Decision:** Use weighted markers rather than a single definitive marker.

**Rationale:** Not all projects have `.git` (e.g., extracted archives, cloud workspaces). Multiple weaker signals can combine to indicate a project.

### Session-Aware Restore

**Decision:** Restore workspace from the most recent session when the workspace is not a project.

**Rationale:** Users expect to return to where they left off. If the daemon restarts, they should see the same workspace.

**Safety:** Only auto-restore if the current workspace is not a project (i.e., the user didn't explicitly start in a valid project).

### Workspace Picker vs. LocationSwitcher

**Decision:** Workspace picker is a separate component from LocationSwitcher.

**Rationale:** LocationSwitcher handles SSH, instances, and file browsing — it's too complex for a simple "pick a project" flow. The workspace picker is lightweight and focused.

**Integration:** The picker has a "Browse..." button that opens the LocationSwitcher for manual directory selection.

## Success Criteria

| Metric | Target |
|--------|--------|
| Project detection | Correctly identifies projects with ≥95% accuracy |
| Startup validation | Warns when workspace is not a project |
| Session restore | Auto-restores workspace from recent session |
| Workspace picker | Shown when workspace is not a project |
| Recent workspaces | Tracks up to 10 recent workspaces |
| Status bar | Shows current workspace path |
| Build | `make build-all` passes |

## Files Reference

| File | Action | Phase |
|------|--------|-------|
| `pkg/webui/project_detect.go` | **New**: project detection logic | 1 |
| `pkg/webui/recent_workspaces.go` | **New**: recent workspace tracking | 1 |
| `pkg/webui/project_detect_test.go` | **New**: tests | 1 |
| `pkg/webui/server.go` | Modify: startup validation | 1 |
| `pkg/webui/api_workspace.go` | Modify: enhanced response, new endpoint | 1 |
| `pkg/webui/client_context.go` | Modify: session-aware restore | 1 |
| `webui/src/components/WorkspacePicker.tsx` | **New**: workspace selection UI | 2 |
| `webui/src/components/WorkspacePicker.test.tsx` | **New**: tests | 2 |
| `webui/src/components/WelcomeTab.tsx` | Modify: show picker | 2 |
| `webui/src/components/StatusBar.tsx` | Modify: workspace indicator | 2 |
| `webui/src/components/LocationSwitcher.tsx` | Modify: wire picker | 2 |
| `webui/src/services/api/workspaceApi.ts` | Modify: new API calls | 2 |
| `webui/src/services/api/types.ts` | Modify: new types | 2 |

## Open Questions

1. **Should workspace selection be per-client or global?** → Per-client initially (each browser tab can have its own workspace). Global selection can be added later.
2. **Should we scan the entire daemon root for projects?** → No, scan up to 2 levels deep to avoid slow startup. Let users browse manually for deeper paths.
3. **How many recent workspaces to track?** → 10 entries, LRU eviction.
4. **Should workspace changes trigger agent recreation?** → Yes, `setClientWorkspaceRoot` already clears the cached agent. This is existing behavior.
