/*
 * sprout-screens v1 — the SP-143 screen runtime.
 *
 * Fixed asset: copied into trees by the scaffold, referenced from screens as
 * ../runtime/sprout-screens.js (defer). Classic script on purpose — file://
 * pages run under a null origin where ES modules are blocked by CORS, while
 * classic <script src> loads fine — with zero dependencies. The runtime never
 * writes files, never talks to an agent, and never navigates away from the
 * document: its only network act is fetching a sibling screen for the
 * in-place swap; a fetch refusal (file://) falls back to plain browser
 * navigation, while a failed target (404) stays put and reports via
 * document-level events (sprout:navigated / sprout:navfailed /
 * sprout:statechanged).
 *
 * What it owns:
 *   - data-nav: <a data-nav="to:<stem>;trigger:<label>"> clicks swap the
 *     document in place (fetch + head/body/title replace), pushState, and
 *     back/forward just work; the anchor href stays authored so the browser
 *     can navigate plainly wherever fetch is refused. Where the history API
 *     refuses entirely (the preview iframe is an about:srcdoc document —
 *     null origin, both pushState and replaceState throw), a same-document
 *     fragment carries the stem instead: location.hash = ... still writes a
 *     real history entry in a srcdoc page, so back/forward pop and popstate
 *     re-swaps.
 *   - screen URLs derive from the runtime's own <script src>; when that src
 *     is the webui preview proxy form (/api/file?path=…, SP-143 §143.4) the
 *     kit root comes from the decoded path param and both the swap fetch and
 *     the swapped document's relative refs are re-expressed through the same
 *     proxy, in memory.
 *   - states: <html data-states="a,b,c"> declares; [data-state="a"] sections
 *     toggle via the #state=a hash (first declared state is active without
 *     one). The switcher renders only when the preview wrapper has stamped
 *     <html data-sprout-preview> — static renders stay clean.
 *   - API: window.SproutScreens.nav(stem) / .setState(name) / .version.
 *   - stamps: <html data-sprout-screens> is set to the runtime version on
 *     boot; the source-hash below detects hand-edits (self-zeroing recipe:
 *     the recorded digest is fnv1a64 over these bytes with the 16 hex digits
 *     of the source-hash line itself replaced by zeros — recomputable
 *     offline, the SP-140-5 §5f convention adapted to a fixed asset).
 *
 * source-hash: fnv1a64:b2dbf3de3df69eaa
 * version: 1
 */
(function () {
  'use strict';

  var VERSION = '1';

  var NAV_RE = /(?:^|;)\s*to:([a-z0-9]+(?:-[a-z0-9]+)*)/i;
  var STATE_HASH_RE = /^#state=([a-z0-9]+(?:-[a-z0-9]+)*)$/;
  var ZERO_HASH = 'fnv1a64:0000000000000000';
  var PATH_QUERY_RE = /[?&]path=([^&]+)/;

  var screensBaseURL = null;
  var currentStem = null;
  var bootStem = null;

  // --- location -----------------------------------------------------------

  // The runtime derives every screen URL from its own <script src>: under
  // file:// that is design/runtime/sprout-screens.js next to design/screens/;
  // under the webui preview the same element carries the rewritten
  // /api/file?path=… URL, so both worlds resolve identically with no
  // environment sniffing.
  function findSelf() {
    var scripts = document.querySelectorAll('script[src]');
    for (var i = scripts.length - 1; i >= 0; i--) {
      var src = scripts[i].getAttribute('src');
      if (!/sprout-screens\.js(?:[?#]|$)/.test(src)) continue;
      // about:srcdoc pages (the webui preview iframe) have no resolvable
      // base URL, so URL construction throws there; the attribute itself
      // still carries everything needed (its path= query names this file).
      try {
        return new URL(src, location.href);
      } catch (e) {
        return src;
      }
    }
    return null;
  }

  // resolveRel joins `rel` onto the directory `baseDir` (both slash-form
  // paths, baseDir's trailing slash optional), honouring ./ and ../ — the
  // one path algebra the runtime needs in both worlds (URL resolution does
  // it natively for real directories; the proxy world has no directories).
  function resolveRel(baseDir, rel) {
    var segs = [];
    var base = baseDir.split('/');
    for (var b = 0; b < base.length; b++) {
      if (base[b] !== '' && base[b] !== '.') segs.push(base[b]);
    }
    var parts = rel.split('/');
    for (var i = 0; i < parts.length; i++) {
      var seg = parts[i];
      if (seg === '' || seg === '.') continue;
      if (seg === '..') segs.pop();
      else segs.push(seg);
    }
    return segs.join('/');
  }

  // The preview proxy serves any workspace file at one opaque URL
  // (/api/file?path=<workspace path>), so a plain '../screens/' climb off it
  // would resolve against /api/file and break. When the self URL carries a
  // path param, the decoded value is the runtime's real workspace location:
  // the screens dir is re-derived from it and every screen fetch goes back
  // through the same proxy with the path re-rooted on the kit.
  //
  // Under srcDoc the page has no origin at all (about:srcdoc), so the proxy
  // URLs are emitted path-only ("/api/file?path=…") and the browser resolves
  // them against the inherited parent base — the same absolute-path form the
  // preview wrapper writes into the document.
  function screensBase(selfURL) {
    var raw = typeof selfURL === 'string' ? selfURL : selfURL.href;
    var match = PATH_QUERY_RE.exec(raw);
    var proxyPath = match ? decodeURIComponent(match[1]) : null;
    if (proxyPath && /runtime\/sprout-screens\.js$/.test(proxyPath)) {
      var kitRoot = proxyPath.slice(0, -'runtime/sprout-screens.js'.length);
      var screensDir = kitRoot + 'screens/';
      var proxy = function (path) {
        return '/api/file?path=' + encodeURIComponent(path);
      };
      return {
        proxy: true,
        resolve: function (rel) {
          return proxy(resolveRel(screensDir, rel));
        },
        // rewrite maps one relative URL that may climb out of screens/
        // (../generated/tokens.css, ../runtime/chrome.css) to its proxy URL;
        // anything not a workspace-relative URL (absolute, protocol-relative,
        // root-relative — an already-proxied ref —, data:, a fragment) comes
        // back unchanged, so foreign refs keep their meaning.
        rewrite: function (rawValue) {
          var value = (rawValue || '').trim();
          if (!value || /^(?:[a-z][a-z0-9+.-]*:|\/\/|#|\/)/i.test(value)) return value;
          return proxy(resolveRel(screensDir, value));
        },
      };
    }
    // Plain (file://, or any server serving real paths): the directory climb
    // works and resolution is the URL constructor's own; nothing needs
    // re-rooting, so rewrite is the identity.
    var base = new URL('../screens/', selfURL);
    return {
      proxy: false,
      resolve: function (rel) {
        return new URL(rel, base).href;
      },
      rewrite: function (rawValue) {
        return rawValue;
      },
    };
  }

  function stemFromLocation() {
    var search = location.search || '';
    var match = PATH_QUERY_RE.exec(search);
    if (match) {
      var m = /\/screens\/([a-z0-9]+(?:-[a-z0-9]+)*)\.html$/i.exec(decodeURIComponent(match[1]));
      if (m) return m[1];
    }
    var n = /\/screens\/([a-z0-9]+(?:-[a-z0-9]+)*)\.html$/i.exec(location.pathname);
    return n ? n[1] : null;
  }

  function stemFromNav(value) {
    var m = NAV_RE.exec(value || '');
    return m ? m[1].toLowerCase() : null;
  }

  // --- states -------------------------------------------------------------

  function declaredStates() {
    var raw = document.documentElement.getAttribute('data-states') || '';
    var out = [];
    raw.split(',').forEach(function (name) {
      name = name.trim();
      if (name && out.indexOf(name) === -1) out.push(name);
    });
    return out;
  }

  function stateFromHash() {
    var m = STATE_HASH_RE.exec(location.hash);
    return m ? m[1] : null;
  }

  // applyState shows only the [data-state] sections matching `active`;
  // sections without data-state are always visible. The runtime marks
  // hidden sections with its own attribute (not `hidden`) so an author's
  // display utilities can never out-specificity the toggle.
  function applyState(active) {
    var sections = document.querySelectorAll('[data-state]');
    for (var i = 0; i < sections.length; i++) {
      var match = sections[i].getAttribute('data-state') === active;
      if (match) sections[i].removeAttribute('data-sprout-hidden');
      else sections[i].setAttribute('data-sprout-hidden', '');
    }
    updateSwitcher(active);
    dispatch('sprout:statechanged', { state: active });
  }

  function resolveActiveState() {
    var declared = declaredStates();
    var fromHash = stateFromHash();
    if (fromHash && declared.indexOf(fromHash) !== -1) return fromHash;
    return declared.length ? declared[0] : null;
  }

  function setState(name) {
    var declared = declaredStates();
    if (declared.indexOf(name) === -1) return false;
    replaceQuiet('#state=' + name);
    applyState(name);
    return true;
  }

  // --- history ------------------------------------------------------------

  // replaceQuiet/pushQuiet wrap the history API because file:// origins can
  // throw SecurityError on same-document URL rewrites; states are best-effort
  // there and the swap itself already fell back to plain navigation.
  function replaceQuiet(hash) {
    try {
      history.replaceState(history.state, '', hash);
      return true;
    } catch (e) {
      // An about:srcdoc document (the webui preview iframe) throws on every
      // URL-form rewrite — it has no base URL to rewrite against. The one
      // history-writing act that still works there is a same-document hash
      // assignment, so the recorded position degrades to a fragment.
      try {
        location.hash = hash;
        return true;
      } catch (e2) { return false; }
    }
  }

  function pushQuiet(url, stem) {
    try {
      history.pushState({ sprout: VERSION, sproutScreen: stem }, '', url);
      return true;
    } catch (e) {
      // Same srcdoc story as replaceQuiet: a fragment assignment is the only
      // writer left, and unlike replaceState it pushes a real entry — the
      // swap stays history-navigable in the preview.
      return replaceQuiet('#sprout-screen=' + stem);
    }
  }

  // --- navigation ---------------------------------------------------------

  function standaloneFallback(url) {
    // Fetch unavailable (file:// null origin) or the target is not a screen:
    // get out of the runtime's way and let the browser navigate plainly.
    location.assign(url);
  }

  // --- in-place swap --------------------------------------------------------

  // CSS url(…) tokens, quoted or bare; the parens inside a URL must be
  // escaped in CSS, so a lazy scan to the first unescaped `)` is exact
  // enough for generated values.
  var CSS_URL_RE = /url\(\s*(?:'([^']*)'|"([^"]*)"|([^)'"]*))\s*\)/gi;

  // rewriteCSS rewrites only relative url() targets (../icons/x.svg); a
  // whole-stylesheet rewrite would eat selectors, so the substitution is
  // token-by-token and leaves non-relative targets byte-identical.
  function rewriteCSS(css) {
    if (!css) return css;
    return css.replace(CSS_URL_RE, function (whole, sq, dq, bare) {
      var target = sq !== undefined ? sq : dq !== undefined ? dq : bare;
      if (!target || /^(?:[a-z][a-z0-9+.-]*:|\/\/|#)/i.test(target.trim())) return whole;
      return 'url(' + screensBaseURL.rewrite(target) + ')';
    });
  }

  // A swapped document arrives as text; its relative URLs would resolve
  // against the page (or the proxy endpoint), not the file they name. While
  // the DOM is in memory — never the fetched text, never any file — every
  // reference to a sibling kit file is re-rooted through the same resolver
  // the runtime fetches with, so the swap stays one consistent world.
  function rewriteSwappedDoc(doc) {
    if (!screensBaseURL || !screensBaseURL.rewrite) return;
    var pairs = [
      ['link', 'href'],
      ['script', 'src'],
      ['img', 'src'],
      ['source', 'src'],
      ['video', 'src'],
      ['audio', 'src'],
    ];
    for (var p = 0; p < pairs.length; p++) {
      var nodes = doc.querySelectorAll(pairs[p][0] + '[' + pairs[p][1] + ']');
      for (var i = 0; i < nodes.length; i++) {
        var raw = nodes[i].getAttribute(pairs[p][1]) || '';
        var next = screensBaseURL.rewrite(raw);
        if (next) nodes[i].setAttribute(pairs[p][1], next);
      }
    }
    var styles = doc.querySelectorAll('style');
    for (var s = 0; s < styles.length; s++) {
      styles[s].textContent = rewriteCSS(styles[s].textContent || '');
    }
  }

  function applyDocument(doc) {
    rewriteSwappedDoc(doc);
    var head = document.head;
    while (head.firstChild) head.removeChild(head.firstChild);
    for (var i = 0; i < doc.head.childNodes.length; i++) {
      head.appendChild(document.importNode(doc.head.childNodes[i], true));
    }
    for (var j = doc.documentElement.attributes.length - 1; j >= 0; j--) {
      var attr = doc.documentElement.attributes[j];
      // The preview marker is the wrapper's runtime contract for this iframe,
      // not the document's content — a swap must not strip it. The version
      // stamp is re-stamped below by convention.
      if (attr.name === 'data-sprout-screens' || attr.name === 'data-sprout-preview') continue;
      document.documentElement.setAttribute(attr.name, attr.value);
    }
    var body = document.importNode(doc.body, true);
    // Parsed scripts never re-execute, so they are dead weight — drop them.
    var scripts = body.querySelectorAll('script');
    for (var k = 0; k < scripts.length; k++) scripts[k].parentNode.removeChild(scripts[k]);
    document.body.replaceWith(body);
    document.title = doc.title;
    // The toggle rule lives in the head, which applyDocument replaced
    // wholesale — re-inject it or every data-state section shows at once.
    injectHiddenRule();
  }

  function swapTo(stem, push) {
    if (!screensBaseURL) return;
    var url = screensBaseURL.resolve(stem + '.html');
    void push;
    if (typeof fetch !== 'function') {
      // fetch itself is unavailable: the spec'd standalone fallback is plain
      // browser navigation, which works everywhere the document does.
      standaloneFallback(url);
      return;
    }
    fetch(url, { redirect: 'error' }).then(function (res) {
      if (!res.ok) throw { sproutStatus: res.status }; // a real answer: stay put
      return res.text();
    }).then(function (text) {
      var doc = new DOMParser().parseFromString(text, 'text/html');
      if (!doc || !doc.body) throw { sproutStatus: 0 };
      applyDocument(doc);
      currentStem = stem;
      document.documentElement.setAttribute('data-sprout-screen-stem', stem);
      // History refused the rewrite (an opaque origin like about:srcdoc
      // throws on URL-form entries): pushQuiet's fallback records a
      // fragment entry, so back reaches the boot position and popstate
      // re-swaps. Never location.replace the proxy URL — in the preview the
      // document would reload inside srcDoc, which has no persistent URL
      // for it.
      pushQuiet(url, stem);
      applyState(resolveActiveState());
      window.scrollTo(0, 0);
      dispatch('sprout:navigated', { stem: stem });
    }).catch(function (err) {
      if (err && typeof err === 'object' && 'sproutStatus' in err) {
        // The stem does not resolve to a screen document. Navigating would
        // only 404 the document away — stay and let tooling hear about it.
        dispatch('sprout:navfailed', { stem: stem, status: err.sproutStatus });
        return;
      }
      // fetch itself is unavailable (file:// null origin): the spec'd
      // standalone fallback is plain browser navigation, which works there.
      standaloneFallback(url);
    });
  }

  // nav is fire-and-forget; failures report via the document-level
  // sprout:navfailed event (status 0 = malformed stem, not attempted).
  function nav(stem) {
    if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(stem)) {
      dispatch('sprout:navfailed', { stem: stem, status: 0 });
      return;
    }
    swapTo(stem, true);
  }

  function onClick(event) {
    if (event.defaultPrevented || event.button !== 0 ||
        event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    var anchor = event.target.closest ? event.target.closest('a[data-nav]') : null;
    if (!anchor) return;
    var stem = stemFromNav(anchor.getAttribute('data-nav'));
    if (!stem) return;
    event.preventDefault();
    nav(stem);
  }

  function onPopstate(event) {
    // Chrome fires popstate alongside every fragment-navigation hashchange
    // (a history difference that is only a same-document fragment change).
    // The #sprout-screen= fragments the history fallback records drive
    // screen pops here; a #state= pop is screen-preserving (the hashchange
    // listener applies it); everything else is a pop back to the boot
    // position, so the boot screen is restored.
    var m = /^#sprout-screen=([a-z0-9]+(?:-[a-z0-9]+)*)$/.exec(location.hash || '');
    if (m) {
      if (m[1] !== currentStem) swapTo(m[1], false);
      return;
    }
    if (stateFromHash()) return;
    var stem = event.state && event.state.sprout
      ? event.state.sproutScreen
      : stemFromLocation() || bootStem;
    if (stem && stem !== currentStem) swapTo(stem, false);
  }

  function onHashchange() {
    var declared = declaredStates();
    if (!declared.length) return;
    var fromHash = stateFromHash();
    applyState(fromHash && declared.indexOf(fromHash) !== -1 ? fromHash : declared[0]);
  }

  // --- preview-gated state switcher ----------------------------------------

  function updateSwitcher(active) {
    var container = document.getElementById('sprout-state-switcher');
    if (container && container.parentNode) container.parentNode.removeChild(container);
    if (!document.documentElement.hasAttribute('data-sprout-preview')) return;
    var declared = declaredStates();
    if (declared.length < 2) return;
    if (!active) active = resolveActiveState();

    container = document.createElement('div');
    container.id = 'sprout-state-switcher';
    container.style.cssText =
      'position:fixed;left:50%;transform:translateX(-50%);bottom:28px;z-index:2147483640;' +
      'display:flex;gap:6px;padding:5px;border-radius:999px;' +
      'background:rgba(20,20,24,0.82);backdrop-filter:blur(6px);' +
      'font:12px/1 system-ui,-apple-system,"Segoe UI",sans-serif;color:#fff;';
    declared.forEach(function (name) {
      var pill = document.createElement('button');
      pill.type = 'button';
      pill.textContent = name;
      pill.setAttribute('data-sprout-state-pill', name);
      pill.setAttribute('aria-pressed', name === active ? 'true' : 'false');
      pill.style.cssText =
        'appearance:none;border:0;border-radius:999px;padding:5px 12px;cursor:pointer;' +
        'font:inherit;color:inherit;background:' +
        (name === active ? 'rgba(255,255,255,0.92);color:#141418' : 'rgba(255,255,255,0.16)');
      pill.addEventListener('click', function () { setState(name); });
      container.appendChild(pill);
    });
    document.body.appendChild(container);
  }

  // --- boot ----------------------------------------------------------------

  function injectHiddenRule() {
    // One style element for the state toggle; idempotent across swaps (the
    // swapped head is a fresh parse, so re-inject on every boot/swap is the
    // rule).
    var style = document.createElement('style');
    style.id = 'sprout-screens-state-rule';
    style.textContent = '[data-state][data-sprout-hidden]{display:none!important}';
    document.head.appendChild(style);
  }

  function dispatch(type, detail) {
    try {
      document.dispatchEvent(new CustomEvent(type, { detail: detail }));
    } catch (e) { /* CustomEvent constructors are universal in target browsers */ }
  }

  function boot() {
    var self = findSelf();
    if (!self) return; // renamed or inlined copy: no screen-resolution basis
    screensBaseURL = screensBase(self);
    bootStem = stemFromLocation();
    if (!bootStem && screensBaseURL.proxy) {
      // A srcdoc page has no location to read the stem from; the authoring
      // contract stamps it on the body (the base templates ship it). Boot
      // position for back/forward pops in the preview.
      var bodyStem = document.body ? document.body.getAttribute('data-sprout-screen') : null;
      if (bodyStem && /^[a-z0-9]+(-[a-z0-9]+)*$/.test(bodyStem) && bodyStem !== 'REPLACE_WITH_STEM') {
        bootStem = bodyStem.toLowerCase();
      }
    }

    document.documentElement.setAttribute('data-sprout-screens', VERSION);
    document.addEventListener('click', onClick, true);
    window.addEventListener('popstate', onPopstate);
    window.addEventListener('hashchange', onHashchange);

    injectHiddenRule();
    currentStem = bootStem;
    if (currentStem) document.documentElement.setAttribute('data-sprout-screen-stem', currentStem);
    applyState(resolveActiveState());
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', boot);
  } else {
    boot();
  }

  window.SproutScreens = {
    version: VERSION,
    nav: nav,
    setState: setState
  };
})();
