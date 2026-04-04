export function parseFilePath(rawPath: string): { fileName: string; fileExt: string } {
  const segments = rawPath.split('/').filter(Boolean);
  const fileName = segments[segments.length - 1] || rawPath;
  const dotIndex = fileName.lastIndexOf('.');
  return {
    fileName,
    fileExt: dotIndex > 0 ? fileName.slice(dotIndex) : '',
  };
}
