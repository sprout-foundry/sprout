# Product Backlog

This backlog is the current productization plan for `ledit` as a desktop-first application that still preserves the existing backend-driven architecture.

The goal is not to rewrite working subsystems. The goal is to close the gap between "works for development" and "ships as a dependable product".

## Priority Model

- `P0`: blocks reliable product use or release readiness
- `P1`: high-value product work that should follow immediately after `P0`
- `P2`: important polish and scale work
- `P3`: opportunistic improvements

## P0: Release-Critical

### 1. Desktop Validation Matrix

Status: `not started`

Scope:
- Validate packaged desktop startup on `windows`, `macOS`, and `linux`
- Validate multi-window behavior
- Validate workspace restore behavior
- Validate packaged backend launch and health check behavior

Acceptance criteria:
- Each supported platform has a documented smoke test
- Packaged app can open a folder and load the UI
- Restored windows reopen successfully after relaunch
- Failures are surfaced with actionable errors instead of silent startup failure

### 2. Windows + WSL End-to-End Validation

Status: `partially implemented`

Scope:
- Validate the new WSL launcher mode on a real Windows host
- Verify distro discovery, backend staging, backend launch, health check, and workspace load
- Verify Linux path handling in recent items and restored windows
- Verify WSL terminal behavior and Git behavior

Acceptance criteria:
- WSL-backed workspace launches successfully from a packaged Windows app
- Existing WSL-backed workspace can be reopened from Recent
- WSL-backed window restore works after relaunch
- Known limitations are documented if any remain

### 3. Signing, Notarization, and Installer Readiness

Status: `partially implemented`

Scope:
- Complete Windows code signing
- Complete macOS signing and notarization validation
- Add proper macOS `.icns`
- Confirm Linux installer output quality

Acceptance criteria:
- Release CI produces signed Windows installers
- Release CI produces notarized macOS artifacts
- Installer metadata and icons are correct on each platform
- Desktop release documentation matches the real release flow

### 4. Crash and Diagnostics Baseline

Status: `not started`

Scope:
- Capture renderer crash, backend startup failure, and unexpected process exit
- Add a diagnostic bundle flow for logs and environment details
- Make desktop startup failures visible in-app

Acceptance criteria:
- Desktop app shows a clear failure screen when backend launch fails
- Users can export a diagnostics snapshot
- Crash/failure logs are written to a stable app data location

## P1: Product-Quality UX

### 5. First-Run Onboarding

Status: `not started`

Scope:
- Provider/model selection
- API credential setup
- Initial workspace selection
- Optional WSL selection on Windows

Acceptance criteria:
- First-run path can get a new user from launch to usable chat/editor flow
- Missing credentials or setup are explained in-product
- Onboarding state is resumable

### 6. Workspace Model UX Cleanup

Status: `partially implemented`

Scope:
- Make daemon root, active workspace, and instance meaning visible in the UI
- Show the current workspace more consistently across sidebar, files, git, and terminal
- Clarify when actions operate on daemon root vs workspace root

Acceptance criteria:
- Users can tell where commands and file operations will run
- Workspace switching is easy to discover
- Folder selection works consistently in desktop and non-desktop flows

### 7. Error Handling and Recovery UX

Status: `not started`

Scope:
- Replace raw fetch or process errors with structured user-facing errors
- Add retry affordances where appropriate
- Add non-destructive recovery flows for common failures

Acceptance criteria:
- Common failures have clear titles, causes, and next actions
- Failed operations do not leave the UI in confusing intermediate state
- Backend disconnection is recoverable without app restart where feasible

### 8. Session Management as a Product Feature

Status: `partially implemented`

Scope:
- Better session naming
- Session search/filter
- Session export/import
- Clearer restore flows

Acceptance criteria:
- Session history is usable beyond raw restore
- Users can find and reopen previous work intentionally
- Exported sessions are readable and portable

## P1: Test and Quality Gates

### 9. Desktop E2E Coverage

Status: `not started`

Scope:
- Add automated desktop test coverage for:
  - launcher open flow
  - workspace restore
  - file open/edit/save
  - git refresh and simple git actions
  - logs and diagnostics surfaces

Acceptance criteria:
- A desktop regression suite exists and runs in CI where feasible
- Critical desktop paths are covered before release

### 10. Workspace Switching Coverage

Status: `partially implemented`

Scope:
- Extend backend tests around daemon root and workspace root behavior
- Add frontend coverage for non-Electron workspace switching UI
- Verify terminal reset semantics after workspace switch

Acceptance criteria:
- Workspace switch API behavior is covered for valid and invalid paths
- UI behavior after switching is tested
- No stale terminal/file state remains after switch

## P2: Performance and Scale

### 11. Large Repository Performance

Status: `not started`

Scope:
- File tree performance
- Search performance
- Git refresh and diff loading
- Log rendering performance

Acceptance criteria:
- Large repo behavior is benchmarked
- Expensive sidebar refreshes are reduced
- Main user interactions stay responsive on large workspaces

### 12. State Persistence and Migration

Status: `not started`

Scope:
- Version desktop state schema
- Add migration handling for persisted settings and state
- Harden restore behavior when old data is malformed

Acceptance criteria:
- Persisted state can evolve without breaking launch
- Corrupt or partial state does not brick the desktop app

## P2: Native Product Features

### 13. Native OS Integration

Status: `partially implemented`

Scope:
- Reveal in Finder/Explorer
- Open from file manager
- Better recent items integration
- Better protocol/deep-link handling

Acceptance criteria:
- OS-level open flows work predictably
- Reveal/open external path flows are supported where appropriate

### 14. Auto-Update

Status: `not started`

Scope:
- Wire update publishing and channel strategy
- Add in-app update notification and restart flow

Acceptance criteria:
- Product can deliver updates without requiring manual reinstall

## P3: Commercial/Product Operations

### 15. Release Operations

Status: `partially implemented`

Scope:
- Release checklist
- Versioning discipline
- Artifact verification and publication checklist
- Human-readable release notes workflow

Acceptance criteria:
- Release process is documented and repeatable
- Every release artifact can be traced to a validated build

### 16. Product Analytics and Feedback Loop

Status: `not started`

Scope:
- Define whether to collect product telemetry
- If yes, add privacy-conscious event collection for key product flows
- Add opt-in and documentation

Acceptance criteria:
- Product decisions can be informed by real usage data
- Telemetry is explicit and documented

## Suggested Execution Order

### Phase 1
- Desktop validation matrix
- Windows + WSL end-to-end validation
- Signing/notarization/installer readiness
- Crash and diagnostics baseline

### Phase 2
- First-run onboarding
- Workspace model UX cleanup
- Error handling and recovery UX
- Workspace switching coverage

### Phase 3
- Desktop E2E coverage
- Session management improvements
- Native OS integration
- Large repository performance

### Phase 4
- Auto-update
- State migration hardening
- Release operations
- Product analytics and feedback loop

## Immediate Next Sprint

If work resumes immediately, the highest-value next sprint is:

1. Validate packaged Windows + WSL mode on a real Windows machine.
2. Add desktop startup failure UI and diagnostics export.
3. Finish signing/notarization assets and CI validation.
4. Add a first-run onboarding flow for provider credentials and workspace selection.
