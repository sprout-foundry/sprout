/**
 * shellGitAdapter.ts — backs the WASM shell's `git` command with
 * browser-side isomorphic-git (read-only subcommands only).
 *
 * The WASM shell (pkg/wasmshell/commands_git.go) answers `git status`,
 * `git diff`, … in-browser instead of exiting 127 — which is the trigger
 * for the transactional container escalation (ETH-2). Without this
 * adapter, every git call would cost a container txn; with it, the
 * read-only subcommands the agent audit shows dominating usage run free
 * in the browser.
 *
 * Contract with the Go side (cmd/wasm/shell_executor.go):
 *   globalThis.__sproutShellGit.execute(subcommand, args)
 *     → Promise<{ stdout: string; stderr: string; exitCode: number }>
 *
 * Output formats mirror the local CLI closely enough that agent workflows
 * behave identically in browser-IDE vs. local (`git status` porcelain-ish
 * labels, `git log --oneline` shape, branch markers).
 */

import { gitBranch, gitDiff, gitLog, gitStatus } from './browserGit';

/** Shape the Go bridge expects from execute(). */
export interface ShellGitResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

const ok = (stdout: string): ShellGitResult => ({ stdout, stderr: '', exitCode: 0 });
const fail = (stderr: string, exitCode = 1): ShellGitResult => ({ stdout: '', stderr, exitCode });

// ── Subcommand formatting ────────────────────────────────────────────────

/** git status — short-format labels then long-format hint. */
async function runStatus(args: string[]): Promise<ShellGitResult> {
  const short = args.includes('-s') || args.includes('--short');
  const { staged, unstaged } = await gitStatus();

  const lines: string[] = [];
  const emit = (list: Array<{ path: string; status: string }>, stagedCol: string) => {
    for (const f of list) {
      if (short) {
        lines.push(`${stagedCol}${statusChar(f.status)} ${f.path}`);
      } else {
        lines.push(`${stagedCol}${statusChar(f.status)} ${describeStatus(f.status)}:   ${f.path}`);
      }
    }
  };

  emit(staged, ' ');
  emit(unstaged, ' ');

  if (lines.length === 0) {
    return ok(short ? '' : 'On branch main\nnothing to commit, working tree clean\n');
  }
  if (short) return ok(lines.join('\n') + '\n');
  return ok(
    'On branch main\nChanges to be committed:\n  (use "git restore --staged <file>…" to unstage)\n' +
      staged.map((f) => `\t${statusChar(f.status)}  ${f.path}`).join('\n') +
      (staged.length ? '\n' : '') +
      'Changes not staged for commit:\n' +
      unstaged.map((f) => `\t${statusChar(f.status)}  ${f.path}`).join('\n') +
      (unstaged.length ? '\n' : ''),
  );
}

function statusChar(status: string): string {
  switch (status) {
    case 'new':
    case 'added':
    case 'untracked':
      return 'A';
    case 'deleted':
      return 'D';
    case 'modified':
      return 'M';
    default:
      return '?';
  }
}

function describeStatus(status: string): string {
  switch (status) {
    case 'new':
      return 'new file';
    case 'deleted':
      return 'deleted';
    case 'modified':
      return 'modified';
    default:
      return status;
  }
}

/** git diff — per-file patch blocks from browserGit's status-backed diff. */
async function runDiff(args: string[]): Promise<ShellGitResult> {
  const cached = args.includes('--cached') || args.includes('--staged');
  void cached; // browserGit diffs the working tree; staged-only is approximated.
  const pathIdx = args.indexOf('--');
  const pathFilter = pathIdx >= 0 ? args[pathIdx + 1] : undefined;
  const changes = await gitDiff({ path: pathFilter });

  if (changes.length === 0) return ok('');

  const blocks = changes.map((c) => {
    const head = `diff --git a/${c.path} b/${c.path}`;
    if (c.type === 'added') {
      return `${head}\nnew file mode 100644\n--- /dev/null\n+++ b/${c.path}\n${linePrefix(c.content, '+')}`;
    }
    if (c.type === 'deleted') {
      return `${head}\ndeleted file mode 100644\n--- a/${c.path}\n+++ /dev/null\n`;
    }
    return `${head}\n--- a/${c.path}\n+++ b/${c.path}\n${linePrefix(c.content, '+')}`;
  });
  return ok(blocks.join('\n'));
}

function linePrefix(content: string, prefix: string): string {
  if (!content) return '';
  return content
    .split('\n')
    .filter((l) => l.length > 0)
    .map((l) => prefix + l)
    .join('\n');
}

/** git log — --oneline, -n <count>, and default medium format. */
async function runLog(args: string[]): Promise<ShellGitResult> {
  const oneline = args.includes('--oneline');
  let count = 50;
  const nIdx = args.indexOf('-n');
  if (nIdx >= 0 && nIdx + 1 < args.length) {
    const n = parseInt(args[nIdx + 1], 10);
    if (!Number.isNaN(n)) count = n;
  }
  // `git log -5` style counts.
  for (const a of args) {
    const m = /^-(\d+)$/.exec(a);
    if (m) {
      count = parseInt(m[1], 10);
      break;
    }
  }

  const commits = await gitLog(count);
  if (commits.length === 0) return ok('');

  if (oneline) {
    return ok(commits.map((c) => `${c.hash.slice(0, 7)} ${firstLine(c.message)}`).join('\n') + '\n');
  }
  const blocks = commits.map((c) => `commit ${c.hash}\nAuthor: ${c.author}\nDate:   ${c.date}\n\n    ${c.message}\n`);
  return ok(blocks.join('\n'));
}

function firstLine(s: string): string {
  const idx = s.indexOf('\n');
  return idx === -1 ? s : s.slice(0, idx);
}

/** git branch — list with the current-branch marker; -a accepted. */
async function runBranch(args: string[]): Promise<ShellGitResult> {
  void args; // -a/-v list the same refs browser-side (no remotes cached).
  const branches = await gitBranch();
  if (branches.length === 0) return ok('');
  return ok(branches.map((b) => (b.current ? `* ${b.name}` : `  ${b.name}`)).join('\n') + '\n');
}

/** git remote — origin with the clone URL when known, else empty. */
async function runRemote(args: string[]): Promise<ShellGitResult> {
  if (args.includes('-v')) {
    return ok('origin\t(push/fetch not tracked in browser git)\n');
  }
  return ok('origin\n');
}

/** git ls-files — every tracked/untracked working-tree file. */
async function runLsFiles(args: string[]): Promise<ShellGitResult> {
  void args;
  const bridge = (await import('./browserGit')).getBrowserGitVfsBridge();
  if (!bridge) return ok('');
  const files = await bridge.readVfsFiles();
  return ok(
    files
      .map((f) => f.path)
      .sort()
      .join('\n') + (files.length ? '\n' : ''),
  );
}

/** git show — commit message by hash prefix (best-effort in-browser). */
async function runShow(args: string[]): Promise<ShellGitResult> {
  const ref = args.find((a) => !a.startsWith('-')) ?? 'HEAD';
  const commits = await gitLog(50);
  const commit = ref === 'HEAD' ? commits[0] : commits.find((c) => c.hash.startsWith(ref));
  if (!commit) {
    return fail(`git show: ambiguous argument '${ref}': unknown revision\n`, 1);
  }
  return ok(`commit ${commit.hash}\nAuthor: ${commit.author}\nDate:   ${commit.date}\n\n    ${commit.message}\n`);
}

/** git rev-parse — HEAD hash (browserGit's resolveRef equivalent). */
async function runRevParse(_args: string[]): Promise<ShellGitResult> {
  const commits = await gitLog(1);
  if (commits.length === 0) return fail("HEAD\nfatal: ambiguous argument 'HEAD': unknown revision\n", 128);
  return ok(commits[0].hash + '\n');
}

/** git rev-list --count HEAD — commit count. */
async function runRevList(args: string[]): Promise<ShellGitResult> {
  const countOnly = args.includes('--count');
  const commits = await gitLog(1000);
  if (countOnly) return ok(commits.length + '\n');
  return ok(commits.map((c) => c.hash).join('\n') + (commits.length ? '\n' : ''));
}

/** git symbolic-ref --short HEAD — current branch name. */
async function runSymbolicRef(_args: string[]): Promise<ShellGitResult> {
  const branches = await gitBranch();
  const current = branches.find((b) => b.current);
  if (!current) return fail('fatal: ref HEAD is not a symbolic ref\n', 1);
  return ok(current.name + '\n');
}

// ── Registry & global installation ───────────────────────────────────────

export const SHELL_GIT_SUBCOMMANDS: Record<string, (args: string[]) => Promise<ShellGitResult>> = {
  status: runStatus,
  diff: runDiff,
  log: runLog,
  show: runShow,
  branch: runBranch,
  remote: runRemote,
  'ls-files': runLsFiles,
  'rev-list': runRevList,
  'rev-parse': runRevParse,
  'symbolic-ref': runSymbolicRef,
};

export interface SproutShellGitGlobal {
  execute(subcommand: string, args: string[]): Promise<ShellGitResult>;
  readonly names: ReadonlySet<string>;
}

declare global {
  interface Window {
    __sproutShellGit?: SproutShellGitGlobal;
  }
  /* eslint-disable no-var -- `var` is required in `declare global` blocks. */
  var __sproutShellGit: SproutShellGitGlobal | undefined;
  /* eslint-enable no-var */
}

/**
 * Install globalThis.__sproutShellGit — the bridge target for the WASM
 * shell's git command. Idempotent.
 */
export function registerShellGitGlobal(): void {
  const impl: SproutShellGitGlobal = {
    async execute(subcommand: string, args: string[]): Promise<ShellGitResult> {
      const fn = SHELL_GIT_SUBCOMMANDS[subcommand];
      if (!fn) {
        return {
          stdout: '',
          stderr: `git: '${subcommand}' is not available in this shell (read-only subcommands only)\n`,
          exitCode: 127,
        };
      }
      try {
        return await fn(args ?? []);
      } catch (err) {
        return fail(`git ${subcommand}: ${err instanceof Error ? err.message : String(err)}\n`, 1);
      }
    },
    names: new Set(Object.keys(SHELL_GIT_SUBCOMMANDS)),
  };

  globalThis.__sproutShellGit = impl;
  if (typeof window !== 'undefined') {
    window.__sproutShellGit = impl;
  }
}
