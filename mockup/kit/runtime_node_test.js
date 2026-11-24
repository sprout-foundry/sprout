// Node check for the link-resolution helpers in runtime.js (no DOM needed).
// Run: node mockup/kit/runtime_node_test.js
var r=require('./runtime.js');
var assert=require('assert');
var screens=['home','menu','about'];
// Page hrefs match the screen whose id equals the slug of the last path segment.
assert.strictEqual(r.resolveLink('/menu','',screens),'menu');
assert.strictEqual(r.resolveLink('menu.html','',screens),'menu');
assert.strictEqual(r.resolveLink('./menu?x=1','',screens),'menu');
assert.strictEqual(r.resolveLink('#menu','',screens),'menu'); // fragment with no id
// The text fallback: slug equals, or ends with, a screen id.
assert.strictEqual(r.resolveLink('https://example.com/nope','Our Menu',screens),'menu');
assert.strictEqual(r.resolveLink('/nothing','Our Menu',screens),'menu');
assert.strictEqual(r.resolveLink('/about','About',screens),'about');
// No match: '' (the link gets data-go-none).
assert.strictEqual(r.resolveLink('https://example.com','',screens),'');
assert.strictEqual(r.resolveLink('/nothing','',screens),'');
assert.strictEqual(r.resolveLink('mailto:a@example.com','',screens),'');
// Slug/href helpers.
assert.strictEqual(r.slugify('Our Menu'),'our-menu');
assert.strictEqual(r.hrefSlug('./menu?x=1'),'menu');
assert.strictEqual(r.hrefSlug('menu.html'),'menu');
assert.strictEqual(r.hrefSlug('#menu'),'menu');
assert.strictEqual(r.hrefSlug('#'),'');
// data-go-none kinds and notes.
assert.strictEqual(r.noneKind('mailto:a@example.com'),'mail');
assert.strictEqual(r.noneKind('tel:+15551234'),'tel');
assert.strictEqual(r.noneKind('https://example.com'),'none');
assert.strictEqual(r.NONE_NOTE.mail,'This opens email');
assert.strictEqual(r.NONE_NOTE.tel,'This calls the number');
assert.strictEqual(r.NONE_NOTE.none,"This link isn't part of the first look");
console.log('runtime resolution checks passed');
