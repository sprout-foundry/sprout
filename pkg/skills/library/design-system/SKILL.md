---
name: Design System
description: How to make a mockup look designed and on brand — compose from a kit, never write your own styling, and change it in the person's own words. Use whenever someone wants to see a screen, a layout, or "what would X look like".
tools: draw_mockup
---

# Mockups — Fast, On Brand, One File

You are drawing a mockup so the person can react to it. The goal is that a
few seconds after they ask, they are looking at a screen that looks like it
belongs to a product — and then they say "make the button bigger" and see
that too.

**The tool is `draw_mockup`.** It draws `design/mockup.html` with a model
call of its own and streams it, so the first screen paints while the rest is
still being written. Never write the mockup HTML yourself with `write_file`:
a file written through a tool argument appears only when the whole argument
is complete, which is the slow path you are avoiding.

## The rules

1. **Compose from a kit; never write styling.** A kit is a look — palette,
   type scale, spacing, component classes, icons — with a short brief. Name
   a kit and a palette on the document's `<html>`, write markup using the
   kit's classes, and reference icons by name. The tool adds the kit's
   CSS. That is what makes the mockup look designed rather than generated,
   and it is the whole brand story: two mockups in the same kit look like
   one product.
2. **One document, many screens.** Each screen is a
   `<section data-screen="id">`. The document's own runtime switches
   between screens, tabs and forms. Keep every screen in the one file.
3. **A change is a sentence.** Pass the person's words as `change`
   ("bigger photos", "make the button green"). When they pointed at one
   thing, name it in `part`. The tool edits the existing document; a full
   redraw happens only when the edit does not apply.
4. **Show the first screen fast.** The first `<section>` is what they see
   first — make it the screen they asked about, not a splash or a menu.
5. **A mockup is a sketch.** Overwrite it freely, keep it short, and do not
   build a design system around it. When they approve it, the real code is
   written from it in the normal loop.

## What not to do

- Do not write your own CSS, pick your own hex colors, or invent a
  component look — the kit owns all of that.
- Do not split the mockup across files, add a build step, or add runtime
  assets.
- Do not ask for a brief, a token pass, or a validation pass before drawing.
  Draw, show, then react to what they say.
