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
 *     can navigate plainly wherever fetch is refused.
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
 * source-hash: fnv1a64:a6ab060c6d6088f8
 * version: 1
 */
(function () {
  'use strict';

  var VERSION = '1';

  var NAV_RE = /(?:^|;)\s*to:([a-z0-9]+(?:-[a-z0-9]+)*)/i;
  var STATE_HASH_RE = /^#state=([a-z0-9]+(?:-[a-z0-9]+)*)$/;
  var ZERO_HASH = 'fnv1a64:0000000000000000';

  var screensBaseURL = null;
  var currentStem = null;

  // --- location -----------------------------------------------------------

  // The runtime derives every screen URL from its own <script src>: under
  // file:// that is design/runtime/sprout-screens.js next to design/screens/;
  // under the webui preview the same element carries the rewritten
  // /api/file?path=… URL, so both worlds resolve identically with no
  // environment sniffing.
  function findSelf() {
    var scripts = document.querySelectorAll('script[src]');
    for (var i = scripts.length - 1; i >= 0; i--) {
      if (/sprout-screens\.js(?:[?#]|$)/.test(scripts[i].getAttribute('src'))) {
        return new URL(scripts[i].getAttribute('src'), location.href);
      }
    }
    return null;
  }

  function screensBase(selfURL) {
    // runtime sits at <kit>/runtime/sprout-screens.js; screens at <kit>/screens/.
    return new URL('../screens/', selfURL);
  }

  function stemFromLocation() {
    var pathParam = new URLSearchParams(location.search).get('path');
    if (pathParam) {
      var m = /\/screens\/([a-z0-9]+(?:-[a-z0-9]+)*)\.html$/i.exec(pathParam);
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
    } catch (e) { /* states are URL-hash convenience, never load-bearing */ }
  }

  function pushQuiet(url, stem) {
    try {
      history.pushState({ sprout: VERSION, sproutScreen: stem }, '', url);
      return true;
    } catch (e) { return false; }
  }

  // --- navigation ---------------------------------------------------------

  function standaloneFallback(url) {
    // Fetch unavailable (file:// null origin) or the target is not a screen:
    // get out of the runtime's way and let the browser navigate plainly.
    location.assign(url);
  }

  function applyDocument(doc) {
    var head = document.head;
    while (head.firstChild) head.removeChild(head.firstChild);
    for (var i = 0; i < doc.head.childNodes.length; i++) {
      head.appendChild(document.importNode(doc.head.childNodes[i], true));
    }
    for (var j = doc.documentElement.attributes.length - 1; j >= 0; j--) {
      var attr = doc.documentElement.attributes[j];
      if (attr.name === 'data-sprout-screens') continue; // runtime owns the stamp
      document.documentElement.setAttribute(attr.name, attr.value);
    }
    var body = document.importNode(doc.body, true);
    // Parsed scripts never re-execute, so they are dead weight — drop them.
    var scripts = body.querySelectorAll('script');
    for (var k = 0; k < scripts.length; k++) scripts[k].parentNode.removeChild(scripts[k]);
    document.body.replaceWith(body);
    document.title = doc.title;
  }

  function swapTo(stem, push) {
    if (!screensBaseURL) return;
    var url = new URL(stem + '.html', screensBaseURL).href;
    fetch(url, { redirect: 'error' }).then(function (res) {
      if (!res.ok) throw { sproutStatus: res.status }; // a real answer: stay put
      return res.text();
    }).then(function (text) {
      var doc = new DOMParser().parseFromString(text, 'text/html');
      if (!doc || !doc.body) throw { sproutStatus: 0 };
      applyDocument(doc);
      currentStem = stem;
      document.documentElement.setAttribute('data-sprout-screen-stem', stem);
      if (push && !pushQuiet(url, stem)) {
        location.replace(url); // history refused (opaque origin): load plainly
        return;
      }
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
    var stem = (event.state && event.state.sprout) ? event.state.sproutScreen : stemFromLocation();
    if (stem) swapTo(stem, false);
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
    // One style element for the state toggle; idempotent across swaps (head
    // is replaced wholesale, so re-inject on every boot/swap is the rule).
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

    document.documentElement.setAttribute('data-sprout-screens', VERSION);
    document.addEventListener('click', onClick, true);
    window.addEventListener('popstate', onPopstate);
    window.addEventListener('hashchange', onHashchange);

    injectHiddenRule();
    currentStem = stemFromLocation();
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
