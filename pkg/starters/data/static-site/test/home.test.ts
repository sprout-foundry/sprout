import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const homeHtml = readFileSync(
  fileURLToPath(new URL('../dist/index.html', import.meta.url)),
  'utf8',
);

describe('built home page', () => {
  it('renders the page title', () => {
    expect(homeHtml).toContain('<title>Home</title>');
  });

  it('renders the heading', () => {
    expect(homeHtml).toMatch(/<h1[^>]*>Welcome<\/h1>/);
  });

  it('renders a link to the about page', () => {
    expect(homeHtml).toContain('href="/about/"');
  });

  it('renders a contact form posting to the configured endpoint', () => {
    expect(homeHtml).toContain('<form');
    expect(homeHtml).toContain('https://formspree.io/f/your-form-id');
  });
});
