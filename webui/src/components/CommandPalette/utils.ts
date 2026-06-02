// ── Path helpers (exported for tests) ────────────────────────────────────

export function normalizePathSeparators(value: string): string {
  return value.replace(/\\/g, '/');
}

export function toWorkspaceRelativePath(filePath: string, workspaceRoot: string): string {
  const normalizedPath = normalizePathSeparators(filePath).replace(/^\.\//, '');
  const normalizedRoot = normalizePathSeparators(workspaceRoot).replace(/\/+$/, '');
  if (!normalizedRoot) return normalizedPath;
  if (normalizedPath === normalizedRoot) return '';
  const prefix = `${normalizedRoot}/`;
  if (normalizedPath.startsWith(prefix)) return normalizedPath.slice(prefix.length);
  return normalizedPath;
}

export function getDirectoryName(relativePath: string): string {
  const normalizedPath = normalizePathSeparators(relativePath);
  const lastSlash = normalizedPath.lastIndexOf('/');
  if (lastSlash <= 0) return '';
  return normalizedPath.slice(0, lastSlash);
}
