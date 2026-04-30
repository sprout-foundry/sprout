export class Terminal {
  open() {}
  write() {}
  writeln() {}
  focus() {}
  blur() {}
  dispose() {}
  refresh() {}
  buffer = { active: { lines: [], length: 0, viewportY: 0, baseY: 0, cursorX: 0, cursorY: 0 } };
  cols = 80;
  rows = 24;
  element = null;
}
