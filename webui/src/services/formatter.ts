import { format } from 'prettier';
import type { BuiltInParserName } from 'prettier';

/** Supported Prettier parsers mapped from file extensions */
const EXTENSION_TO_PARSER: Partial<Record<string, BuiltInParserName>> = {
  // JS/TS
  '.js': 'babel',
  '.jsx': 'babel',
  '.mjs': 'babel',
  '.cjs': 'babel',
  '.ts': 'typescript',
  '.tsx': 'typescript',
  // CSS
  '.css': 'css',
  '.scss': 'scss',
  '.less': 'less',
  // HTML
  '.html': 'html',
  '.htm': 'html',
  '.vue': 'vue',
  // Data
  '.json': 'json',
  '.json5': 'json5',
  '.jsonc': 'jsonc',
  // Markup
  '.md': 'markdown',
  '.markdown': 'markdown',
  '.mdx': 'mdx',
  // YAML
  '.yaml': 'yaml',
  '.yml': 'yaml',
  // GraphQL
  '.graphql': 'graphql',
  '.gql': 'graphql',
};

/** Maximum file size to attempt formatting (1 MB). */
const MAX_FORMAT_SIZE = 1024 * 1024;

export interface FormatResult {
  formatted: string;
  error?: string;
}

/**
 * Format source code using Prettier.
 * Returns the formatted string, or the original string with an error message if formatting fails.
 */
export async function formatCode(
  content: string,
  filePath: string,
  fileSize?: number,
): Promise<FormatResult> {
  const ext = getExtension(filePath);
  const parser = EXTENSION_TO_PARSER[ext];

  if (!parser) {
    return { formatted: content };
  }

  // Skip formatting for very large files to avoid blocking the UI
  if (fileSize !== undefined && fileSize > MAX_FORMAT_SIZE) {
    return { formatted: content, error: 'File too large to format' };
  }

  try {
    const formatted = await format(content, {
      parser,
      semi: true,
      singleQuote: true,
      tabWidth: 2,
      trailingComma: 'all',
      printWidth: 80,
    });
    return { formatted };
  } catch (err: unknown) {
    // Prettier throws on invalid syntax — return original + error message
    const message = err instanceof Error ? err.message : String(err);
    return { formatted: content, error: message };
  }
}

/**
 * Check if a file can be formatted by Prettier based on its extension.
 */
export function isFormattable(filePath: string): boolean {
  const ext = getExtension(filePath);
  return ext in EXTENSION_TO_PARSER;
}

function getExtension(filePath: string): string {
  const lastDot = filePath.lastIndexOf('.');
  if (lastDot === -1 || lastDot === filePath.length - 1) return '';
  return filePath.slice(lastDot).toLowerCase();
}
