# TODO

- [ ] SP-032-1d: Manual verification — install + start the daemon, open a web terminal, kick off an agent query, run `systemctl --user stop sprout`. `pgrep -f sprout` (and `pgrep -f gopls` / `pgrep -f bash` from the terminal) returns empty within 15s.
- [ ] SP-039-3c: `Sidebar`, `StatusBar`, `MenuBar` → same.
- [ ] SP-048-4e: Ctrl-R reverse history search — DEFERRED. Most invasive sub-item; requires a real state machine inside the raw-mode read loop with its own keystroke handler, search-buffer rendering, and history filtering. Lift out into a separate small task when ready.
