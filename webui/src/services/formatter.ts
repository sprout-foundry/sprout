import { format } from 'prettier/standalone';
import type { BuiltInParserName, Plugin } from 'prettier';
import * as prettierPluginBabel from 'prettier/plugins/babel';
import * as prettierPluginEstree from 'prettier/plugins/estree';
import * as prettierPluginTypescript from 'prettier/plugins/typescript';
import * as prettierPluginPostcss from 'prettier/plugins/postcss';
import * as prettierPluginHtml from 'prettier/plugins/html';
import * as prettierPluginMarkdown from 'prettier/plugins/markdown';
import * as prettierPluginYaml from 'prettier/plugins/yaml';
import * as prettierPluginGraphql from 'prettier/plugins/graphql';

/**
 * Plugins required by the browser standalone bundle.
 *
 * The `prettier` package's `browser` field maps `prettier` → `prettier/standalone`,
 * which Vite resolves via the browser condition. The standalone bundle ships NO
 * built-in parsers — every parser must be provided explicitly via `plugins`.
 * Without this, `format()` throws:
 *   Couldn't resolve parser "json". Plugins must be explicitly added to the standalone bundle.
 *
 * Parser coverage (Prettier 3.x):
 *   - babel plugin:       babel, babel-ts, json, json5, jsonc
 *   - estree plugin:      shared printer dependency for babel/typescript
 *   - typescript plugin:  typescript (requires estree)
 *   - postcss plugin:     css, scss, less
 *   - html plugin:        html, vue
 *   - markdown plugin:    markdown, mdx
 *   - yaml plugin:        yaml
 *   - graphql plugin:     graphql
 */
const PLUGINS: Plugin[] = [
  prettierPluginBabel,
  prettierPluginEstree,
  prettierPluginTypescript,
  prettierPluginPostcss,
  prettierPluginHtml,
  prettierPluginMarkdown,
  prettierPluginYaml,
  prettierPluginGraphql,
];

/** Merge any user-provided plugins from config with the required standalone plugins. */
function resolvePlugins(formatOptions: Record<string, unknown>): Plugin[] {
  const userPlugins = Array.isArray(formatOptions.plugins) ? (formatOptions.plugins as Plugin[]) : [];
  return [...PLUGINS, ...userPlugins];
}

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

/** Cached Prettier config (single entry — the backend resolves from workspace root). */
let cachedConfig: Record<string, unknown> | null = null;

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
 * Fetch and cache Prettier config.
 * The backend resolves config from the workspace root, so the result is the
 * same regardless of which file path is passed. We cache a single entry.
 */
async function fetchAndCacheConfig(filePath: string): Promise<Record<string, unknown>> {
  if (cachedConfig !== null) {
    return cachedConfig;
  }

  let config: Record<string, unknown> = {};
  if (configFetcher) {
    try {
      config = await configFetcher(filePath);
    } catch {
      // Keep empty config on error
    }
  }

  cachedConfig = config;
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
  const formatOptions = prettierConfig ? mergeOptions(prettierConfig) : DEFAULT_OPTIONS;

  try {
    const formatted = await format(content, {
      parser,
      ...formatOptions,
      plugins: resolvePlugins(formatOptions),
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
  cachedConfig = null;
}

function getExtension(filePath: string): string {
  const lastDot = filePath.lastIndexOf('.');
  if (lastDot === -1 || lastDot === filePath.length - 1) return '';
  return filePath.slice(lastDot).toLowerCase();
}
