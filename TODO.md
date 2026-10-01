# TODO

Active work tracked here. Each item is a small, independently committable
unit for the workflow automation (~30 min – 2 h). Every item cites its spec
section — read the cited spec before starting. Validation gate for every
item: `make vet && make fmt-check && make lint && make build-all` and
`go test ./...` clean (webui items additionally: `cd webui && npx
prettier --check`).

---

## SP-142 — Chat Mode Lanes (`roadmap/SP-142-chat-mode-lanes.md`)

- [x] **142.1** Server: `Mode` field on chatSession (`""` = legacy, reads
      as code); create stamps the mode; summary/list/switch carry it;
      cross-mode switch → `409 mode_mismatch`; TS mirror update
      (`types/generated.ts`, `services/chatSessions.ts`). Spec: SP-142 §1.
- [x] **142.2** Client: mode-filtered tab strip in `useChatSessionsSync`;
      pin writes scoped to in-mode switches; `mode_mismatch` fallback to
      the mode pin or a fresh session. Spec: SP-142 §2.
- [ ] **142.3** Server: workspace query gate — a query from a second chat
      while another runs in the same client context returns
      `409 workspace_busy` naming the running chat; releases on
      `query_completed`; shared-mode unchanged. Spec: SP-142 §3.
- [ ] **142.4** Client: busy notice in the composer ("send anyway"
      queues behind the running chat; drains on completion; cancel
      clears). Spec: SP-142 §3.
- [ ] **142.5** Design agent panel header: design chat name + scoped
      New Chat affordance. Spec: SP-142 §4.

---

---
