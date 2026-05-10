import { useState, useRef, useEffect, useCallback } from 'react';
import type { ChangeEvent, ClipboardEvent as ReactClipboardEvent } from 'react';
import { ApiService } from '../services/api';
import { debugLog } from '../utils/log';

export interface AttachedImage {
  id: string;
  file: File;
  preview: string;
  uploadedPath?: string;
  error?: string;
}

interface UseImageUploadOptions {
  inputRef: React.RefObject<HTMLTextAreaElement | null>;
}

export function useImageUpload({ inputRef }: UseImageUploadOptions) {
  const fileInputRef = useRef<HTMLInputElement>(null);
  const apiService = useRef(ApiService.getInstance());
  const uploadInProgressRef = useRef<Set<string>>(new Set());
  const [attachedImages, setAttachedImages] = useState<AttachedImage[]>([]);
  const [previewImageId, setPreviewImageId] = useState<string | null>(null);

  const previewImage = previewImageId
    ? attachedImages.find((img) => img.id === previewImageId) || null
    : null;

  // Handle paste event for images
  const handlePaste = useCallback((e: ReactClipboardEvent) => {
    const items = e.clipboardData.items;
    for (let i = 0; i < items.length; i++) {
      if (items[i].type.startsWith('image/')) {
        e.preventDefault();
        const blob = items[i].getAsFile();
        if (blob) {
          const preview = URL.createObjectURL(blob);
          const imageId = crypto.randomUUID();
          setAttachedImages((prev) => [
            ...prev,
            {
              id: imageId,
              file: blob,
              preview,
            },
          ]);
        }
        break; // Only handle first image
      }
    }
  }, []);

  // Handle file selection from input
  const handleFileSelect = useCallback((e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      const preview = URL.createObjectURL(file);
      const imageId = crypto.randomUUID();
      setAttachedImages((prev) => [
        ...prev,
        {
          id: imageId,
          file,
          preview,
        },
      ]);
      // Reset input so same file can be selected again
      e.target.value = '';
      // Focus back to textarea
      inputRef.current?.focus();
    }
  }, [inputRef]);

  // Click handler for upload button
  const handleUploadClick = useCallback(() => {
    fileInputRef.current?.click();
  }, []);

  // Remove an image from the list
  const removeImage = useCallback((id: string) => {
    setAttachedImages((prev) => {
      const imageToRemove = prev.find((img) => img.id === id);
      if (imageToRemove) {
        URL.revokeObjectURL(imageToRemove.preview);
      }
      // Clean up upload tracking ref
      uploadInProgressRef.current.delete(id);
      return prev.filter((img) => img.id !== id);
    });
    setPreviewImageId((current) => (current === id ? null : current));
  }, []);

  // Clear all images and revoke URLs
  const clearImages = useCallback(() => {
    setAttachedImages((prev) => {
      prev.forEach((img) => URL.revokeObjectURL(img.preview));
      // Clean up upload tracking ref
      prev.forEach((img) => uploadInProgressRef.current.delete(img.id));
      return [];
    });
    setPreviewImageId(null);
  }, []);

  // Upload image to server
  const uploadImageAsync = useCallback(async (imageId: string, imageFile: File) => {
    if (uploadInProgressRef.current.has(imageId)) return;
    uploadInProgressRef.current.add(imageId);

    try {
      const result = await apiService.current.uploadImage(imageFile);
      setAttachedImages((prev) =>
        prev.map((img) => (img.id === imageId ? { ...img, uploadedPath: result.path, error: undefined } : img)),
      );
    } catch (error) {
      debugLog('Failed to upload image:', error);
      setAttachedImages((prev) =>
        prev.map((img) =>
          img.id === imageId ? { ...img, error: error instanceof Error ? error.message : 'Upload failed' } : img,
        ),
      );
    }
  }, []);

  // Auto-upload images when they are added
  useEffect(() => {
    attachedImages.forEach((img) => {
      if (!img.uploadedPath && !img.error) {
        uploadImageAsync(img.id, img.file);
      }
    });
  }, [attachedImages, uploadImageAsync]);

  // Escape key handler for preview modal
  useEffect(() => {
    if (!previewImageId) {
      return;
    }

    const handlePreviewEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setPreviewImageId(null);
      }
    };

    window.addEventListener('keydown', handlePreviewEscape);
    return () => window.removeEventListener('keydown', handlePreviewEscape);
  }, [previewImageId]);

  return {
    attachedImages,
    previewImageId,
    setPreviewImageId,
    previewImage,
    handlePaste,
    handleUploadClick,
    removeImage,
    fileInputRef,
    clearImages,
    handleFileSelect,
  };
}
