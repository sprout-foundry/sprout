import type { FieldRenderers } from './useSettingsFieldRenderers';

interface OcrSettingsTabProps {
  renderToggle: FieldRenderers['renderToggle'];
  renderTextInput: FieldRenderers['renderTextInput'];
}

export default function OcrSettingsTab({
  renderToggle,
  renderTextInput,
}: OcrSettingsTabProps) {
  return (
    <div className="section">
      <h4>PDF OCR</h4>
      {renderToggle('pdf_ocr_enabled', 'Enable PDF OCR')}
      {renderTextInput('pdf_ocr_provider', 'Provider', 'zai, minimax, openrouter…')}
      {renderTextInput('pdf_ocr_model', 'Model', 'GLM-4.6V, MiniMax-VL, qwen-vl…')}
    </div>
  );
}
