import { X } from 'lucide-react';
import type { AttachedImage } from './useImageUpload';

interface ImagePreviewPanelProps {
  attachedImages: AttachedImage[];
  previewImageId: string | null;
  setPreviewImageId: (id: string | null) => void;
  previewImage: AttachedImage | null;
  removeImage: (id: string) => void;
}

export function ImagePreviewPanel({
  attachedImages,
  previewImageId,
  setPreviewImageId,
  previewImage,
  removeImage,
}: ImagePreviewPanelProps): JSX.Element {
  return (
    <>
      {attachedImages.length > 0 && (
        <div className="image-preview-strip">
          {attachedImages.map((img) => (
            <div
              key={img.id}
              className={`image-preview-chip ${img.error ? 'error' : ''} ${!img.uploadedPath && !img.error ? 'uploading' : ''}`}
            >
              <button
                type="button"
                className="image-preview-open"
                onClick={() => setPreviewImageId(img.id)}
                aria-label={`Preview ${img.file.name}`}
              >
                <img src={img.preview} alt={img.file.name} />
              </button>
              <span className="image-name">{img.file.name}</span>
              {!img.uploadedPath && !img.error && <span className="upload-spinner" />}
              {img.error && <span className="upload-error">{img.error}</span>}
              <button
                type="button"
                className="remove-btn"
                onClick={(event) => {
                  event.stopPropagation();
                  removeImage(img.id);
                }}
                aria-label="Remove image"
              >
                <X size={12} />
              </button>
            </div>
          ))}
        </div>
      )}

      {previewImage ? (
        <div
          className="image-preview-modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-label={`Preview image ${previewImage.file.name}`}
          onClick={() => setPreviewImageId(null)}
        >
          <div className="image-preview-modal" onClick={(event) => event.stopPropagation()}>
            <div className="image-preview-modal-header">
              <span>{previewImage.file.name}</span>
              <button
                type="button"
                className="image-preview-modal-close"
                onClick={() => setPreviewImageId(null)}
                aria-label="Close image preview"
              >
                <X size={16} />
              </button>
            </div>
            <div className="image-preview-modal-body">
              <img src={previewImage.preview} alt={previewImage.file.name} />
            </div>
          </div>
        </div>
      ) : null}
    </>
  );
}
