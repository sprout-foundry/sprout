/**
 * Agent Git Tools — client-side tool definitions for the cloud/WASM agent.
 *
 * These tools let the WASM agent operate on repos cloned into lightning-fs
 * via the gitClient singleton. Each tool wraps a gitClient call and returns
 * a human-readable string result (the agent loop reads the returned string).
 *
 * ### Integration
 *
 * To register with an agent loop, iterate over `AGENT_GIT_TOOLS` and register
 * each definition. On a tool call from the model, dispatch to the matching
 * tool's `execute(args)` method and feed the result string back.
 *
 * ### Dependencies
 *
 * - `gitClient` singleton (gitClient.ts) — all git I/O goes through this
 * - repos must be pre-cloned at `/repos/<owner>/<name>/` in lightning-fs
 * - `git_push` / `git_pull` read the GitHub PAT from localStorage(`github_pat`)
 *
 * This module is self-contained: it imports only `gitClient`. Wiring into the
 * agent loop is a separate concern. The `/api/git/*` HTTP path serves the UI;
 * this module serves the agent.
 */

import { gitClient } from './gitClient';

/**
 * A single git tool definition with its executor.
 * The `parameters` field is a JSON Schema object describing the tool's inputs.
 * The `execute` function receives the resolved arguments and returns a
 * human-readable result string (never throws — errors are returned as text).
 */
export interface AgentGitToolDefinition {
  name: string;
  description: string;
  parameters: Record<string, unknown>;
  execute: (args: Record<string, unknown>) => Promise<string>;
}

/** Validate `owner/name` format and return the lightning-fs repo directory. */
function resolveRepoDir(repo: string): string {
  if (!repo || typeof repo !== 'string') {
    throw new Error('Invalid repo: must be a non-empty string in "owner/name" format');
  }
  const parts = repo.split('/');
  if (parts.length !== 2 || !parts[0] || !parts[1]) {
    throw new Error(`Invalid repo "${repo}": must be "owner/name" (e.g. "sprout/sprout")`);
  }
  return `/repos/${parts[0]}/${parts[1]}`;
}

/** Read the GitHub PAT from localStorage (may be null). */
function getGithubToken(): string | null {
  if (typeof localStorage === 'undefined') return null;
  return localStorage.getItem('github_pat') ?? null;
}

/** Reject path traversal and absolute paths in filepaths. */
function sanitizeFilepath(filepath: string): string {
  if (typeof filepath !== 'string' || filepath === '') {
    throw new Error('filepath must be a non-empty string');
  }
  if (filepath.includes('..') || filepath.startsWith('/')) {
    throw new Error('Invalid filepath: path traversal and absolute paths are not allowed');
  }
  return filepath;
}

// ── Tool definitions ──────────────────────────────────────────────────────

const repoParam = { type: 'string', description: 'Repository in "owner/name" format' } as const;

export const AGENT_GIT_TOOLS: AgentGitToolDefinition[] = [
  {
    name: 'git_status',
    description:
      'Get the working tree status of a cloned git repository. Shows modified, added, deleted, or untracked files.',
    parameters: { type: 'object', properties: { repo: repoParam }, required: ['repo'] },
    execute: async (args) => {
      try {
        const dir = resolveRepoDir(args.repo as string);
        const entries = await gitClient.status(dir);
        if (entries.length === 0) return 'No changes detected. Working tree is clean.';
        return (
          'Status for ' + args.repo + ':\n' + entries.map((e) => '  ' + e.type.padEnd(10) + ' ' + e.filepath).join('\n')
        );
      } catch (err) {
        return 'git_status error: ' + (err instanceof Error ? err.message : String(err));
      }
    },
  },
  {
    name: 'git_diff',
    description:
      'Show diff of working tree changes in a cloned repo. Returns unified-format patches for each changed file.',
    parameters: { type: 'object', properties: { repo: repoParam }, required: ['repo'] },
    execute: async (args) => {
      try {
        const dir = resolveRepoDir(args.repo as string);
        const results = await gitClient.diff(dir);
        if (results.length === 0) return 'No changes detected.';
        const blocks = results.map((r) => '--- ' + r.filepath + ' (' + r.type + ')\n' + r.patch);
        return 'Diff for ' + args.repo + ':\n\n' + blocks.join('\n\n');
      } catch (err) {
        return 'git_diff error: ' + (err instanceof Error ? err.message : String(err));
      }
    },
  },
  {
    name: 'git_log',
    description: 'Show commit log for a cloned repo. Returns commit SHAs, messages, authors, and dates.',
    parameters: {
      type: 'object',
      properties: { repo: repoParam, depth: { type: 'number', description: 'Max commits to return (default 50)' } },
      required: ['repo'],
    },
    execute: async (args) => {
      try {
        const dir = resolveRepoDir(args.repo as string);
        const depth = typeof args.depth === 'number' ? Math.floor(args.depth) : 50;
        const entries = await gitClient.log(dir, { depth });
        if (entries.length === 0) return 'No commits found in this repository.';
        const lines = entries.map(
          (e) =>
            '  ' +
            e.oid.substring(0, 8) +
            '  ' +
            new Date(e.commit.author.timestamp * 1000).toISOString() +
            '  ' +
            e.commit.author.name +
            ' <' +
            e.commit.author.email +
            '>\n    ' +
            e.commit.message,
        );
        return 'Log for ' + args.repo + ' (' + entries.length + ' commit(s)):\n' + lines.join('\n');
      } catch (err) {
        return 'git_log error: ' + (err instanceof Error ? err.message : String(err));
      }
    },
  },
  {
    name: 'git_branch_list',
    description: 'List all branches in a cloned repo and indicate the currently checked-out branch.',
    parameters: { type: 'object', properties: { repo: repoParam }, required: ['repo'] },
    execute: async (args) => {
      try {
        const dir = resolveRepoDir(args.repo as string);
        const branches = await gitClient.listBranches(dir);
        const current = await gitClient.currentBranch(dir);
        if (branches.length === 0) return 'No branches found.';
        const lines = branches.map((b) => (b === current ? '  * ' + b : '    ' + b));
        return (
          'Branches for ' +
          args.repo +
          (current ? ' (HEAD -> ' + current + ')' : ' (detached HEAD)') +
          ':\n' +
          lines.join('\n')
        );
      } catch (err) {
        return 'git_branch_list error: ' + (err instanceof Error ? err.message : String(err));
      }
    },
  },
  {
    name: 'git_read_file',
    description: 'Read the contents of a file from a cloned git repository.',
    parameters: {
      type: 'object',
      properties: {
        repo: repoParam,
        filepath: { type: 'string', description: 'Path to the file within the repo (e.g. "src/main.ts")' },
      },
      required: ['repo', 'filepath'],
    },
    execute: async (args) => {
      try {
        if (typeof args.filepath !== 'string') throw new Error('filepath must be a string');
        const fp = sanitizeFilepath(args.filepath);
        return await gitClient.readFile(resolveRepoDir(args.repo as string), fp);
      } catch (err) {
        return 'git_read_file error: ' + (err instanceof Error ? err.message : String(err));
      }
    },
  },
  {
    name: 'git_write_file',
    description: 'Write or create a file in a cloned git repository. Creates parent directories as needed.',
    parameters: {
      type: 'object',
      properties: {
        repo: repoParam,
        filepath: { type: 'string', description: 'Path to write within the repo' },
        content: { type: 'string', description: 'File contents to write' },
      },
      required: ['repo', 'filepath', 'content'],
    },
    execute: async (args) => {
      try {
        if (typeof args.filepath !== 'string') throw new Error('filepath must be a string');
        if (typeof args.content !== 'string') throw new Error('content must be a string');
        const fp = sanitizeFilepath(args.filepath);
        const dir = resolveRepoDir(args.repo as string);
        await gitClient.writeFile(dir, fp, args.content);
        return 'Wrote ' + fp + ' in ' + args.repo;
      } catch (err) {
        return 'git_write_file error: ' + (err instanceof Error ? err.message : String(err));
      }
    },
  },
  {
    name: 'git_list_files',
    description: 'Recursively list all files and directories in a cloned repo. Excludes .git internals.',
    parameters: { type: 'object', properties: { repo: repoParam }, required: ['repo'] },
    execute: async (args) => {
      try {
        const dir = resolveRepoDir(args.repo as string);
        const entries = await gitClient.listAllFiles(dir);
        if (entries.length === 0) return 'No files found in this repository.';
        const lines = entries.map((e) => '  ' + (e.type === 'dir' ? e.path + '/' : e.path));
        return 'Files in ' + args.repo + ' (' + entries.length + ' entries):\n' + lines.join('\n');
      } catch (err) {
        return 'git_list_files error: ' + (err instanceof Error ? err.message : String(err));
      }
    },
  },
  {
    name: 'git_add',
    description: 'Stage file(s) for commit. If filepath is omitted, stages all changes.',
    parameters: {
      type: 'object',
      properties: {
        repo: repoParam,
        filepath: { type: 'string', description: 'Optional: single file to stage. Omit to stage all.' },
      },
      required: ['repo'],
    },
    execute: async (args) => {
      try {
        const dir = resolveRepoDir(args.repo as string);
        // Normalize null/undefined to undefined → stage all changes
        const fp = typeof args.filepath === 'string' ? sanitizeFilepath(args.filepath) : undefined;
        await gitClient.add(dir, fp);
        return (fp ? 'Staged ' + fp + ' in ' : 'Staged all changes in ') + args.repo;
      } catch (err) {
        return 'git_add error: ' + (err instanceof Error ? err.message : String(err));
      }
    },
  },
  {
    name: 'git_commit',
    description: 'Create a commit with the currently staged changes. Returns the new commit SHA.',
    parameters: {
      type: 'object',
      properties: { repo: repoParam, message: { type: 'string', description: 'Commit message' } },
      required: ['repo', 'message'],
    },
    execute: async (args) => {
      try {
        if (typeof args.message !== 'string') throw new Error('message must be a string');
        const oid = await gitClient.commit(resolveRepoDir(args.repo as string), args.message);
        return 'Committed ' + oid.substring(0, 8) + ' in ' + args.repo + ': "' + args.message + '"';
      } catch (err) {
        return 'git_commit error: ' + (err instanceof Error ? err.message : String(err));
      }
    },
  },
  {
    name: 'git_push',
    description: 'Push current branch to remote. Requires a GitHub token (localStorage "github_pat").',
    parameters: {
      type: 'object',
      properties: {
        repo: repoParam,
        branch: { type: 'string', description: 'Optional: branch to push (default: current)' },
      },
      required: ['repo'],
    },
    execute: async (args) => {
      try {
        const token = getGithubToken();
        if (!token) return 'No GitHub token found. The user must authenticate first.';
        await gitClient.push(resolveRepoDir(args.repo as string), { token, branch: args.branch as string | undefined });
        return 'Pushed to ' + args.repo;
      } catch (err) {
        return 'git_push error: ' + (err instanceof Error ? err.message : String(err));
      }
    },
  },
  {
    name: 'git_pull',
    description: 'Pull latest changes from remote. Requires a GitHub token (localStorage "github_pat").',
    parameters: {
      type: 'object',
      properties: {
        repo: repoParam,
        branch: { type: 'string', description: 'Optional: branch to pull (default: current)' },
      },
      required: ['repo'],
    },
    execute: async (args) => {
      try {
        if (typeof args.branch !== 'string' && args.branch !== undefined) {
          throw new Error('branch must be a string');
        }
        const token = getGithubToken();
        if (!token) return 'No GitHub token found. The user must authenticate first.';
        await gitClient.pull(resolveRepoDir(args.repo as string), { token, branch: args.branch as string | undefined });
        return 'Pulled from ' + args.repo;
      } catch (err) {
        return 'git_pull error: ' + (err instanceof Error ? err.message : String(err));
      }
    },
  },
  {
    name: 'git_create_branch',
    description: 'Create a new branch in a cloned repo. Created from HEAD but not checked out.',
    parameters: {
      type: 'object',
      properties: { repo: repoParam, name: { type: 'string', description: 'Name of the new branch' } },
      required: ['repo', 'name'],
    },
    execute: async (args) => {
      try {
        if (typeof args.name !== 'string') throw new Error('name must be a string');
        await gitClient.branch(resolveRepoDir(args.repo as string), args.name);
        return 'Created branch "' + args.name + '" in ' + args.repo;
      } catch (err) {
        return 'git_create_branch error: ' + (err instanceof Error ? err.message : String(err));
      }
    },
  },
  {
    name: 'git_checkout',
    description: 'Checkout a branch, tag, or commit in a cloned repo. Updates the working tree.',
    parameters: {
      type: 'object',
      properties: {
        repo: repoParam,
        ref: { type: 'string', description: 'Branch name, tag, or commit SHA to checkout' },
      },
      required: ['repo', 'ref'],
    },
    execute: async (args) => {
      try {
        if (typeof args.ref !== 'string') throw new Error('ref must be a string');
        await gitClient.checkout(resolveRepoDir(args.repo as string), args.ref);
        return 'Checked out "' + args.ref + '" in ' + args.repo;
      } catch (err) {
        return 'git_checkout error: ' + (err instanceof Error ? err.message : String(err));
      }
    },
  },
  {
    name: 'git_clone',
    description: 'Clone a public GitHub/GitLab/Bitbucket/Codeberg repository into the workspace. Args: { url } where url is like https://github.com/owner/name (or owner/name shorthand). Shallow clone (depth 1, default branch). After cloning, refer to the repo as "owner/name" in other git tools and read its files under repos/owner/name/. A small repo like octocat/Hello-World is a good smoke test.',
    parameters: {
      type: 'object',
      properties: {
        url: { type: 'string', description: 'Repository URL (https://github.com/owner/name) or owner/name shorthand' },
      },
      required: ['url'],
    },
    execute: async (args) => {
      try {
        if (typeof args.url !== 'string' || !args.url.trim()) {
          throw new Error('url must be a non-empty string');
        }
        let url = args.url.trim();
        if (!/^https:\/\//.test(url)) {
          if (/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(url)) {
            url = 'https://github.com/' + url;
          } else {
            throw new Error('url must be https://… or "owner/name"');
          }
        }
        // Derive owner/name for the /repos/<owner>/<name>/ layout
        const m = url.replace(/\.git$/, '').match(/\/([A-Za-z0-9_.-]+)\/([A-Za-z0-9_.-]+)$/);
        if (!m) throw new Error('cannot parse owner/name from url');
        const repo = m[1] + '/' + m[2];
        await gitClient.clone(url, resolveRepoDir(repo), { depth: 1, singleBranch: true });
        const entries = await gitClient.listDir(resolveRepoDir(repo), '/');
        return 'Cloned ' + repo + ' (' + entries.length + ' top-level entries). Use repo "' + repo + '" with the other git tools.';
      } catch (err) {
        return 'git_clone error: ' + (err instanceof Error ? err.message : String(err));
      }
    },
  },
  {
    name: 'git_list_repos',
    description: 'List repositories previously cloned into the workspace via git_clone.',
    parameters: { type: 'object', properties: {}, required: [] },
    execute: async (args) => {
      try {
        const dirs = await gitClient.listDir('/repos', '/');
        const repos = dirs.filter((d) => d.type === 'dir').map((d) => d.name);
        return repos.length ? repos.join('\n') : 'No repos cloned yet. Use git_clone first.';
      } catch (err) {
        return 'git_list_repos error: ' + (err instanceof Error ? err.message : String(err));
      }
    },
  },
];

/** Set of all registered tool names for quick lookup. */
export const AGENT_GIT_TOOL_NAMES = new Set(AGENT_GIT_TOOLS.map((t) => t.name));
