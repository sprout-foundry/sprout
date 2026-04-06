/**
 * Regular expression pattern to match binary file extensions.
 * Files matching this pattern are skipped during drag-and-drop to avoid
 * attempting to open binary content as text.
 */
export const BINARY_FILE_PATTERN = /\.(png|jpe?g|gif|bmp|webp|ico|tiff?|avif|mp[34]|wav|ogg|mpg|mpeg|avi|mov|zip|tar|gz|bz2|xz|7z|rar|exe|dll|so|dylib|wasm|pdf|docx?|xlsx?|pptx?|odt|ods|odp|ttf|otf|woff2?|eot|bin|dat|db|sqlite)$/i;

/**
 * Checks if a filename appears to be a binary file based on extension.
 * Returns true if the file should be skipped during drag-and-drop.
 */
export function isBinaryFile(fileName: string): boolean {
  return BINARY_FILE_PATTERN.test(fileName);
}
