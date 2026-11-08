//go:build !js

package appcheck

import "strings"

// The in-page JavaScript is carried over from the platform's app-probe.mjs:
// reading values from each field's label, judging refusals by the browser's
// own validation state, and the choices a person makes. The driver around it
// (navigation, rounds, budget, recording) is Go — see probe.go.
//
// Each script is evaluated as a statement (webcontent.RodEvalStatement wraps
// it that way), so it runs in page scope with document and window available.

// probeWords are the values typed into text fields. They are words, not
// numbers: an app may rightly refuse digits in a name, and a person's
// entries never contain numbers like that.
var probeWords = []string{"Alfa", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot", "Golf", "Hotel", "India", "Juliett", "Kilo", "Lima"}

// fillFormScript fills the bound form's empty fields with probe values chosen
// from each field's label: a number for a quantity, today for a date, a
// plausible address for an email, and a probe word for a name. Fields that
// collect several values and read-only date fields are left to the hand pass.
const fillFormScript = `(() => {
  const f = document.querySelectorAll('form')[%d] || null;
  const multi = %q;
  const word = %q;
  const today = %q;
  const email = %q;
  if (!f) return 0;
  const multiValue = new RegExp(multi);
  const set = (el, v) => {
    const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype
      : el instanceof HTMLSelectElement ? HTMLSelectElement.prototype
      : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, v);
    el.dispatchEvent(new Event('input', { bubbles: true }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
  };
  const labelOf = (el) => {
    const byFor = el.id && f.ownerDocument.querySelector('label[for="' + el.id + '"]');
    return [el.name, el.id, el.placeholder, el.getAttribute('aria-label'), el.getAttribute('inputmode'),
      (byFor || el.closest('label'))?.textContent].join(' ').toLowerCase();
  };
  let filled = 0;
  for (const el of f.querySelectorAll('input, textarea, select')) {
    if (el.value) continue;
    const t = (el.getAttribute('type') || '').toLowerCase();
    if (el.disabled || el.readOnly || ['hidden', 'submit', 'button', 'checkbox', 'radio', 'file'].includes(t)) continue;
    const hint = labelOf(el);
    if (el.tagName !== 'SELECT' && t !== 'search' && multiValue.test(hint)) continue;
    if (el.tagName === 'SELECT') {
      const opt = [...el.options].find((o) => o.value && !o.disabled);
      if (opt) set(el, opt.value);
      else continue;
    } else if (t === 'number' || t === 'range') set(el, '5');
    else if (t === 'date' || (t !== 'datetime-local' && /\bdate\b|\bday\b/.test(hint) && t !== 'email')) set(el, today);
    else if (t === 'datetime-local') set(el, today + 'T09:30');
    else if (t === 'month') set(el, today.slice(0, 7));
    else if (t === 'time' || /\btime\b/.test(hint)) set(el, '09:30');
    else if (t === 'email' || /e-?mail/.test(hint)) set(el, email);
    else if (t === 'color' || /colou?r/.test(hint)) set(el, '#3366cc');
    else if (t === 'url' || /\burl\b|website|link/.test(hint)) set(el, 'https://example.com');
    else if (t === 'tel' || /phone|\btel\b|mobile/.test(hint)) set(el, '5550100');
    else if (/decimal|numeric/.test(hint) || /\b(rate|price|amount|hours?|qty|quantity|cost|total|count|minutes|duration|budget|age)\b|\$|number of|how many/.test(hint)) set(el, '5');
    else set(el, 'Probe ' + word);
    filled++;
  }
  return filled;
})()`

// handFieldScript is the field lookup the hand pass scripts share: it takes
// the bound form and returns the element the hand pass works on next, or
// null. It embeds the multi-value pattern as %q (filled by the caller).
const handFieldTemplate = `const lookupHandField = (f) => {
  const multi = new RegExp(%q);
  const hintOf = (e) => {
    const byFor = e.id && e.ownerDocument.querySelector('label[for="' + e.id + '"]');
    return [e.name, e.id, e.placeholder, e.getAttribute('aria-label'),
      (byFor || e.closest('label'))?.textContent].join(' ').toLowerCase();
  };
  return (kind) => {
    for (const e of f.querySelectorAll('input:not([type]), input[type=text], input[type=date], textarea')) {
      if (e.disabled || e.offsetParent === null || e.dataset.appCheckDone) continue;
      const hint = hintOf(e);
      if (kind === 'multi' && !e.readOnly && multi.test(hint)) return e;
      if (kind === 'calendar' && e.readOnly && /\bdate\b|\bday\b|\bwhen\b/.test(hint)) return e;
    }
    return null;
  };
};`

// handFieldScript builds the shared field lookup with the multi-value pattern
// embedded.
func handFieldScript(multiPattern string) string {
	return strings.ReplaceAll(handFieldTemplate, "%q", quote(multiPattern))
}

// handScriptsPrefix is the lookup every hand-pass script is bound with; each
// script names the lookup it calls.
func handScriptsPrefix(multiPattern string) string {
	return handFieldScript(multiPattern) + "; "
}

// handFieldsScript counts the fields the hand pass works on: visible text
// fields that collect several values, and visible read-only date fields.
const handFieldsScript = `(() => {
  const f = document.querySelectorAll('form')[%d] || null;
  if (!f) return { multi: 0, calendars: 0 };
  const out = { multi: 0, calendars: 0 };
  for (const e of f.querySelectorAll('input:not([type]), input[type=text], input[type=date], textarea')) {
    if (e.disabled || e.offsetParent === null) continue;
    const byFor = e.id && e.ownerDocument.querySelector('label[for="' + e.id + '"]');
    const hint = [e.name, e.id, e.placeholder, e.getAttribute('aria-label'),
      (byFor || e.closest('label'))?.textContent].join(' ').toLowerCase();
    if (!e.readOnly && new RegExp(%q).test(hint)) out.multi++;
    else if (e.readOnly && /\bdate\b|\bday\b|\bwhen\b/.test(hint)) out.calendars++;
    else continue;
    e.dataset.appCheckCounted = '1';
  }
  return out;
})()`

// typeMultiValueScript types a value into the form's next unused multi-value
// field and leaves it focused; the prober then presses Enter as a real key,
// adding it as a chip or tag. It reports whether it found a field. It takes
// the handFieldScript lookup as its argument.
const typeMultiValueScript = `lookupTypeMultiValue(%d, %q, %s)()`
const typeMultiValueBody = `const lookupTypeMultiValue = (index, word, lookup) => () => {
  const f = document.querySelectorAll('form')[index] || null;
  if (!f) return false;
  const el = lookup(f)('multi');
  if (!el) return false;
  el.dataset.appCheckDone = '1';
  el.focus();
  el.value = word;
  el.dispatchEvent(new Event('input', { bubbles: true }));
  return true;
};
`

// openCalendarScript clicks the form's next read-only date field, which opens
// a calendar. It reports whether it found one. It takes the handFieldScript
// lookup as its argument.
const openCalendarScript = `lookupOpenCalendar(%d, %s)()`
const openCalendarBody = `const lookupOpenCalendar = (index, lookup) => () => {
  const f = document.querySelectorAll('form')[index] || null;
  if (!f) return false;
  const el = lookup(f)('calendar');
  if (!el) return false;
  el.dataset.appCheckDone = '1';
  el.click();
  return true;
};
`

// clickButtonScript clicks the form's n-th button.
const clickButtonScript = `(() => {
  const f = document.querySelectorAll('form')[%d] || null;
  if (!f) return false;
  const b = f.querySelectorAll('button')[%d];
  if (!b) return false;
  b.click();
  return true;
})()`

// clickSubmitScript clicks the form's submit control the way a person does: a
// real sequence of pointer and mouse events at the control's centre, which is
// what the browser's own validation and the app's handlers answer to. A
// control with no clickable box falls back to a plain scripted click.
const clickSubmitScript = `(() => {
  const f = document.querySelectorAll('form')[%d] || null;
  if (!f) return false;
  const c = f.querySelector('button[type=submit], button:not([type]), input[type=submit]');
  if (!c) return false;
  c.scrollIntoView({ block: 'center' });
  const r = c.getBoundingClientRect();
  if (r.width > 0 && r.height > 0) {
    const opts = { bubbles: true, cancelable: true, clientX: r.x + r.width / 2, clientY: r.y + r.height / 2, button: 0 };
    for (const [Ctor, type] of [[PointerEvent, 'pointerdown'], [MouseEvent, 'mousedown'], [PointerEvent, 'pointerup'], [MouseEvent, 'mouseup'], [MouseEvent, 'click']]) {
      c.dispatchEvent(new Ctor(type, opts));
    }
  } else {
    c.click();
  }
  return true;
})()`

// pressButtonScript presses the first visible action button outside a form
// whose label matches exactly, and reports whether it found one.
const pressButtonScript = `(() => {
  const wanted = %s;
  for (const b of document.querySelectorAll('button:not(form button)')) {
    const text = ((b.textContent || '') + ' ' + (b.getAttribute('aria-label') || '')).trim().toLowerCase();
    if (text !== wanted) continue;
    if (b.disabled || b.offsetParent === null) continue;
    b.click();
    return true;
  }
  return false;
})()`

// formLabelScript returns a short label for a form that stays the same
// between rounds: never text that includes the form's own values or options.
const formLabelScript = `(() => {
  const f = document.querySelectorAll('form')[%d] || null;
  if (!f) return 'form';
  const own = (el) => {
    if (!el) return '';
    const c = el.cloneNode(true);
    c.querySelectorAll('input, select, textarea, option').forEach((x) => x.remove());
    return c.textContent.trim().replace(/\s+/g, ' ');
  };
  const before = f.closest('section, article, [role=dialog], main')?.querySelector('h1, h2, h3');
  return String(
    f.getAttribute('aria-label')
    || own(f.querySelector('h1, h2, h3, legend'))
    || own(before && !before.contains(f) ? before : null)
    || own(f.querySelector('button[type=submit], button:not([type]), input[type=submit]'))
    || f.querySelector('input[type=submit]')?.value
    || own(f.querySelector('label'))
    || 'form'
  ).slice(0, 60);
})()`

// blockedScript reports the browser's refusals on a form: a field the app
// refuses still holds what was entered and says why, while a field the app
// took and cleared (a form reset) holds nothing. So a message counts only
// when the field still has its value — which keeps a refused probe value a
// problem and does not turn a working form that clears itself into one.
const blockedScript = `(() => {
  const f = document.querySelectorAll('form')[%d] || null;
  if (!f) return [];
  const out = [];
  for (const e of [...f.elements].filter((e) => e.validationMessage && e.value)) {
    out.push((e.name || e.id || e.type) + ': ' + e.validationMessage);
  }
  for (const e of [...f.querySelectorAll('[aria-invalid="true"]')]) {
    out.push((e.name || e.id || 'field') + ': marked invalid');
  }
  for (const e of [...f.ownerDocument.querySelectorAll('[role="alert"]')]) {
    const t = e.textContent.trim();
    if (t) out.push('alert: ' + t.slice(0, 120));
  }
  return out;
})()`

// chooseScript makes the choices a person makes before submitting: the
// first option of each radio group, then the first choice button inside the
// form (a time slot, a size) that is not destructive, a next step or a
// multi-value field. It reports whether a choice button was pressed, so the
// driver can fill the fields again (a choice can reveal or reset them).
const chooseScript = `(() => {
  const f = document.querySelectorAll('form')[%d] || null;
  if (!f) return { pressed: false };
  const seen = new Set();
  for (const r of f.querySelectorAll('input[type=radio]')) {
    if (r.disabled || seen.has(r.name)) continue;
    seen.add(r.name);
    r.click();
  }
  const destructive = new RegExp(%q);
  const nextStep = new RegExp(%q);
  const multiValue = new RegExp(%q);
  for (const c of f.querySelectorAll('button[type=button], [role=radio], [role=option]')) {
    const text = ((c.textContent || '') + ' ' + (c.getAttribute('aria-label') || '')).trim().toLowerCase();
    if (destructive.test(text) || nextStep.test(text) || multiValue.test(text)) continue;
    if (c.disabled) continue;
    c.click();
    return { pressed: true };
  }
  return { pressed: false };
})()`

// nextStepScript finds a visible, enabled Next/Continue button in a
// multi-step form and describes it by its position among the form's buttons.
const nextStepScript = `(() => {
  const f = document.querySelectorAll('form')[%d] || null;
  if (!f) return -1;
  const nextStep = new RegExp(%q);
  const buttons = [...f.querySelectorAll('button')];
  for (let i = 0; i < buttons.length; i++) {
    const b = buttons[i];
    const text = ((b.textContent || '') + ' ' + (b.getAttribute('aria-label') || '')).trim().toLowerCase();
    if (!nextStep.test(text)) continue;
    if (b.offsetParent === null || b.disabled) continue;
    return i;
  }
  return -1;
})()`

// submitIndexScript returns the position of the form's submit control among
// the form's submit-capable elements, and whether its label marks it as a
// next step (which is not a submit).
const submitIndexScript = `(() => {
  const f = document.querySelectorAll('form')[%d] || null;
  if (!f) return { index: -1, next: false };
  const nextStep = new RegExp(%q);
  const controls = [...f.querySelectorAll('button[type=submit], button:not([type]), input[type=submit]')];
  if (!controls.length) return { index: -1, next: false };
  const c = controls[0];
  const text = ((c.textContent || '') + ' ' + (c.getAttribute('aria-label') || '') + ' ' + (c.value || '')).trim().toLowerCase();
  return { index: 0, next: nextStep.test(text) };
})()`

// actionButtonsScript lists the labels of the buttons outside forms the
// probe may press: a tick, a toggle, "mark done" — never anything that
// deletes, removes or cancels, and never a menu.
const actionButtonsScript = `(() => {
  const destructive = new RegExp(%q);
  const wanted = new RegExp(%q);
  const out = [];
  for (const b of document.querySelectorAll('button:not(form button)')) {
    const text = ((b.textContent || '') + ' ' + (b.getAttribute('aria-label') || '')).trim().toLowerCase();
    if (!text || destructive.test(text) || /menu/.test(text)) continue;
    if (!wanted.test(text)) continue;
    if (b.disabled || b.offsetParent === null) continue;
    out.push(text.slice(0, 40));
  }
  return out;
})()`

// discoverScript returns the same-origin link targets on the page, as route
// paths (no query, no fragment, no trailing slash).
const discoverScript = `(() => {
  const base = BASE_ORIGIN;
  const origin = new URL(base).origin;
  const out = [];
  for (const a of document.querySelectorAll('a[href]')) {
    const href = a.getAttribute('href');
    if (!href || href.startsWith('#') || href.startsWith('mailto:') || href.startsWith('tel:')) continue;
    let u;
    try { u = new URL(href, base + BASE_ROUTE); } catch { continue; }
    if (u.origin !== origin) continue;
    out.push(u.pathname.replace(/\/+$/, '') || '/');
  }
  return out;
})()`

// countFormsScript returns how many forms the page shows.
const countFormsScript = `(() => document.querySelectorAll('form').length)()`

// pageTextScript returns the page's visible text.
const pageTextScript = `(() => (document.body ? document.body.innerText : '') || '')()`

// pickCalendarDayScript clicks the first enabled day in a calendar that
// opened from a read-only date field. It reports whether a day was found.
const pickCalendarDayScript = `(() => {
  const day = document.querySelector('[role=gridcell]:not([aria-disabled=true]) button:not([disabled]), button[role=gridcell]:not([disabled]), [role=dialog] button[name=day]:not([disabled]), .rdp-day:not([disabled])');
  if (!day) return false;
  day.click();
  return true;
})()`
