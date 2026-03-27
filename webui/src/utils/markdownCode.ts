import React from 'react';

export const flattenMarkdownText = (value: React.ReactNode): string => {
  if (typeof value === 'string') {
    return value;
  }

  if (typeof value === 'number') {
    return String(value);
  }

  if (Array.isArray(value)) {
    return value.map(flattenMarkdownText).join('');
  }

  if (React.isValidElement(value)) {
    return flattenMarkdownText(value.props.children);
  }

  return '';
};

export const isMarkdownCodeBlock = (className: string | undefined, codeText: string): boolean =>
  Boolean(className || codeText.includes('\n'));
