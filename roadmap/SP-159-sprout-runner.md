# SP-159 — `sprout runner`: Your Own Machine as the Browser Build's Runner

> **Status (2026-10-05):** In progress — `sprout runner` link/start/status/mode/install, container/native/bare-metal launchers, host server and the macOS/Linux sandbox are implemented; relay and the browser host picker are next.
> Platform counterpart: platform SP-BUILDER-14 (runner linking, protocol,
> security fixes), building on SP-BUILDER-12 (runner-hosted workspaces) and
> SP-BUILDER-13 (relay tunnels). Related: SP-158 (design loop in the browser
> build), SP-155a (preview pane), `docs/txn-protocol.md`.

## Problem

The browser build runs the agent in WASM. Anything it cannot run in-page
(compilers, package managers, test runners, dev servers, `git push`) exits
127 and escalates over the txn protocol to a platform-hosted container.
Users who own capable machines — a Mac with Xcode, a Linux box with a GPU —
cannot use them:

- The runner is a separate platform binary (`cmd/sprout-runner`), registered
  by hand-copying an ID and API key, and only reachable when the user exposes
  a public HTTPS URL.
- It runs everything in Docker, so native toolchains (Xcode, iOS/Android
  SDKs, macOS-only tools) are out of reach.
- Escalated runs are one-shot `sh -c` calls: no streaming output, no
  long-running processes, no preview, no conflict detection on file sync.

## Goals

1. **One install, one command to link.** `sprout runner link` pairs the
   machine with the user's account through a device code; nothing to copy,
   no network setup.
2. **Three execution modes per runner**, chosen by the machine's owner:
   `container` (default), `native` (host toolchains under the OS sandbox),
   `bare-metal` (host execution, no sandbox).
3. **Works in tandem with the browser build**: the escalation prompt can
   target the runner, output streams, dev servers run and preview, and file
   sync detects conflicts.

## Design

### 159a. Commands

```
sprout runner link [--name "MacBook Pro"]   # device-code pairing
sprout runner install                       # launchd / systemd user service
sprout runner start | stop | status
sprout runner mode container|native|bare-metal
sprout runner unlink
```

- `link` requests a device code from the platform, prints the code and the
  verification URL, opens the browser, and polls until the user approves the
  machine on the platform's Runners page (RFC 8628 shape — the platform's
  GitHub device flow is the precedent). The issued runner key is stored with
  `pkg/credentials` (OS keyring; encrypted-file fallback), never in a
  dotfile.
- `install` reuses `pkg/service` (launchd on macOS, systemd user units on
  Linux). Windows service support is new work in `pkg/service`.
- `status` shows link state, platform connectivity (relay/direct), mode and
  its sandbox capability, active workspaces, and recent runs.

### 159b. Connection

The runner keeps one outbound connection to the platform: the SP-BUILDER-13
relay tunnel (multiplexed, flow-controlled frames over a WebSocket). It
carries task dispatch, txn calls and preview traffic, so the machine needs no
inbound port, public URL or VPN. Direct mode (`RUNNER_PUBLIC_URL`) remains as
an advanced option.

The runner terminates relay streams and speaks plain HTTP to the workspace's
sprout daemon on loopback — the daemon's `/api/txn/*` contract is unchanged.
The runner is a thin process: registration, tunnel, workspace lifecycle, and
the mode-specific launcher.

### 159c. Execution modes

Every workspace gets its own clone under the runner's data directory
(`~/Library/Application Support/sprout/runner/workspaces/<id>` on macOS,
`$XDG_DATA_HOME/sprout/runner/...` on Linux). A runner never operates on the
user's own working copies.

| Mode | Isolation | What runs |
|---|---|---|
| `container` (default) | Linux container: Docker, Podman, OrbStack; Apple's container runtime on Apple silicon if it proves out. Today's profile: non-root, read-only rootfs, all caps dropped, no-new-privileges, CPU/memory limits. | The sprout workspace image. |
| `native` | Host process under the OS sandbox: macOS Seatbelt profile; Linux bubblewrap + Landlock + seccomp; Windows restricted token + job object (labeled weaker). | Host toolchains (Xcode, SDKs). |
| `bare-metal` | None. Runs as the user, with the user's environment. | Anything the user can run. |

Native sandbox policy (all platforms, to the extent each OS supports it):

- Writes allowed only to the workspace clone, a per-workspace temp dir and
  toolchain caches the user allowlists (e.g. `~/Library/Developer/Xcode/
  DerivedData`).
- Reads denied for credential stores (`~/.ssh`, `~/.aws`, `~/.config/gh`,
  keychains, browser profiles) and other users' data.
- Network: on by default (package managers need it), with an optional
  allowlist enforced through a local proxy.
- Environment: a minimal allowlisted environment; platform secrets are
  injected only for the commands that need them (git operations go through
  the runner's credential helper, not an env var).

Native mode on macOS, as built and verified with git, clang, `swift build`
and `xcodebuild` on a Swift package:

- Default writable set beyond the workspace: the per-user cache dir
  (clang module and xcrun caches), specific per-user temp entries
  (`xcrun_db*`, Foundation's `TemporaryItems/`, SwiftPM's
  `TemporaryDirectory.*`, `ResultBundle_*`) and `/private/tmp`.
- Home caches (`~/Library/Caches/org.swift.swiftpm`, Xcode DerivedData,
  `~/.npm`) stay read-only by default — writing a shared host cache from a
  workspace is a poisoning route. Users allowlist them per runner; npm gets
  a per-workspace cache via `npm_config_cache`.
- A sandboxed process cannot create a nested Seatbelt sandbox: SwiftPM
  needs `--disable-sandbox` (or xcodebuild's
  `-IDEPackageSupportDisableManifestSandbox=YES`), and Xcode projects with
  user-script sandboxing likely need `ENABLE_USER_SCRIPT_SANDBOXING=NO`.
- Not blocked: Mach-service paths (securityd for the System keychain,
  nsurlsessiond for indirect network). Denying them is a follow-up; it
  risks breaking code signing.
- Linux: deny-read paths that don't exist yet are not masked (bwrap needs
  an existing mount point); Landlock and seccomp are not implemented.

`bare-metal` is opt-in per runner and per machine owner (`sprout runner mode
bare-metal` requires an interactive confirmation on the machine itself; the
platform cannot switch a runner into it). Its mode is shown wherever the
runner is selectable — the Runners page and the browser build's escalation
prompt ("MacBook Pro · bare metal").

The runner reports its mode and detected sandbox capability with every
heartbeat; the platform stores it and the browser shows it.

### 159d. The browser build

- **Host choice.** The escalation prompt offers the user's online runners
  and the cloud: "Run on: MacBook Pro (native) · Cloud". The choice maps to
  SP-BUILDER-12's `host` (`auto | runner | fly`) plus a runner id; `Always
  allow` remembers it per repo.
- **Streaming.** `/api/txn/run` gains a streaming variant (chunked stdout/
  stderr events, then the result) carried through the relay frame-by-frame.
  The terminal and the agent's tool output show progress instead of a 10-
  minute silence. The one-shot form stays for compatibility.
- **Long-running processes.** A txn can start a process that outlives the
  `run` call (dev servers, watchers): `/api/txn/spawn` returns a handle;
  `logs`, `signal` and `list` manage it. A registered preview port is
  served through SP-BUILDER-13's `/workspaces/{id}/preview/` route into the
  SP-155a pane.
- **Conflict detection.** Push manifests carry each file's base content hash
  (the hash the browser last pulled); the daemon rejects a write whose
  on-disk hash differs, returning the conflicting paths. Pull manifests carry
  the daemon-side hash so the browser can do the same. Conflicts surface in
  the existing conflict UX (SP-140-7) instead of silently overwriting.
- **Binary-safe sync.** Pulled binary files are written byte-exact through
  the SP-158 binary VFS path, not decoded as UTF-8.

### 159e. Daemon hardening (applies to every host)

- The daemon enforces the per-workspace secret on `/api/txn/*`: the platform
  now sets `SPROUT_AUTH_TOKEN` to it (SP-BUILDER-14), and the daemon's
  existing bearer middleware does the rest.
- `pkg/txn` runs commands without the daemon's own credentials
  (`SPROUT_AUTH_TOKEN`, the txn secret). Git credentials stay: the
  credential helper reads them, and a command allowed to push can reach them
  through git regardless. LLM keys are not scrubbed here, because users set
  provider keys as their own workspace variables for their apps' tests; the
  platform instead stops injecting the agent's LLM key into txn-only
  workspaces (SP-BUILDER-14 §14d).

## Acceptance criteria

- [ ] `sprout runner link` on macOS and Linux pairs through the browser with
      no copied secrets; the key lands in the OS keyring.
- [ ] A linked Mac behind NAT, with no public URL, serves the browser build's
      escalations end to end over the relay.
- [x] `native` and `bare-metal` serve a real workspace daemon end to end
      through the host server (`TestHostLauncherEndToEnd`, gated on
      `SPROUT_RUNNER_E2E_BIN`); `native` on macOS builds a Swift package
      with `xcodebuild` (verified by hand). [ ] `container` end to end.
- [x] Native sandbox: a command writing outside the workspace or reading
      `~/.ssh` fails (`pkg/runner/sandbox` tests); bare metal is unconfined.
- [x] The platform cannot put a runner into `bare-metal`: the mode is set
      only by `sprout runner mode`, which requires typing a confirmation on
      the machine. [ ] The mode is shown in the escalation prompt.
- [ ] Streaming run output appears in the browser terminal as it is produced.
- [ ] A dev server started through `spawn` renders in the preview pane,
      including HMR.
- [ ] A push over a file changed on the runner since the last pull is
      rejected with the conflicting path; nothing is overwritten.
- [ ] Escalated commands do not see git tokens or LLM keys in their
      environment.

## Non-goals

- Sharing one runner across users or teams (single-owner runners only).
- Arbitrary TCP tunneling (databases, debuggers) — HTTP-shaped traffic only.
- Running the browser build's agent loop itself on the runner; the agent
  stays in the browser, the runner executes.

## Open questions

- Apple's container runtime: maturity and performance versus OrbStack/Docker
  on Apple silicon — measure before making it the macOS default.
- Seatbelt (`sandbox-exec`) is deprecated but still supported and widely
  used; track a replacement if Apple removes it.
- Windows `native`: AppContainer gives real isolation but breaks many
  toolchains; ship restricted token + job object first, labeled weaker.
