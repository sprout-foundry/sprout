/**
 * SVG Sanitizer Utility
 *
 * Removes dangerous elements and attributes from SVG content to prevent XSS attacks.
 * Uses only standard DOM APIs - no external dependencies.
 */

// Elements that are always removed
const DANGEROUS_ELEMENTS = new Set([
  'script',
  'iframe',
  'object',
  'embed',
  'applet',
  'form',
  'input',
  'textarea',
  'select',
  'button',
  'meta',
  'base',
  'link',
  'foreignobject', // Allows embedding arbitrary HTML (including scripts)
  'set', // Can dynamically set event handler attributes
  'animate', // Can animate attributes including event handlers
  'animatemotion', // Can animate motion along a path
  'animatetransform', // Can animate transformations
]);

// Attributes that can contain URLs (need to check for javascript: protocol)
const URL_ATTRIBUTES = new Set([
  'href',
  'xlink:href',
  'src',
  'background',
  'poster',
  'data',
  'cite',
  'action',
  'formaction',
  'longdesc',
  'usemap',
  'profile',
]);

/**
 * Normalizes a URL by removing zero-width characters and extra whitespace
 * This prevents bypass attempts like "java\u200Bscript:"
 */
function normalizeUrl(value: string): string {
  // Remove zero-width spaces (U+200B), zero-width non-joiners (U+200C),
  // zero-width joiners (U+200D), and BOM (U+FEFF)
  let result = value.replace(/[\u200B\u200C\u200D\uFEFF]/g, '');
  // Remove extra whitespace but keep single spaces
  result = result.replace(/\s+/g, ' ');
  return result;
}

/**
 * Checks if a URL attribute value is safe (no javascript: or dangerous data: URLs)
 */
function isSafeUrl(value: string): boolean {
  const normalized = normalizeUrl(value);
  const trimmed = normalized.trim().toLowerCase();

  // Block javascript: protocol (with unicode bypass protection)
  if (trimmed.startsWith('javascript:')) {
    return false;
  }

  // Block dangerous data: URLs that can contain scripts
  if (trimmed.startsWith('data:')) {
    // Check for data:image/svg+xml or data:text/html which can execute scripts
    if (trimmed.startsWith('data:image/svg+xml') || trimmed.startsWith('data:text/html')) {
      return false;
    }
    // Other data: URLs (png, jpeg, etc.) are safe
    return true;
  }

  return true;
}

/**
 * Checks if a CSS value contains dangerous constructs
 */
function sanitizeCssValue(value: string): string {
  let result = value;

  // Remove @import statements
  result = result.replace(/@import\s+[^;]+;?/gi, '');

  // Remove expression() calls (IE-specific, but dangerous)
  result = result.replace(/expression\s*\([^)]*\)/gi, '');

  // Sanitize url() functions that contain dangerous URLs
  // This matches url("javascript:...") or url('javascript:...') or url(javascript:...)
  result = result.replace(/url\s*\(\s*(['"]?)(.*?)\1\s*\)/gi, (match, quote, url) => {
    // Remove zero-width characters and check if URL is safe
    const normalized = normalizeUrl(url);
    const trimmed = normalized.trim().toLowerCase();

    // Check for dangerous URLs
    if (trimmed.startsWith('javascript:') ||
        trimmed.startsWith('data:image/svg+xml') ||
        trimmed.startsWith('data:text/html')) {
      // Replace with empty url()
      return 'url()';
    }
    // Keep the original url() with the safe URL
    return match;
  });

  return result;
}

/**
 * Sanitizes a <style> element's content
 */
function sanitizeStyleElement(styleElement: SVGStyleElement): void {
  if (styleElement.textContent) {
    styleElement.textContent = sanitizeCssValue(styleElement.textContent);
  }
}

/**
 * Checks if an attribute name is an event handler (starts with "on")
 */
function isEventHandler(attrName: string): boolean {
  return attrName.toLowerCase().startsWith('on');
}

/**
 * Sanitizes an element and its descendants
 */
function sanitizeElement(element: Element): void {
  // Remove dangerous elements first (before processing children)
  const children = Array.from(element.children);
  for (const child of children) {
    const tagName = child.tagName.toLowerCase();
    if (DANGEROUS_ELEMENTS.has(tagName)) {
      child.remove();
    } else {
      // Recursively sanitize children
      sanitizeElement(child);
    }
  }

  // Sanitize <style> elements specifically
  if (element.tagName === 'style') {
    sanitizeStyleElement(element as SVGStyleElement);
  }

  // Remove dangerous attributes
  const attributes = Array.from(element.attributes);
  for (const attr of attributes) {
    const attrName = attr.name.toLowerCase();

    // Remove event handlers
    if (isEventHandler(attrName)) {
      element.removeAttribute(attr.name);
      continue;
    }

    // Sanitize inline style attribute
    if (attrName === 'style') {
      const sanitizedStyle = sanitizeCssValue(attr.value);
      if (sanitizedStyle !== attr.value) {
        // Update the attribute with sanitized value
        element.setAttribute(attr.name, sanitizedStyle);
      }
      continue;
    }

    // Sanitize URL attributes
    if (URL_ATTRIBUTES.has(attrName) || attrName.startsWith('data-')) {
      if (!isSafeUrl(attr.value)) {
        element.removeAttribute(attr.name);
      }
    }
  }
}

/**
 * Sanitizes SVG content by removing dangerous elements and attributes.
 *
 * @param svgContent - Raw SVG content as a string
 * @returns Sanitized SVG content, or empty string if parsing fails
 */
export function sanitizeSvg(svgContent: string): string {
  if (!svgContent || !svgContent.trim()) {
    return '';
  }

  try {
    // Parse the SVG content
    const parser = new DOMParser();
    const doc = parser.parseFromString(svgContent, 'image/svg+xml');

    // Check for parsing errors
    const parserError = doc.querySelector('parsererror');
    if (parserError) {
      console.warn('SVG sanitizer: Failed to parse SVG content');
      return '';
    }

    // Get the root SVG element
    const svgElement = doc.querySelector('svg');
    if (!svgElement) {
      // If no SVG element found, try to sanitize the whole document
      // (might be a fragment)
      sanitizeElement(doc.documentElement);
    } else {
      sanitizeElement(svgElement);
    }

    // Serialize back to string
    const serializer = new XMLSerializer();
    const sanitized = serializer.serializeToString(doc);

    return sanitized;
  } catch (err) {
    console.warn('SVG sanitizer: Error processing SVG content', err);
    return '';
  }
}
