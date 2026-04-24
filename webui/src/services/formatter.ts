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

/** Default Prettier options */
const DEFAULT_OPTIONS = {
  semi: true,
  singleQuote: true,
  tabWidth: 2,
  trailingComma: 'all' as const,
  printWidth: 80,
};

/** Cached Prettier config by directory */
const configCache = new Map<string, Record<string, unknown>>();

/** Prettier config fetcher function - set by the consumer (formatter.ts doesn't directly import api) */
let configFetcher: ((filePath: string) => Promise<Record<string, unknown>>) | null = null;

/**
 * Set a custom config fetcher function.
 * This allows the formatter to fetch config from the API without direct coupling.
 */
export function setConfigFetcher(fetcher: (filePath: string) => Promise<Record<string, unknown>>): void {
  configFetcher = fetcher;
}

/**
 * Get the directory for caching config.
 * Uses the parent directory of the file path.
 */
function getConfigDirectory(filePath: string): string {
  // Get the directory part of the file path for caching
  const lastSlash = Math.max(filePath.lastIndexOf('/'), filePath.lastIndexOf('\\'));
  if (lastSlash > 0) {
    return filePath.slice(0, lastSlash);
  }
  // If no directory, use the file's directory (current dir)
  return '.';
}

/**
 * Fetch and cache Prettier config for a file's directory.
 * Only fetches once per directory and caches the result.
 */
async function fetchAndCacheConfig(filePath: string): Promise<Record<string, unknown>> {
  const configDir = getConfigDirectory(filePath);

  if (configCache.has(configDir)) {
    return configCache.get(configDir)!;
  }

  let config: Record<string, unknown> = {};
  if (configFetcher) {
    try {
      config = await configFetcher(filePath);
    } catch {
      // Keep empty config on error
    }
  }

  configCache.set(configDir, config);
  return config;
}

/**
 * Merge user config with defaults, where user config takes precedence.
 */
function mergeOptions(userConfig: Record<string, unknown>): Record<string, unknown> {
  return { ...DEFAULT_OPTIONS, ...userConfig };
}

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
  prettierConfig?: Record<string, unknown>,
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

  // Get formatting options (config takes precedence over defaults)
  const formatOptions = prettierConfig
    ? mergeOptions(prettierConfig)
    : DEFAULT_OPTIONS;

  try {
    const formatted = await format(content, {
      parser,
      ...formatOptions,
    });
    return { formatted: formatted ?? content };
  } catch (err: unknown) {
    // Prettier throws on invalid syntax — return original + error message
    const message = err instanceof Error ? err.message : String(err);
    return { formatted: content, error: message };
  }
}

/**
 * Format source code using Prettier with automatic config discovery.
 * Fetches Prettier config from the backend for the file's project.
 */
export async function formatCodeWithConfigDiscovery(
  content: string,
  filePath: string,
  fileSize?: number,
): Promise<FormatResult> {
  const config = await fetchAndCacheConfig(filePath);
  return formatCode(content, filePath, fileSize, config);
}

/**
 * Check if a file can be formatted by Prettier based on its extension.
 */
export function isFormattable(filePath: string): boolean {
  const ext = getExtension(filePath);
  return ext in EXTENSION_TO_PARSER;
}

/** Clear the config cache (useful for testing). */
export function clearConfigCache(): void {
  configCache.clear();
}

function getExtension(filePath: string): string {
  const lastDot = filePath.lastIndexOf('.');
  if (lastDot === -1 || lastDot === filePath.length - 1) return '';
  return filePath.slice(lastDot).toLowerCase();
}
