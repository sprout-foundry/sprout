import { describe, it, expect } from 'vitest';
import { sanitizeSvg } from './svgSanitize';

describe('sanitizeSvg', () => {
  describe('basic valid SVG', () => {
    it('passes through a simple valid SVG unchanged', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100">
        <rect x="10" y="10" width="80" height="80" fill="blue"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).toContain('<rect');
      expect(result).toContain('fill="blue"');
      expect(result).toContain('width="100"');
    });

    it('preserves XML namespace declarations', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="100" height="100">
        <circle cx="50" cy="50" r="40" fill="red"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).toContain('xmlns="http://www.w3.org/2000/svg"');
      expect(result).toContain('xmlns:xlink="http://www.w3.org/1999/xlink"');
    });
  });

  describe('script tag removal', () => {
    it('removes <script> tags inside SVG', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100">
        <script>alert('XSS')</script>
        <rect x="10" y="10" width="80" height="80" fill="blue"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<script>');
      expect(result).not.toContain('alert');
      expect(result).toContain('<rect');
    });

    it('removes nested <script> tags', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <g>
          <script>alert('nested')</script>
        </g>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<script>');
      expect(result).not.toContain('alert');
    });

    it('removes multiple script tags', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <script>alert('1')</script>
        <rect/>
        <script>alert('2')</script>
        <circle/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<script>');
      expect(result).toContain('<rect');
      expect(result).toContain('<circle');
    });
  });

  describe('event handler removal', () => {
    it('removes onclick attributes', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <rect onclick="alert('XSS')" x="10" y="10" width="50" height="50"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('onclick');
      expect(result).not.toContain('alert');
      expect(result).toContain('<rect');
    });

    it('removes onload attributes', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <rect onload="stealCookies()" x="10" y="10" width="50" height="50"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('onload');
      expect(result).not.toContain('stealCookies');
    });

    it('removes onerror attributes', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <image onerror="evil()" href="bad.jpg"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('onerror');
      expect(result).not.toContain('evil');
    });

    it('removes onmouseover attributes', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <circle onmouseover="alert('hover')" cx="50" cy="50" r="20"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('onmouseover');
    });

    it('removes on attributes from nested elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <g>
          <rect onclick="alert(1)" x="0" y="0" width="20" height="20"/>
          <circle onload="alert(2)" cx="50" cy="50" r="20"/>
        </g>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('onclick');
      expect(result).not.toContain('onload');
      expect(result).toContain('<rect');
      expect(result).toContain('<circle');
    });
  });

  describe('javascript: URL removal', () => {
    it('removes javascript: URLs from href attributes', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <a href="javascript:alert('XSS')"><rect width="100" height="100"/></a>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('javascript:');
      expect(result).not.toContain('alert');
      expect(result).toContain('<a');
    });

    it('removes javascript: URLs from xlink:href attributes', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink">
        <a xlink:href="javascript:evil()"><rect width="100" height="100"/></a>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('javascript:');
      expect(result).not.toContain('evil');
    });

    it('preserves safe URLs', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink">
        <a href="https://example.com"><rect width="100" height="100"/></a>
        <image href="image.png"/>
        <use xlink:href="#icon"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).toContain('href="https://example.com"');
      expect(result).toContain('href="image.png"');
      expect(result).toContain('xlink:href="#icon"');
    });
  });

  describe('data: URL preservation', () => {
    it('preserves data: URLs (they are safe)', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <image href="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII="/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).toContain('data:image/png');
    });
  });

  describe('CSS expression() removal', () => {
    it('removes expression() from style elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <style>
          rect { fill: expression(alert('XSS')); }
        </style>
        <rect width="100" height="100"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('expression(');
      expect(result).not.toContain('alert');
      expect(result).toContain('<style>');
    });

    it('removes @import from style elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <style>
          @import url('evil.css');
          rect { fill: red; }
        </style>
        <rect width="100" height="100"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('@import');
      expect(result).toContain('fill: red');
    });
  });

  describe('dangerous element removal', () => {
    it('removes iframe elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <iframe src="evil.html"/>
        <rect width="100" height="100"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<iframe');
      expect(result).toContain('<rect');
    });

    it('removes object elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <object data="evil.swf"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<object');
    });

    it('removes embed elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <embed src="evil.swf"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<embed');
    });

    it('removes applet elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <applet code="Evil.class"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<applet');
    });

    it('removes form elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <form action="steal.php">
          <input type="text"/>
        </form>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<form');
      expect(result).not.toContain('<input');
    });

    it('removes input elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <input type="button" value="Click me"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<input');
    });

    it('removes textarea elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <textarea>XSS</textarea>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<textarea');
    });

    it('removes select elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <select><option>1</option></select>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<select');
    });

    it('removes button elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <button onclick="evil()">Click</button>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<button');
    });

    it('removes meta elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <meta http-equiv="refresh" content="0;url=evil"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<meta');
    });

    it('removes base elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <base href="https://evil.com/"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<base');
    });

    it('removes link elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <link rel="stylesheet" href="evil.css"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<link');
    });
  });

  describe('foreignObject removal', () => {
    it('removes foreignObject elements containing HTML with scripts', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <foreignObject width="100" height="100">
          <div xmlns="http://www.w3.org/1999/xhtml">
            <script>alert('XSS via foreignObject')</script>
            <p>Hello</p>
          </div>
        </foreignObject>
        <rect width="100" height="100" fill="blue"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<foreignObject');
      expect(result).not.toContain('alert');
      expect(result).toContain('<rect');
    });

    it('removes foreignObject with event handlers', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <foreignObject width="100" height="100">
          <div xmlns="http://www.w3.org/1999/xhtml" onerror="evil()">
            Click me
          </div>
        </foreignObject>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<foreignObject');
      expect(result).not.toContain('onerror');
      expect(result).not.toContain('evil');
    });

    it('removes deeply nested foreignObject elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <g>
          <foreignObject>
            <body xmlns="http://www.w3.org/1999/xhtml">
              <script>evil()</script>
            </body>
          </foreignObject>
        </g>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<foreignObject');
      expect(result).not.toContain('script');
    });
  });

  describe('SVG animation elements removal', () => {
    it('removes set element that can set event handlers', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <rect id="target" width="100" height="100"/>
        <set attributeName="onclick" to="alert(1)"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<set');
      expect(result).not.toContain('alert(1)');
      expect(result).toContain('<rect');
    });

    it('removes animate element', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <rect width="100" height="100" fill="red">
          <animate attributeName="onclick" from="" to="evil()"/>
        </rect>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<animate');
      expect(result).not.toContain('evil()');
      expect(result).toContain('<rect');
    });

    it('removes animateMotion element', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <circle cx="50" cy="50" r="20">
          <animateMotion path="M 10 10 L 100 100"/>
        </circle>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<animateMotion');
    });

    it('removes animateTransform element', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <rect width="100" height="100">
          <animateTransform attributeName="transform" type="rotate" from="0" to="360"/>
        </rect>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<animateTransform');
    });
  });

  describe('data:image/svg+xml URL removal', () => {
    it('removes data:image/svg+xml URLs from href', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <image href="data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciPjxzY3JpcHQ+YWxlcnQoJ1hTUycpPC9zY3JpcHQ+PC9zdmc+"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('data:image/svg+xml');
    });

    it('removes data:image/svg+xml from xlink:href', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink">
        <image xlink:href="data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciPjxzY3JpcHQ+ZXZpbCgpPC9zY3JpcHQ+PC9zdmc+"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('data:image/svg+xml');
    });

    it('removes case-insensitive data:image/svg+xml URLs', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <image href="DATA:IMAGE/SVG+XML;base64,PHN2Zy..."/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toMatch(/data:image\/svg\+xml/i);
    });
  });

  describe('data:text/html URL removal', () => {
    it('removes data:text/html URLs from href', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <image href="data:text/html;base64,PHNjcmlwdD5hbGVydCgnWFNTJyk8L3NjcmlwdD4="/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('data:text/html');
    });

    it('removes data:text/html from xlink:href', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink">
        <a xlink:href="data:text/html,<script>alert(1)</script>">Click</a>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('data:text/html');
    });

    it('removes case-insensitive data:text/html URLs', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <image href="DATA:TEXT/HTML;base64,PHNjcmlwdD4uLjwvc2NyaXB0Pg=="/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toMatch(/data:text\/html/i);
    });
  });

  describe('safe data: URLs preserved', () => {
    it('preserves data:image/png URLs', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <image href="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII="/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).toContain('data:image/png');
    });

    it('preserves data:image/jpeg URLs', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <image href="data:image/jpeg;base64,/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAP//////////////////////////////////////wAALCAABAAEBAREA/8QAFBABAAAAAAAAAAAAAAAAAAAAAP/aAAgBAQABPxA="/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).toContain('data:image/jpeg');
    });

    it('preserves data:image/gif URLs', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <image href="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).toContain('data:image/gif');
    });
  });

  describe('CSS url() sanitization', () => {
    it('removes javascript: URLs from CSS url()', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <style>
          rect { background: url("javascript:alert(1)"); }
        </style>
        <rect width="100" height="100"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('javascript:');
      expect(result).toContain('url()');
    });

    it('removes data:image/svg+xml from CSS url()', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <style>
          rect { background: url('data:image/svg+xml;base64,PHN2Zy...'); }
        </style>
        <rect width="100" height="100"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('data:image/svg+xml');
      expect(result).toContain('url()');
    });

    it('removes data:text/html from CSS url()', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <style>
          rect { background: url(data:text/html,<script>evil()</script>); }
        </style>
        <rect width="100" height="100"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('data:text/html');
      expect(result).toContain('url()');
    });

    it('preserves safe URLs in CSS url()', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <style>
          rect { background: url("image.png"); }
          circle { fill: url(#gradient); }
        </style>
        <rect width="100" height="100"/>
        <circle cx="50" cy="50" r="20"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).toContain('url("image.png")');
      expect(result).toContain('url(#gradient)');
    });
  });

  describe('inline style attribute sanitization', () => {
    it('removes javascript: URLs from inline style', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <rect style="background: url('javascript:alert(1)')" width="100" height="100"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('javascript:');
      expect(result).toContain('url()');
    });

    it('removes data:image/svg+xml from inline style', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <rect style="background: url('data:image/svg+xml;base64,PHN2Zy...')" width="100" height="100"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('data:image/svg+xml');
      expect(result).toContain('url()');
    });

    it('removes @import from inline style', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <rect style="@import url('evil.css'); fill: red" width="100" height="100"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('@import');
      expect(result).toContain('fill: red');
    });

    it('removes expression() from inline style', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <rect style="width: expression(alert(1))" width="100" height="100"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('expression(');
      expect(result).not.toContain('alert');
    });

    it('preserves safe CSS in inline style', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <rect style="fill: blue; stroke: red; stroke-width: 2" width="100" height="100"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).toContain('fill: blue');
      expect(result).toContain('stroke: red');
      expect(result).toContain('stroke-width: 2');
    });
  });

  describe('unicode and whitespace bypass attempts', () => {
    it('blocks javascript: with zero-width space (U+200B)', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <a href="java\u200Bscript:alert(1)">Click</a>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toMatch(/javascript:/i);
      expect(result).not.toContain('alert');
    });

    it('blocks javascript: with zero-width non-joiner (U+200C)', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <a href="java\u200Cscript:evil()">Click</a>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toMatch(/javascript:/i);
      expect(result).not.toContain('evil');
    });

    it('blocks javascript: with zero-width joiner (U+200D)', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <a href="java\u200Dscript:alert(1)">Click</a>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toMatch(/javascript:/i);
    });

    it('blocks javascript: with BOM (U+FEFF)', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <a href="java\uFEFFscript:alert(1)">Click</a>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toMatch(/javascript:/i);
    });

    it('blocks javascript: with extra whitespace', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <a href="java  script:alert(1)">Click</a>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toMatch(/javascript:/i);
    });

    it('handles CSS url() with unicode bypass', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <style>
          rect { background: url("java\u200Bscript:alert(1)"); }
        </style>
        <rect width="100" height="100"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toMatch(/javascript:/i);
      expect(result).toContain('url()');
    });
  });

  describe('multiple dangerous elements', () => {
    it('removes all types of dangerous elements in one SVG', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <script>alert(1)</script>
        <iframe src="evil.html"/>
        <object data="evil.swf"/>
        <embed src="evil.swf"/>
        <applet code="Evil.class"/>
        <form action="steal"/>
        <input type="text"/>
        <textarea>XSS</textarea>
        <select><option>x</option></select>
        <button onclick="evil()">Click</button>
        <meta http-equiv="refresh"/>
        <base href="evil"/>
        <link rel="stylesheet" href="evil.css"/>
        <foreignObject><script>evil()</script></foreignObject>
        <set attributeName="onclick" to="alert(1)"/>
        <animate attributeName="onclick" to="evil()"/>
        <animateMotion path="M 0 0"/>
        <animateTransform attributeName="transform" type="rotate"/>
        <rect width="100" height="100" fill="blue"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<script');
      expect(result).not.toContain('<iframe');
      expect(result).not.toContain('<object');
      expect(result).not.toContain('<embed');
      expect(result).not.toContain('<applet');
      expect(result).not.toContain('<form');
      expect(result).not.toContain('<input');
      expect(result).not.toContain('<textarea');
      expect(result).not.toContain('<select');
      expect(result).not.toContain('<button');
      expect(result).not.toContain('<meta');
      expect(result).not.toContain('<base');
      expect(result).not.toContain('<link');
      expect(result).not.toContain('<foreignObject');
      expect(result).not.toContain('<set');
      expect(result).not.toContain('<animate');
      expect(result).not.toContain('<animateMotion');
      expect(result).not.toContain('<animateTransform');
      expect(result).toContain('<rect');
      expect(result).toContain('fill="blue"');
    });
  });

  describe('edge cases', () => {
    it('handles empty SVG gracefully', () => {
      const input = '';
      const result = sanitizeSvg(input);
      expect(result).toBe('');
    });

    it('handles whitespace-only SVG gracefully', () => {
      const input = '   \n\t  ';
      const result = sanitizeSvg(input);
      expect(result).toBe('');
    });

    it('handles malformed SVG gracefully', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <rect width="100" height="100"
      </svg>`;

      const result = sanitizeSvg(input);
      // Should either return empty string or best-effort sanitized output
      expect(typeof result).toBe('string');
    });

    it('handles SVG fragments (no root svg element)', () => {
      const input = `<rect width="100" height="100" fill="blue"/>`;
      const result = sanitizeSvg(input);
      expect(result).toContain('<rect');
    });

    it('preserves safe attributes on elements', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <rect id="myRect" class="highlight" x="10" y="10" width="80" height="80" fill="#0000ff" stroke="red" stroke-width="2"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).toContain('id="myRect"');
      expect(result).toContain('class="highlight"');
      expect(result).toContain('x="10"');
      expect(result).toContain('y="10"');
      expect(result).toContain('width="80"');
      expect(result).toContain('height="80"');
      expect(result).toContain('fill="#0000ff"');
      expect(result).toContain('stroke="red"');
      expect(result).toContain('stroke-width="2"');
    });

    it('handles case-insensitive attribute names', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <rect ONCLICK="alert(1)" x="10" y="10" width="80" height="80"/>
        <circle OnLoad="alert(2)" cx="50" cy="50" r="20"/>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toMatch(/onclick/i);
      expect(result).not.toMatch(/onload/i);
    });
  });

  describe('nested dangerous elements', () => {
    it('removes deeply nested script tags', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <g>
          <g>
            <g>
              <script>alert('deep')</script>
            </g>
          </g>
        </g>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<script>');
      expect(result).not.toContain('alert');
    });

    it('removes dangerous elements at multiple nesting levels', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg">
        <script>alert('level1')</script>
        <g>
          <script>alert('level2')</script>
          <g>
            <script>alert('level3')</script>
            <rect width="100" height="100"/>
          </g>
        </g>
      </svg>`;

      const result = sanitizeSvg(input);
      expect(result).not.toContain('<script>');
      expect(result).not.toContain('alert');
      expect(result).toContain('<rect');
    });
  });

  describe('complex real-world examples', () => {
    it('sanitizes a complex SVG with mixed threats', () => {
      const input = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="200" height="200">
        <defs>
          <script>alert('def script')</script>
        </defs>
        <style>
          @import url('evil.css');
          rect { fill: expression(alert('css')); }
          circle { fill: blue; }
        </style>
        <g id="group1">
          <rect onclick="alert('rect')" x="10" y="10" width="80" height="80" fill="red"/>
          <circle onload="alert('circle')" cx="150" cy="50" r="30"/>
        </g>
        <a xlink:href="javascript:steal()">
          <polygon points="100,100 120,150 80,150"/>
        </a>
        <image href="safe-image.png"/>
      </svg>`;

      const result = sanitizeSvg(input);

      // Script tags should be removed
      expect(result).not.toContain('<script>');
      expect(result).not.toContain('alert');

      // @import and expression should be removed
      expect(result).not.toContain('@import');
      expect(result).not.toContain('expression(');

      // Event handlers should be removed
      expect(result).not.toMatch(/onclick/i);
      expect(result).not.toMatch(/onload/i);

      // javascript: URLs should be removed
      expect(result).not.toContain('javascript:');

      // Safe content should remain
      expect(result).toContain('<rect');
      expect(result).toContain('<circle');
      expect(result).toContain('<polygon');
      expect(result).toContain('fill: blue');
      expect(result).toContain('xmlns="http://www.w3.org/2000/svg"');
    });
  });
});
