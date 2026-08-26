# ETH-2 Transactional Escalation Protocol

**Status: PINNED CONTRACT.** This page pins the four JSON shapes of the
ETH-2 transactional-escalation surface. The platform (sprout-foundry) and
the webui build parsers against them; the sprout daemon and CLI produce
them. Field names, nesting and zero-values must not change without
coordinating across repos.

The model: the browser keeps a **WASM editing plane**; the workspace
**container is the executor**. The platform drives a three-phase
transaction against the sprout daemon running inside the container:

```
1. POST /api/txn/push    browser → container   apply file deltas
2. POST /api/txn/run     container executes    run a command
3. POST /api/txn/pull    container → browser   pull the resulting deltas
```

`GET /api/txn/status` is the read-only preflight available before, between
and after the phases. The same four operations exist as CLI commands
(`sprout txn-status`, `sprout txn-push`, `sprout txn-pull`) that print the
identical JSON, so the platform can shell into a container instead of
talking to the daemon.

Implementation: `pkg/txn` (core, dependency-free), `pkg/webui/api_txn.go`
(routes), `cmd/txn.go` (CLI).

---

## Transport

| Route              | Method    | Body (request) | Body (response)     | Mutates |
|--------------------|-----------|----------------|---------------------|---------|
| `/api/txn/push`    | `POST`    | shape 1        | Apply result        | yes     |
| `/api/txn/run`     | `POST`    | shape 2 (req)  | shape 2 (resp)      | yes     |
| `/api/txn/pull`    | `POST`    | none           | shape 1             | no      |
| `/api/txn/status`  | `GET`/`HEAD` | none        | shape 3             | no      |

- The method **is** the security boundary. `GET`/`HEAD` is read-only and
  reachable unauthenticated. The three `POST`s sit behind the Bearer
  boundary whenever `SPROUT_AUTH_TOKEN` is configured (`Authorization:
  Bearer <token>`); with no token configured (localhost/dev) they are open.
- A wrong method is a `405`. A body that is not the contract at all is a
  `400`; a push body over 100 MiB is a `413`.
- **Every reportable state is a `200`** — a partial apply, a failed
  command, a timeout, a not-a-repo directory. `500` is reserved for
  catastrophic failure (unreadable workspace root, broken git).
- The push body is capped at 100 MiB by `http.MaxBytesReader`.
- Unknown JSON fields are ignored (forward compatibility); a second JSON
  document in one body is rejected.
- Mutating phases run under `context.WithoutCancel`, so a client hangup
  cannot abort a half-applied delta or SIGKILL a running command. The run
  timeout is the only canceller of a command.

### Workdir

Every route resolves the workspace root exactly as `/api/sync` does (the
per-client workspace root, symlinks evaluated). The optional `workdir`
field of a run request is **repo-relative** and confined to that root: an
absolute value, or one containing `..`, is a `400`. It is never a way to
reach outside the workspace.

---

## Shape 1 — Delta manifest

The push request body and the pull response body.

```json
{
  "base": {"git_sha": "", "client": "wasm"},
  "files": [
    {"path": "src/main.go", "content_base64": "...", "size": 123, "mode": "0644"}
  ],
  "deletes": ["old.go"],
  "truncated": false,
  "skipped": [{"path": "x.bin", "reason": "exceeds_per_file_cap"}]
}
```

| Field        | Type             | Notes                                            |
|--------------|------------------|--------------------------------------------------|
| `base`       | object           | What the manifest was computed against.          |
| `base.git_sha` | string         | HEAD at computation time (`""` when unborn/unknown). |
| `base.client` | string          | `"wasm"` on a push, `"container"` on a pull.     |
| `files`      | array            | Always an array, never `null`.                   |
| `files[].path` | string         | Repo-relative, forward slashes.                  |
| `files[].content_base64` | string | Standard base64 of the file bytes.       |
| `files[].size` | integer        | Decoded byte count. Advisory on push (the container decodes and measures for itself); authoritative on pull. |
| `files[].mode` | string         | Octal permission string, e.g. `"0644"`. Optional; defaults to `0644`. |
| `deletes`    | array of string  | Repo-relative paths to remove.                   |
| `truncated`  | boolean          | Pull only: `true` iff any entry was skipped.     |
| `skipped`    | array            | Entries not transferred, with the reason.        |

### Path rules

All paths are repo-relative, forward-slash separated. A path is **skipped**
(never a whole-request error) when it is:

- empty or whitespace-only,
- absolute (leading `/`, a Windows drive letter, or a leading `\\`),
- containing any `..` segment,
- `.git`, or containing a `.git` segment anywhere,
- containing a NUL byte,
- containing an empty (`a//b`, trailing `/`) or `.` segment.

### Caps

| Cap             | Value   | Skip reason                  |
|-----------------|---------|------------------------------|
| per file        | 5 MiB decoded | `exceeds_per_file_cap`  |
| files per manifest | 2000 | `exceeds_file_count_cap`     |
| total decoded   | 100 MiB | `exceeds_total_cap`          |

Entries beyond the 2000th are skipped with `exceeds_file_count_cap`. The
per-file cap is measured on **decoded** bytes; a file of exactly 5 MiB is
allowed.

Base64 that does not decode (standard alphabet) is skipped with
`invalid_base64`. An unparsable `mode` is skipped with `invalid_mode` rather
than guessed. A `0o`/`0O` prefix on the mode is accepted.

### Symlink containment

After joining a path onto the workspace root, the nearest **existing**
ancestor is resolved with `filepath.EvalSymlinks` and must remain inside the
root; a missing final file is fine (its parent is what must be real). A
final component that already exists as a symlink pointing outside the root
is likewise refused. Violations are skipped with `symlink_escape`. A symlink
that stays inside the root is followed normally.

### Push semantics (apply)

Parent directories are created as needed with mode `0755`; file contents are
written with the requested mode (`0644` default). The mode is re-applied
(`chmod`) on existing files, so push and pull converge instead of drifting on
a pre-existing file's permissions. Deletes are processed **after** writes.

Response:

```json
{"applied": 3, "deleted": 1, "skipped": [...], "status": "ok"}
```

`status` is `"ok"` or `"partial"` — `partial` iff anything was skipped. A
delete target that does not exist is a **no-op**: the requested end state
(path absent) already holds, so it is neither counted in `deleted` nor added
to `skipped`. A delete that fails for another reason (e.g. the target is a
non-empty directory) is skipped with `delete_failed`.

### Pull semantics (build)

Computed from the working tree with `git status --porcelain -z -uall
--untracked-files=all` (paths are repo-root relative even when the daemon's
cwd is a subdirectory):

- **dirty tracked** files (modified, staged, renamed-to, copied-to) →
  `files`, content base64-encoded;
- **deleted tracked** files → `deletes`;
- **untracked** files → `files`.

A rename appears once, under its **new** path; the consumed original path is
never reported as a delete. Pull honors the same caps by omission — an
over-cap entry goes to `skipped` with its reason and sets `truncated`.

Pull **never touches the working tree**: no `add`, no `stash`, no `reset`,
so it is safe to run after a failed command. Symlinks are **not** followed
(`symlink`), and non-regular files are skipped (`not_a_file`) — reading
through a symlink would exfiltrate a file outside the workspace. An
unreadable file is skipped with `read_failed`.

`base.client` is `"container"` and `base.git_sha` is the current HEAD.

### Skip reasons

`exceeds_per_file_cap`, `exceeds_file_count_cap`, `exceeds_total_cap`,
`absolute_path`, `path_traversal`, `git_path`, `empty_path`,
`invalid_path`, `nul_in_path`, `invalid_base64`, `invalid_mode`,
`symlink_escape`, `write_failed`, `delete_failed`, `not_a_file`,
`symlink`, `read_failed`.

(`delete_missing` is defined for symmetry but never emitted — a missing
delete target is a no-op, not a skip.)

---

## Shape 2 — Run

Request:

```json
{"command": "go build ./...", "timeout_seconds": 600, "workdir": ""}
```

Response:

```json
{"stdout": "...", "stderr": "...", "exit_code": 0, "duration_ms": 1234, "timed_out": false, "truncated": false}
```

- The command executes via `/bin/sh -c` with the workspace root (or the
  confined `workdir`) as cwd, inheriting the environment. `SHELL` is
  honored only when it is an absolute path.
- `timeout_seconds` defaults to 600 when zero/absent and is hard-capped at
  900.
- On timeout the whole **process group** is SIGKILLed (`Setpgid: true`,
  kill `-pgid`), so compiler children die with the shell. The response is
  `exit_code: 124`, `timed_out: true`.
- `stdout` and `stderr` are captured separately, each keeping the **last
  256 KiB**; `truncated` is `true` if either stream was capped.
- `exit_code` is the process's exit status; `-1` when the process died to a
  signal. A command `sh` cannot find reports its own `127`.
- A failure to even start (missing workdir, exec failure) reports
  `exit_code: 126` with the error text in `stderr` — still a `200`, since
  there is nothing else to report.
- Every outcome is a `200`. The only `400` is an unusable `workdir`.

---

## Shape 3 — Status

`GET /api/txn/status` and `sprout txn-status`. Strictly read-only: one
`git status` and one branch probe, no fetch, no write.

```json
{
  "in_git_repo": true,
  "branch": "main",
  "dirty_files": ["a.go"],
  "untracked_files": ["b.out"],
  "deleted_files": ["c.go"],
  "total_changes": 3,
  "timestamp": "2026-08-26T07:10:40Z"
}
```

- The three lists are disjoint and always arrays (never `null`).
  `total_changes` is their sum.
- `deleted_files` are tracked files removed from the tree; `dirty_files`
  are tracked files that differ from HEAD in any other way (modified,
  staged, renamed-to, copied-to). Untracked files are in
  `untracked_files`, expanded file-by-file (`--untracked-files=all`).
- `branch` is `""` for a detached HEAD that cannot be named, and `""` when
  not in a repo.
- `timestamp` is RFC3339 UTC and is always set.
- Not being a git repository is a reportable state: `in_git_repo: false`,
  `branch: ""`, empty lists, `total_changes: 0` — still a `200`.

---

## CLI mirror

```
sprout txn-status [--dir DIR]                       # prints shape 3
sprout txn-push   [--dir DIR] [--in PATH|-]         # reads shape 1, prints the apply result
sprout txn-pull   [--dir DIR] [--out PATH|-]        # prints shape 1
```

`--dir` defaults to the process working directory. `--in` defaults to `-`
(stdin); `--out` defaults to `-` (stdout).

stdout is exactly one JSON object of the corresponding shape, so it can be
piped straight into a parser; human-readable logging goes to stderr only.
Exit code is `0` for every reportable state — including a `partial` apply,
a truncated pull and a not-a-repo directory — and `1` only for a usage or
IO error, in which case `{"error":"..."}` is printed to stdout instead.
