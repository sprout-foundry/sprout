(async () => {
  // Capture a rendered page as one standalone HTML document.
  // opts: { selector: string, maxBytes: number, level: number, viewportWidth: number, viewportHeight: number }
  // Returns a JSON string: { html, trimmed, title, finalUrl, viewportWidth, viewportHeight } or { error }.
  const opts = __OPTS__;
  const FONT_MAX_BYTES = 300 * 1024;
  const ENC = new TextEncoder();
  const bytesOf = (s) => ENC.encode(s).length;
  const clampLevel = (n) => Math.max(0, Math.min(3, n | 0));

  const targetSel = String(opts.selector || '').trim();
  const target = targetSel ? document.querySelector(targetSel) : null;
  if (targetSel && !target) {
    return JSON.stringify({ error: 'selector not found: ' + targetSel });
  }
  const vw = Math.round(opts.viewportWidth || window.innerWidth);
  const vh = Math.round(opts.viewportHeight || window.innerHeight);

  const title = document.title || '';
  const finalUrl = location.href;

  const imageCache = new Map();
  const bgCache = new Map();
  const fontCache = new Map();

  const fetchAsDataUrl = async (url, sizeLimit) => {
    if (!url || /^data:/.test(url) || /^blob:/.test(url)) return url || null;
    try {
      const res = await fetch(url);
      if (!res.ok) return null;
      const blob = await res.blob();
      if (blob.size > (sizeLimit || 4 * 1024 * 1024)) return null;
      if (blob.type && /text\/html|javascript|json|xml/.test(blob.type)) return null;
      return await new Promise((resolve) => {
        const reader = new FileReader();
        reader.onloadend = () => resolve(reader.result);
        reader.onerror = () => resolve(null);
        reader.readAsDataURL(blob);
      });
    } catch (_e) {
      return null;
    }
  };

  const getImageDataUrl = async (url) => {
    if (!url) return null;
    if (imageCache.has(url)) return imageCache.get(url);
    const dataUrl = await fetchAsDataUrl(url, 4 * 1024 * 1024);
    imageCache.set(url, dataUrl);
    return dataUrl;
  };

  const getBgImageDataUrl = async (url) => {
    if (!url) return null;
    if (bgCache.has(url)) return bgCache.get(url);
    const dataUrl = await fetchAsDataUrl(url, 4 * 1024 * 1024);
    bgCache.set(url, dataUrl);
    return dataUrl;
  };

  const downscaleDataUrl = async (dataUrl) => {
    try {
      const img = new Image();
      img.crossOrigin = 'anonymous';
      img.src = dataUrl;
      await new Promise((resolve, reject) => {
        img.onload = resolve;
        img.onerror = reject;
      });
      const scale = 0.5;
      const w = Math.max(1, Math.round(img.naturalWidth * scale));
      const h = Math.max(1, Math.round(img.naturalHeight * scale));
      const canvas = document.createElement('canvas');
      canvas.width = w;
      canvas.height = h;
      const ctx = canvas.getContext('2d');
      ctx.drawImage(img, 0, 0, w, h);
      return canvas.toDataURL('image/jpeg', 0.75);
    } catch (_e) {
      return null;
    }
  };

  const getFontDataUrl = async (url) => {
    if (!url || fontCache.has(url)) return fontCache.get(url);
    try {
      const res = await fetch(url);
      if (!res.ok) { fontCache.set(url, null); return null; }
      const blob = await res.blob();
      if (blob.size > FONT_MAX_BYTES) { fontCache.set(url, null); return null; }
      const dataUrl = await new Promise((resolve) => {
        const reader = new FileReader();
        reader.onloadend = () => resolve(reader.result);
        reader.onerror = () => resolve(null);
        reader.readAsDataURL(blob);
      });
      fontCache.set(url, dataUrl);
      return dataUrl;
    } catch (_e) {
      fontCache.set(url, null);
      return null;
    }
  };

  const getSheetRules = async (sheet) => {
    try {
      return Array.from(sheet.cssRules || []);
    } catch (_e) {
      try {
        const href = sheet.href;
        if (!href) return [];
        const res = await fetch(href);
        if (!res.ok) return [];
        const text = await res.text();
        const styleEl = document.createElement('style');
        styleEl.textContent = text;
        document.head.appendChild(styleEl);
        const rules = Array.from(styleEl.sheet ? styleEl.sheet.cssRules : []);
        styleEl.remove();
        return rules;
      } catch (_e2) {
        return [];
      }
    }
  };

  const extractBoxStyles = async (el) => {
    let s;
    try { s = getComputedStyle(el); } catch (_e) { return ''; }
    const props = [
      'display', 'box-sizing', 'width', 'min-width', 'max-width',
      'height', 'min-height', 'max-height',
      'margin', 'padding', 'border', 'border-radius',
      'background-color', 'box-shadow', 'gap', 'align-items', 'justify-content',
      'position', 'overflow', 'z-index', 'flex-wrap',
    ];
    const pieces = [];
    for (const p of props) {
      const v = s.getPropertyValue(p);
      if (v && v !== 'none' && v !== 'normal' && v !== '0px' && v !== 'auto') pieces.push(p + ':' + v);
    }
    const bg = s.getPropertyValue('background-image');
    if (bg && bg !== 'none') {
      const urls = bg.match(/url\(([^)]+)\)/gi) || [];
      const parts = [];
      for (const m of urls) {
        const u = m.replace(/^url\(/i, '').replace(/\)\s*$/, '').replace(/^['"]|['"]$/g, '').trim();
        if (/^data:/.test(u)) { parts.push("url('" + u + "')"); continue; }
        const du = await getBgImageDataUrl(u);
        parts.push("url('" + (du || u) + "')");
      }
      if (parts.length) pieces.push('background-image:' + parts.join(','));
    }
    return pieces.join(';');
  };

  const getLiveAncestors = (el) => {
    const out = [];
    let p = el.parentElement;
    while (p && p !== document.body) { out.push(p); p = p.parentElement; }
    return out.reverse();
  };

  const liveElsFor = (root) => Array.from(root.querySelectorAll('*'));

  const joinStyles = (parts) => parts.map(function(p) { return String(p || '').trim().replace(/;\s*$/, '') + ';'; }).filter(function(p) { return p !== ';'; }).join(' ');

  const escapeHtml = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

  const buildHtml = async () => {
    const level = clampLevel(opts.level);
    const isSelectorMode = !!targetSel;
    const liveRoot = isSelectorMode ? target : document.body;

    // 1. Clone + strip scripts/styles/noscripts + on* attributes.
    const clone = liveRoot.cloneNode(true);
    clone.querySelectorAll('script, style, noscript').forEach((el) => el.remove());
    const stripOnAttrs = (root) => {
      const els = [root, ...Array.from(root.querySelectorAll('*'))];
      for (const el of els) {
        for (const attr of Array.from(el.attributes)) {
          if (attr.name.toLowerCase().startsWith('on')) el.removeAttribute(attr.name);
        }
      }
    };
    stripOnAttrs(clone);

    // 2. Images.
    const liveImgs = Array.from(liveRoot.querySelectorAll('img'));
    const cloneImgs = Array.from(clone.querySelectorAll('img'));
    for (let i = 0; i < cloneImgs.length; i++) {
      const live = liveImgs[i];
      const srcLive = live ? (live.currentSrc || live.src || '') : (cloneImgs[i].getAttribute('src') || '');
      if (!srcLive || /^data:/.test(srcLive) || /^blob:/.test(srcLive)) continue;
      if (level >= 2) {
        const full = await getImageDataUrl(srcLive);
        if (full) {
          const small = await downscaleDataUrl(full);
          if (small) { cloneImgs[i].setAttribute('src', small); continue; }
          cloneImgs[i].setAttribute('src', full);
          continue;
        }
      } else {
        const dataUrl = await getImageDataUrl(srcLive);
        if (dataUrl) { cloneImgs[i].setAttribute('src', dataUrl); continue; }
      }
    }
    clone.querySelectorAll('img').forEach((img) => img.removeAttribute('srcset'));

    // 3. Background images.
    const liveDesc = liveElsFor(liveRoot);
    const cloneDesc = liveElsFor(clone);
    for (let i = 0; i < cloneDesc.length; i++) {
      const liveEl = liveDesc[i];
      if (!liveEl) continue;
      let bg;
      try { bg = getComputedStyle(liveEl).backgroundImage; } catch (_e) { continue; }
      if (!bg || bg === 'none') continue;
      const matches = bg.match(/url\(([^)]+)\)/gi);
      if (!matches) continue;
      const dataUrls = [];
      for (const m of matches) {
        const u = m.replace(/^url\(/i, '').replace(/\)\s*$/, '').replace(/^['"]|['"]$/g, '').trim();
        if (/^data:/.test(u)) { dataUrls.push(u); continue; }
        const du = await getBgImageDataUrl(u);
        dataUrls.push(du || u);
      }
      const parts = dataUrls.map((d) => "url('" + String(d).replace(/'/g, "\\'") + "')").join(', ');
      cloneDesc[i].style.setProperty('background-image', parts, 'important');
    }

    // 4. Canvas replacement.
    const liveCanvases = Array.from(liveRoot.querySelectorAll('canvas'));
    const cloneCanvases = Array.from(clone.querySelectorAll('canvas'));
    for (let i = 0; i < cloneCanvases.length; i++) {
      const live = liveCanvases[i];
      let dataUrl = null;
      if (live) {
        try { dataUrl = live.toDataURL('image/png'); } catch (_e) { dataUrl = null; }
      }
      let rect = null;
      if (live) { try { rect = live.getBoundingClientRect(); } catch (_e) { rect = null; } }
      const w = rect ? Math.round(rect.width) : 300;
      const h = rect ? Math.round(rect.height) : 150;
      const el = cloneCanvases[i];
      if (dataUrl) {
        const img = document.createElement('img');
        img.src = dataUrl;
        img.alt = 'canvas snapshot';
        img.setAttribute('style', 'max-width:100%;height:auto;display:block');
        el.replaceWith(img);
      } else {
        const div = document.createElement('div');
        div.setAttribute('style', 'width:' + w + 'px;height:' + h + 'px;background:#e5e7eb;border:1px solid #d1d5db;box-sizing:border-box;display:block');
        div.textContent = '[canvas]';
        el.replaceWith(div);
      }
    }

    // 5. Iframe replacement.
    const liveIframes = Array.from(liveRoot.querySelectorAll('iframe'));
    const cloneIframes = Array.from(clone.querySelectorAll('iframe'));
    for (let i = 0; i < cloneIframes.length; i++) {
      const live = liveIframes[i];
      let rect = null;
      if (live) { try { rect = live.getBoundingClientRect(); } catch (_e) { rect = null; } }
      const w = rect ? Math.round(rect.width) : 600;
      const h = rect ? Math.round(rect.height) : 400;
      const div = document.createElement('div');
      div.setAttribute('style', 'width:' + w + 'px;height:' + h + 'px;background:#e5e7eb;border:1px solid #d1d5db;box-sizing:border-box;display:block');
      div.textContent = '[iframe]';
      cloneIframes[i].replaceWith(div);
    }

    // 6. SVG <use> sprites: inline external referenced elements.
    const cloneUses = Array.from(clone.querySelectorAll('use'));
    for (const use of cloneUses) {
      const href = use.getAttribute('href') || use.getAttribute('xlink:href') || '';
      if (!href) continue;
      if (href.startsWith('#') || href.startsWith('data:') || href.startsWith('blob:')) continue;
      try {
        const res = await fetch(href);
        if (!res.ok) continue;
        const text = await res.text();
        const doc = new DOMParser().parseFromString(text, 'image/svg+xml');
        const fragId = href.split('#')[1] || null;
        let refEl = fragId ? doc.getElementById(fragId) : null;
        const svgRoot = doc.querySelector('svg');
        if (!refEl) refEl = svgRoot;
        if (!refEl) continue;
        const holder = document.createElement('svg');
        holder.setAttribute('style', 'display:inline-block;vertical-align:middle');
        if (svgRoot) {
          const vb = svgRoot.getAttribute('viewBox');
          if (vb) holder.setAttribute('viewBox', vb);
          const w = svgRoot.getAttribute('width');
          const h = svgRoot.getAttribute('height');
          if (w) holder.setAttribute('width', w);
          if (h) holder.setAttribute('height', h);
        }
        holder.innerHTML = refEl.outerHTML || '';
        const parent = use.parentElement;
        if (parent) parent.replaceChild(holder, use);
      } catch (_e) { /* leave <use> as-is */ }
    }

    // 7. Form state kept as attributes.
    const liveForms = Array.from(liveRoot.querySelectorAll('input, textarea, select, option, button'));
    const cloneForms = Array.from(clone.querySelectorAll('input, textarea, select, option, button'));
    for (let i = 0; i < cloneForms.length; i++) {
      const cloneEl = cloneForms[i];
      const liveEl = liveForms[i];
      if (!liveEl) continue;
      const tag = cloneEl.tagName.toLowerCase();
      if (tag === 'input') {
        const type = (liveEl.type || 'text').toLowerCase();
        if (type === 'checkbox' || type === 'radio') {
          if (liveEl.checked) cloneEl.setAttribute('checked', '');
          else cloneEl.removeAttribute('checked');
        } else if (type === 'file') {
          cloneEl.removeAttribute('value');
        } else {
          const v = String(liveEl.value ?? '');
          if (v) cloneEl.setAttribute('value', v);
          else cloneEl.removeAttribute('value');
        }
      } else if (tag === 'textarea') {
        cloneEl.textContent = liveEl.value ?? '';
      } else if (tag === 'select') {
        const options = Array.from(cloneEl.querySelectorAll('option'));
        const liveOptions = Array.from(liveEl.querySelectorAll('option'));
        options.forEach((opt, j) => {
          const liveOpt = liveOptions[j];
          if (liveOpt && liveOpt.selected) opt.setAttribute('selected', '');
          else opt.removeAttribute('selected');
        });
      }
    }

    // 8. Used @keyframes names (from live elements).
    const usedKeyframes = new Set();
    for (const el of liveElsFor(liveRoot)) {
      try {
        const name = getComputedStyle(el).animationName;
        if (name && name !== 'none') name.split(',').forEach((n) => { if (n) usedKeyframes.add(n.trim()); });
      } catch (_e) {}
    }

    // 9. Custom properties on :root/html/body.
    const customPropsOn = (el) => {
      const out = [];
      if (!el) return out;
      let s;
      try { s = getComputedStyle(el); } catch (_e) { return out; }
      for (let i = 0; i < s.length; i++) {
        const name = s[i];
        if (name && name.startsWith('--')) {
          const v = (s.getPropertyValue(name) || '').trim();
          if (v) out.push([name, v]);
        }
      }
      return out;
    };
    const rootProps = customPropsOn(document.documentElement);
    const bodyProps = customPropsOn(document.body);

    // 10. Gather CSS rules from all sheets.
    const keptRules = [];
    const fontRules = [];
    const keyframeRules = [];
    const mediaRules = [];
    for (const sheet of Array.from(document.styleSheets)) {
      const rulesEls = await getSheetRules(sheet);
      for (const rule of rulesEls) {
        try {
          if (rule.type === CSSRule.FONT_FACE_RULE) {
            let text = rule.cssText;
            if (level === 0) {
              const replacements = [];
              const re = /url\(([^)]+)\)/gi;
              let m;
              while ((m = re.exec(text))) {
                const u = m[1].replace(/^['"]|['"]$/g, '').trim();
                if (/^(data:|blob:)/.test(u)) continue;
                const du = await getFontDataUrl(u);
                if (du) replacements.push({ from: u, to: du });
              }
              for (const r of replacements) text = text.split(r.from).join(r.to);
            }
            fontRules.push(text);
            continue;
          }
          if (rule.type === CSSRule.KEYFRAMES_RULE) {
            if (usedKeyframes.has(rule.name)) keyframeRules.push(rule.cssText);
            continue;
          }
          if (rule.type === CSSRule.MEDIA_RULE) {
            try {
              if (window.matchMedia(rule.media).matches) mediaRules.push(rule.cssText);
            } catch (_e) {}
            continue;
          }
          if (rule.type === CSSRule.SUPPORTS_RULE) {
            try {
              if (CSS.supports(rule.conditionText)) mediaRules.push(rule.cssText);
            } catch (_e) {}
            continue;
          }
          if (rule.type === CSSRule.STYLE_RULE) {
            const sel = (rule.selectorText || '').trim();
            if (!sel) continue;
            if (/^(html|body|:root|:html|:body)$/i.test(sel)) { keptRules.push(rule.cssText); continue; }
            try {
              if (clone.querySelectorAll(sel).length > 0) keptRules.push(rule.cssText);
            } catch (_e) {}
            continue;
          }
        } catch (_e) {}
      }
    }

    // 11. Compose the CSS.
    let css = '';
    const rootLines = rootProps.map(([k, v]) => k + ':' + v + ';').join(' ');
    if (rootLines) css += ':root, html { ' + rootLines + ' }\n';
    const bodyLines = bodyProps.map(([k, v]) => k + ':' + v + ';').join(' ');
    if (bodyLines) css += 'body { ' + bodyLines + ' }\n';
    for (const t of keptRules) css += t.trim() + '\n';
    for (const t of fontRules) css += t.trim() + '\n';
    for (const t of keyframeRules) css += t.trim() + '\n';
    for (const t of mediaRules) css += t.trim() + '\n';

    // 12. Trim to the largest visible section (level 3, full mode only).
    // Both the live page and the clone drop scripts/styles/noscripts before
    // comparing, so the child lists line up.
    let trimmed = false;
    if (level >= 3 && !isSelectorMode) {
      const liveChildren = Array.from(document.body.children).filter((el) => !/^(SCRIPT|STYLE|NOSCRIPT)$/.test(el.tagName));
      const cloneChildren = Array.from(clone.children).filter((el) => !/^(SCRIPT|STYLE|NOSCRIPT)$/.test(el.tagName));
      if (liveChildren.length > 1 && cloneChildren.length === liveChildren.length) {
        let bestIndex = -1;
        let bestArea = -1;
        for (let i = 0; i < liveChildren.length; i++) {
          let r;
          try { r = liveChildren[i].getBoundingClientRect(); } catch (_e) { continue; }
          if (r.width <= 0 || r.height <= 0) continue;
          const area = r.width * r.height;
          if (area > bestArea) { bestArea = area; bestIndex = i; }
        }
        if (bestIndex >= 0) {
          clone.innerHTML = cloneChildren[bestIndex].outerHTML;
          trimmed = true;
        }
      }
    }

    // 13. Build the shell. The body keeps the original clone's inline style
    // (width/margin/padding from the page are preserved, width is pinned to
    // the viewport) and the clone's content, not its tag.
    const metaViewport = '<meta name="viewport" content="width=' + vw + ', initial-scale=1">';
    const rootInline = rootProps.map(([k, v]) => k + ':' + v + ';').join('');
    const bodyInline = bodyProps.map(([k, v]) => k + ':' + v + ';').join('');
    const titleTag = '<title>' + escapeHtml(title) + '</title>';

    let shellBodyStyle = '';
    let content;
    if (isSelectorMode) {
      const ancestors = getLiveAncestors(target);
      let acc = clone.outerHTML;
      for (let i = ancestors.length - 1; i >= 0; i--) {
        const box = await extractBoxStyles(ancestors[i]);
        acc = '<div class="sprout-ancestor" style="' + box + '">' + acc + '</div>';
      }
      shellBodyStyle = joinStyles([bodyInline, 'width:' + vw + 'px;']);
      content = acc;
    } else {
      const cloneBodyInline = clone.getAttribute('style') || '';
      shellBodyStyle = joinStyles([cloneBodyInline, bodyInline, 'width:' + vw + 'px;']);
      content = clone.innerHTML;
    }

    const html = '<!DOCTYPE html>\n<html style="' + rootInline + 'width:' + vw + 'px;">\n<head>\n' + metaViewport + '\n<meta charset="utf-8">\n' + titleTag + '\n<style>\n' + css + '\n</style>\n</head>\n<body style="' + shellBodyStyle + '">\n' + content + '\n</body>\n</html>';
    return { html: html, trimmed: trimmed };
  };

  // Level-escalation loop: drop font inlining (1), downscale images (2),
  // then keep only the largest visible section (3).
  opts.level = clampLevel(opts.level);
  let best = await buildHtml();
  let html = best.html;
  let trimmed = best.trimmed;
  for (let level = 1; level <= 3; level++) {
    if (bytesOf(html) <= opts.maxBytes) break;
    opts.level = level;
    best = await buildHtml();
    html = best.html;
    trimmed = best.trimmed;
  }

  return JSON.stringify({
    html: html,
    trimmed: trimmed,
    title: title,
    finalUrl: finalUrl,
    viewportWidth: vw,
    viewportHeight: vh,
  });
})();
