/**
 * TokenValueEditor — the structured token value editor (SP-140-7 §7c).
 *
 * Mounted in the TokensTree detail pane for the selected leaf: a type-aware
 * input (color swatch + hex field, number+unit for dimensions) with an alias
 * warning (N references — from the §6b tokenRefs payload) and a Save that
 * performs the surgical DTCG edit through the §7a safe-write seam. Light
 * client validation only: the authoritative check is design_validate, whose
 * findings the health strip already surfaces.
 *
 * The edit flow: read the token file → applyTokenEdit (parse → one leaf →
 * canonical stringify) → writeAssetIfUnchanged. The form never invents
 * structure; it only sets the one leaf's $value.
 */

import { useEffect, useState } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import { applyTokenEdit, coerceTokenValue, referenceCount } from '../../design/tokenEdit';
import { designRootPath, baseMtimeFromResponse, writeAssetIfUnchanged } from '../../services/api/designApi';
export interface TokenValueEditorProps {
  /** Dotted path of the selected token, e.g. "color.brand.primary". */
  tokenPath: string;
  /** Workspace-relative file the token lives in, e.g. "design/tokens/color.tokens.json". */
  filePath: string;
  /** The leaf's $type (drives the input shape). */
  type?: string;
  /** The current $value text (as the tree displays it). */
  currentText: string;
  /** The §6b tokenRefs map (references per dotted path). */
  tokenRefs?: Record<string, number>;
  /** Read transport override (tests). */
  readFn?: typeof fetch;
  /** Fired after a successful save (hosts refetch the inventory). */
  onSaved?: () => void;
}

export default function TokenValueEditor({
  tokenPath,
  filePath,
  type,
  currentText,
  tokenRefs,
  readFn,
  onSaved,
}: TokenValueEditorProps) {
  const contextFetch = useSproutFetch();
  const transport = readFn ?? contextFetch;
  const [draft, setDraft] = useState(currentText);
  const [status, setStatus] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle');
  const [errorText, setErrorText] = useState('');
  const [conflictText, setConflictText] = useState('');

  useEffect(() => {
    setDraft(currentText);
    setStatus('idle');
    setErrorText('');
    setConflictText('');
  }, [currentText, tokenPath, filePath]);

  const refs = referenceCount(tokenRefs, tokenPath);
  const coerced = coerceTokenValue(type, draft);
  const isColor = type === 'color' && !draft.trim().startsWith('{');

  const handleSave = async () => {
    if (!coerced.plausible || status === 'saving') return;
    setStatus('saving');
    setErrorText('');
    setConflictText('');
    try {
      const readResponse = await transport(`/api/file?path=${encodeURIComponent(designRootPath(filePath))}`);
      if (!readResponse.ok) throw new Error('could not read the token file');
      const fileText = await readResponse.text();
      const nextText = applyTokenEdit(fileText, tokenPath, coerced.value);
      // §7a: guard the write with the revision just read, so an agent write
      // between read and save surfaces as the conflict message, not a loss.
      await writeAssetIfUnchanged(transport, filePath, nextText, {
        baseMtime: baseMtimeFromResponse(readResponse),
      });
      setStatus('saved');
      onSaved?.();
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      if (message.includes('changed after it was read')) {
        setConflictText('The token file changed while you were editing. Reload the Tokens tab and try again.');
      } else {
        setErrorText(message);
      }
      setStatus('error');
    }
  };

  return (
    <div className="design-token-editor" data-testid="design-token-editor">
      <div className="design-tokens-detail-row">
        <span className="design-tokens-detail-label">Edit</span>
        <span className="design-token-editor-input">
          {isColor && (
            <input
              type="color"
              className="design-token-color-well"
              aria-label="Pick a color"
              value={/^#[0-9a-f]{6}$/i.test(draft) ? draft : '#000000'}
              onChange={(event) => setDraft(event.target.value)}
              data-testid="design-token-color-well"
            />
          )}
          <input
            type="text"
            className="design-token-input"
            value={draft}
            onChange={(event) => {
              setDraft(event.target.value);
              setStatus('idle');
            }}
            data-testid="design-token-input"
            aria-label={`Value for ${tokenPath}`}
          />
        </span>
      </div>
      {!coerced.plausible && draft.trim() !== '' && (
        <p className="design-token-warn" data-testid="design-token-warn">
          This does not look like a valid {type ?? 'value'}; the validator will flag it if saved.
        </p>
      )}
      {refs > 0 && (
        <p className="design-token-warn" data-testid="design-token-refs">
          {refs} {refs === 1 ? 'token references' : 'tokens reference'} this value — aliases update with it.
        </p>
      )}
      <div className="design-token-editor-actions">
        <button
          type="button"
          className="design-token-save"
          data-testid="design-token-save"
          disabled={!coerced.plausible || status === 'saving' || draft === currentText}
          onClick={handleSave}
        >
          {status === 'saving' ? 'Saving…' : 'Save value'}
        </button>
        {status === 'saved' && (
          <span className="design-token-saved" data-testid="design-token-saved">
            Saved
          </span>
        )}
        {conflictText && (
          <span className="design-token-conflict" data-testid="design-token-conflict" role="alert">
            {conflictText}
          </span>
        )}
        {errorText && (
          <span className="design-token-error" data-testid="design-token-error" role="alert">
            {errorText}
          </span>
        )}
      </div>
    </div>
  );
}
