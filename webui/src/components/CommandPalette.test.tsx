import { getDirectoryName, toWorkspaceRelativePath } from './CommandPalette';

describe('CommandPalette path formatting', () => {
  it('collapses absolute file paths to be relative to the workspace root', () => {
    expect(
      toWorkspaceRelativePath('/workspace/project/src/components/deep/ReallyImportantFile.tsx', '/workspace/project'),
    ).toBe('src/components/deep/ReallyImportantFile.tsx');
  });

  it('extracts only the parent directory so the file name can be shown separately', () => {
    expect(getDirectoryName('src/components/deep/ReallyImportantFile.tsx')).toBe('src/components/deep');
  });

  it('normalizes leading relative markers when no workspace root is available', () => {
    expect(toWorkspaceRelativePath('./src/components/ReallyImportantFile.tsx', '')).toBe(
      'src/components/ReallyImportantFile.tsx',
    );
  });

  it('returns the raw path when workspace root does not match', () => {
    expect(toWorkspaceRelativePath('/other/path/file.ts', '/workspace/project')).toBe('/other/path/file.ts');
  });

  it('returns empty string when path equals workspace root', () => {
    expect(toWorkspaceRelativePath('/workspace/project', '/workspace/project')).toBe('');
  });

  it('normalizes backslashes to forward slashes', () => {
    expect(toWorkspaceRelativePath('src\\components\\file.ts', '')).toBe('src/components/file.ts');
  });
});
