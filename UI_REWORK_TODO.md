### Ledit UI Rework TODO

 - [x] Scaffold a TUI mode using a robust terminal UI toolkit
  - **Goal**: Provide a visually rich experience leveraging terminal buffers instead of plain prints
  - **Plan**: Introduce Bubble Tea-based TUI scaffold with header, body panes, footer, and keybindings
  - **Deliverable**: `ledit ui` command that launches a basic dashboard shell

- [x] Introduce a global `--ui` flag and `LEDIT_UI=1` env var
  - **Goal**: Enable TUI for existing commands without changing their semantics by default
  - **Plan**: Add persistent flag to `root` and a simple switch in long-running commands to spawn TUI

- [x] Define a UI abstraction layer
  - **Goal**: Replace scattered `fmt.Print*` with an interface that can route to stdout or the TUI
  - **Plan**: Create `pkg/ui/output.go` with interfaces: `OutputSink`, `StdoutSink`, `TuiSink`

- [x] Event bus for progress updates
  - **Goal**: Stream state from orchestrators/workers to UI reactively
  - **Plan**: Define `pkg/ui/events.go` with typed events (header, progress, log, table rows, footer)

- [x] Wire TUI to the orchestrator
  - **Goal**: Visualize multi-agent progress (status, step, tokens, cost) in a table that updates live
  - **Plan**: Publish progress events from `pkg/orchestration/*` and render in a Bubble Tea table/panels

- [ ] Gradual migration of prints to UI layer
  - **Goal**: Centralize messages and avoid double-printing
  - **Plan**: Start with high-traffic areas: filesystem writes, editor updates, context builder prompts

- [ ] Add basic theming and compact/detailed layouts
  - **Goal**: Allow dense or verbose rendering depending on terminal size
  - **Plan**: Use lipgloss styles; auto-detect width/height and adapt layout

- [ ] Keyboard interactions
  - **Goal**: Provide fast navigation and toggles (q to quit, tab to switch panes, v to toggle verbosity)
  - **Plan**: Minimal keymap now, extend later

- [ ] Configuration
  - **Goal**: Persist UI preferences
  - **Plan**: Extend config with `ui.enabled`, `ui.theme`, `ui.compact`

- [ ] Docs
  - **Goal**: Document TUI usage and flags
  - **Plan**: Add README section and `--help` examples


