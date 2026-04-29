// Thin shell: wraps @sprout/ui StatusBar with local webui-specific prop computation
import { useMemo } from 'react';
import { StatusBar as SproutStatusBar, detectLineEnding } from '@sprout/ui';
import { allLanguageEntries, resolveLanguageId } from '../extensions/languageRegistry';

interface StatusBarBufferInfo {
  kind: string;
  file?: { name: string; ext?: string };
  content?: string;
  cursorPosition?: { line: number; column: number };
  languageOverride?: string | null;
}

interface WebuiStatusBarProps {
  branch?: string;
  buffer?: StatusBarBufferInfo | null;
  encoding?: string;
  indentation?: string;
}

/**
 * Webui-specific StatusBar that derives language name and line ending
 * from the buffer prop, then delegates rendering to @sprout/ui StatusBar.
 */
function StatusBar({ branch, buffer, encoding, indentation }: WebuiStatusBarProps): JSX.Element {
  // Language name — derived from buffer metadata using local language registry
  const language = useMemo(() => {
    if (!buffer) return undefined;
    if (buffer.kind === 'file' && buffer.file) {
      const { languageId } = resolveLanguageId(
        buffer.languageOverride,
        buffer.file.ext?.replace(/^\./, ''),
        buffer.file.name,
      );
      if (languageId) {
        const entry = allLanguageEntries.find((e) => e.id === languageId);
        if (entry) return entry.name;
      }
    }
    return buffer.kind.charAt(0).toUpperCase() + buffer.kind.slice(1);
  }, [buffer]);

  // Line ending — detected from buffer content
  const lineEnding = useMemo(() => {
    const result = detectLineEnding(buffer?.content || '');
    return result.lineEnding;
  }, [buffer?.content]);

  return (
    <SproutStatusBar
      branch={branch}
      cursorPosition={buffer?.cursorPosition}
      language={language}
      encoding={encoding}
      lineEnding={lineEnding}
      indentation={indentation}
      showRightSection={buffer != null}
    />
  );
}

export default StatusBar;
