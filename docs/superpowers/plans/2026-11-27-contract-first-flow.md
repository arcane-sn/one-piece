# Contract-first ticket flow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A ticket becomes a contract settled in Business (description + Agent Tasks + QA Test Cases + Plan branching), approved as a whole, then executed in Agent and closed to Done when the QA cases pass. The `qa` column disappears.

**Architecture:** Same Go store/API and React board. The card keeps one `approved` flag and one `content_version`; every change to the contract in Business calls a new `Store.TouchCard`, which bumps the version and revokes approval, so the existing "review is tied to the version" rule now covers the whole contract. Generation moves from Agent/QA to Business and is triggered on Draft → Business. The Run All chain continues from Agent Tasks straight into the QA chain, and finishes in Done.

**Tech Stack:** Go (net/http, SQLite via `internal/store`), React (Vite) in `frontend/`.

**Spec:** `docs/superpowers/specs/2026-11-27-contract-first-flow-design.md`

**Before starting:** the working tree already has uncommitted changes in files this plan touches (`items.go`, `qarun.go`, `review*.go`, `run.go`, `TicketModal.jsx`, `ItemsSection.jsx`, …). Commit or stash them first, and run the plan in a worktree off that commit. The database is wiped (spec decision): delete `backend/one-piece.db` before the final manual check; there is no migration.

## Global Constraints

- Columns, in order: `draft, business, agent, done` (`store.Statuses`). No `qa` status anywhere.
- Approval = a passing review of the **current** `content_version` AND at least one live (non-superseded) Agent Task AND at least one live QA case.
- Contract edits (item create/update/delete/restore/regenerate, branch plan create/update/delete) are only allowed while the card is `business`; in `agent`/`done` they return `409` with `store.ConflictError`.
- Contract edits in Business call `Store.TouchCard`. Run-time writes (`SaveItem`, `SetItemStatus`, `AppendItemLog`, `AddFixTask`, branch status/PR updates by runs) never touch it.
- Moving to `agent` requires `approved`. Moving Agent/Done → Business revokes approval (via `TouchCard`). `done` is reachable only from `agent`.
- QA cases are requirement-level (behaviour a user can observe, each citing its requirement); concrete steps are resolved at run time.
- Autopilot rounds stay `autopilotRounds = 3`.
- Reviewer replies and UI copy keep the project's existing English UI strings.

## Review Focus

- Editing an item through the API while the card is in Agent: expect 409, nothing changes.
- Editing a QA case or the branch plan in Business after a passing review: approval is revoked and the old review no longer approves.
- A `[FIX]` task added by autopilot, a run log line, or a branch status change during a run must NOT revoke approval.
- Run All finishing the agent tasks when the ticket has zero QA cases: must not move to Done.
- Dragging Agent → Business while a Run All chain is running: refused until the run is stopped.
- Generation job finishing after the card was moved out of Business or deleted: must not write items into an approved ticket.
- Dragging Business → Done directly: refused.

---

### Task 1: Statuses and move rules

**Files:**
- Modify: `backend/internal/store/cards.go` (`Statuses` line 32, `checkMove` ~line 339, `ListCards` ORDER BY line 183, `MoveCard` after the `draft` approval reset ~line 327)
- Modify: `backend/internal/store/draft_test.go` (the `[]string{"agent","qa","done"}` loop at line 22)
- Test: `backend/internal/store/cards_test.go` (add), `backend/internal/store/draft_test.go`

**Interfaces:**
- Produces: `store.Statuses = []string{"draft","business","agent","done"}`; `MoveCard` returns `ConflictError` for the new refusals.

- [ ] **Step 1: Write failing tests** in `cards_test.go` using `newCard(t)` (card starts in Draft; move it to `business` first):
  - `TestMoveToAgentNeedsApproval`: Business → agent returns `isConflict`; after `s.SetApproved`-style setup (set `approved=1` with `s.DB.Exec`) it succeeds.
  - `TestDoneOnlyFromAgent`: Business → done is a conflict; Agent → done succeeds and keeps `Approved == true`.
  - `TestBackToBusinessRevokesApproval`: approved card in agent moved to business has `Approved == false` and `ContentVersion` increased by 1.
  - `TestQAStatusIsGone`: `MoveCard(id, "qa", 0)` returns `ValidationError`.
- [ ] **Step 2: Run** `cd backend && go test ./internal/store -run 'TestMoveToAgent|TestDoneOnly|TestBackToBusiness|TestQAStatusIsGone' -v` → FAIL.
- [ ] **Step 3: Implement.** Remove `"qa"` from `Statuses`; in `checkMove` add the `agent`-needs-`c.Approved` and `done`-only-from-`agent` cases; in `MoveCard`, when `c.Status` is `agent` or `done` and `status == "business"`, call `touchCard(tx, id)`. Fix the `ListCards` ORDER BY to `draft 0, business 1, agent 2, else 3`. Update the loop in `draft_test.go` to `{"agent","done"}` (it expects Draft → those to fail).
- [ ] **Step 4: Run** `go test ./internal/store` → PASS (other store tests that moved cards into `qa` must be updated to `agent`; fix compile/logic fallout here).
- [ ] **Step 5: Commit** `git commit -am "feat: remove QA column; agent needs approval; done only from agent"`.

### Task 2: Contract approval gate and `TouchCard`

**Files:**
- Modify: `backend/internal/store/reviews.go` (`SetApproved` ~line 90)
- Modify: `backend/internal/store/flows.go` (add exported wrapper next to `touchCard`)
- Test: `backend/internal/store/reviews_test.go` (`TestApprovalGate` line 87 needs items; add new tests)

**Interfaces:**
- Produces: `func (s *Store) TouchCard(cardID int64) error` (bump `content_version`, `approved=0`; `ErrNotFound` if missing). `SetApproved` additionally returns `ConflictError{"Approval needs at least one Agent Task and one QA Test Case."}` when either kind has no live item.

- [ ] **Step 1: Write failing tests:** `TestApproveNeedsTasksAndCases` (passing review recorded via the existing helper in `TestApprovalGate`, no items → conflict; with one agent and one qa item → ok; with only superseded items → conflict) and `TestTouchCardRevokesApproval` (approve, `TouchCard`, `GetCard` shows `approved == false`, version +1, `SetApproved(true)` conflicts until a new passing review at the new version).
- [ ] **Step 2: Run** `go test ./internal/store -run 'TestApproveNeeds|TestTouchCard' -v` → FAIL.
- [ ] **Step 3: Implement** `TouchCard` as a one-transaction call to `touchCard`; add the items check in `SetApproved` after the review check, counting `card_items` with `superseded=0` per kind.
- [ ] **Step 4: Update `TestApprovalGate`** (and any test approving a card, e.g. in `internal/api`) to create one agent and one qa item first. Run `go test ./...` → PASS.
- [ ] **Step 5: Commit** `git commit -am "feat: approval needs tasks and QA cases; Store.TouchCard"`.

### Task 3: Review covers the whole contract

**Files:**
- Modify: `backend/internal/store/reviewchat.go` (`ReviewCriteria` line 161)
- Modify: `backend/internal/api/reviewchat.go` (`reviewChatSystem` checklist text and verdict format lines; `reviewChatPrompt` OP subcommand list)
- Modify: `backend/cmd/server/agentcli.go` (`ticket-show` case line 154; `parseChecklist` if it hard-codes keys; header comment)
- Modify: `frontend/src/components/ReviewChecklist.jsx` (`LABEL`)
- Test: `backend/internal/store/reviewchat_test.go`, `backend/cmd/server/` agentcli test if one exists (otherwise add `TestTicketShowPrintsContract` beside the other CLI tests)

**Interfaces:**
- Produces: `ReviewCriteria` keys in order: `description, cases, tasks, qa, edge, scope, flows, open` (`dod` is removed). Labels: tasks "Every requirement is covered by an Agent Task", qa "Every QA case traces to a requirement, and every requirement has one", open "No open questions or TBD anywhere".
- `ticket-show` additionally prints the live Agent Tasks and QA cases (title + body) and the branch plan (repo, base branch, feature branch).

- [ ] **Step 1: Write failing tests:** a checklist missing `tasks` is rejected (`the checklist is missing: tasks`); a checklist with the old `dod` key is rejected (`unknown or repeated checklist key: dod`); `ticket-show` output contains a created Agent Task title, a QA case title and the planned branch repo.
- [ ] **Step 2: Run** the new tests → FAIL.
- [ ] **Step 3: Implement.** Update `ReviewCriteria` and `ReviewChecklist.jsx` `LABEL` identically. In `reviewChatSystem` replace the six-criteria list by the eight, defining: `tasks` — each requirement in the description has an Agent Task, and tasks name areas that exist in the product folders (Claude checks with Read/Grep/Glob); `qa` — two-way traceability, cases describe observable behaviour; `open` — no open questions, "TBD", or unresolved choices in description, tasks, QA cases or branch plan. Update both verdict-format lines (`description, cases, tasks, qa, edge, scope, flows, open`). The reviewer may only *report* gaps in tasks/QA/branches (it keeps its existing edit commands for the description and flows); say so in the system prompt. Extend `ticket-show` using `st.ListItems` (skip `Superseded`) and `st.ListBranches`.
- [ ] **Step 4: Run** `go test ./...` → PASS; run `cd frontend && npm run build` to confirm the label change compiles.
- [ ] **Step 5: Commit** `git commit -am "feat: review checks tasks, QA cases and open questions"`.

### Task 4: Contract lock and touch-on-edit (API)

**Files:**
- Modify: `backend/internal/api/items.go` (`createItem` line 82, `updateItem` 103, `deleteItem` 146, and the handler that calls `SetSuperseded` — find with `grep -n SetSuperseded backend/internal/api/*.go`)
- Modify: `backend/internal/api/branches.go` (`planBranch` 47, `updateBranch` 66, `deleteBranch` 89)
- Modify: `backend/internal/api/cards.go` (`moveCard` line 65)
- Test: `backend/internal/api/items_test.go`, `backend/internal/api/branches_test.go`, `backend/internal/api/cards_test.go` (use the existing `newTestServer` helper in `auth_test.go`)

**Interfaces:**
- Produces: `func (s *server) editContract(w http.ResponseWriter, cardID int64) bool` — loads the card; if `Status` is `agent` or `done` it writes the 409 `ConflictError{"The ticket is approved and running. Move it back to Business to change the plan."}` and returns false; otherwise returns true. After a successful edit the handler calls `s.st.TouchCard(cardID)`.
- `moveCard` returns 409 `"Stop the run first."` when moving to `business` while `s.chains[id]` exists (check under `s.mu`).

- [ ] **Step 1: Write failing tests:** (a) PUT an item of a card in `agent` → 409, title unchanged; same for POST item, DELETE item, restore, POST/PUT/DELETE branches. (b) In Business, PUT an item of an approved card → card `approved == false`, `contentVersion` +1. (c) `SaveItem`-path run status change and branch status change in `agent` leave `approved` true (call the store methods directly and assert). (d) move Agent → Business with a chain registered (`s.chains[id] = cancel`) → 409.
- [ ] **Step 2: Run** `go test ./internal/api -run 'Contract|Lock' -v` → FAIL.
- [ ] **Step 3: Implement** `editContract` and call it first in the six handlers above; call `TouchCard` after success. Update `TestItemsCRUDDoesNotTouchContentVersion` in the store tests only if it calls API handlers (store methods themselves stay version-neutral, so it should still pass).
- [ ] **Step 4: Run** `go test ./...` → PASS.
- [ ] **Step 5: Commit** `git commit -am "feat: lock the contract in Agent, touch it on Business edits"`.

### Task 5: Generate Agent Tasks and QA cases in Business

**Files:**
- Modify: `backend/internal/api/items.go` (`generateItems` gate lines 223-230, `qaSystemPromptGen` text, the `SupersedeAndAdd` callsite ~line 304)
- Modify: `backend/internal/api/autopilot.go` (replace `autoGenerateQA` with `autoGenerateContract`)
- Modify: `backend/internal/api/cards.go` (`moveCard`: after a successful Draft → Business move, `go s.autoGenerateContract(id)`)
- Test: `backend/internal/api/generate_test.go`, `backend/internal/api/autopilot_test.go`

**Interfaces:**
- Produces: `func (s *server) autoGenerateContract(cardID int64)` — if the card has no live Agent Task, starts agent generation through `s.generateItems` (httptest pattern from the old `autoGenerateQA`), waits for that job to leave `running` (poll `s.st.GetJob(job.ID)` every 2s, give up after 15 min), then does the same for `qa` if the card has no live QA case. Logs through `s.logCard` on failure. Both generations only run while the card is still `business`.
- `generateItems` gate: `Status` must be `business` for both kinds (error text: `Generate the plan while the ticket is in Business.`); the `Approved` requirement is removed. Inside the job, before `SupersedeAndAdd`, re-read the card and abort with an error if `Status != "business"`; after it, call `s.st.TouchCard(id)`.

- [ ] **Step 1: Write failing tests** (stub Claude the way `generate_test.go` already does): generate on a Draft or Agent card → 409; generate on an unapproved Business card succeeds and leaves the card unapproved with items present; an approved card whose items are regenerated is revoked (`approved == false`); `autoGenerateContract` on a fresh Business card produces both kinds, and is a no-op for a kind that already has live items; the job completing after the card moved to `agent` writes no items.
- [ ] **Step 2: Run** `go test ./internal/api -run 'Generate|Contract' -v` → FAIL.
- [ ] **Step 3: Implement** as specified. In `qaSystemPromptGen` state: cases describe observable behaviour per requirement, name the requirement each covers, and do not invent selectors or click paths for code that does not exist yet (it may still read the repo to ground cases in screens that exist today). Remove the now-wrong sentence "Agent Tasks come from an approved ticket; QA Test Cases only once the ticket is in the QA column" in the handler's doc comment.
- [ ] **Step 4: Run** `go test ./...` → PASS.
- [ ] **Step 5: Commit** `git commit -am "feat: generate tasks and QA cases in Business"`.

### Task 6: Agent run flows into QA and ends in Done

**Files:**
- Modify: `backend/internal/api/run.go` (`advanceToQA` line 459 and its call in `runAll` ~line 233; the `runnable` QA gate stays)
- Modify: `backend/internal/api/autopilot.go` (`finishTicket`, the "stays in QA" log text)
- Test: `backend/internal/api/run_test.go`, `backend/internal/api/autopilot_test.go`

**Interfaces:**
- Produces: `func (s *server) continueToQA(ctx context.Context, cardID int64, runList func([]store.Item) bool)` replacing `advanceToQA`. If every live Agent Task is done and at least one live QA case exists, it runs the To Do QA cases with `runList` and then `s.qaAutopilot(ctx, cardID, runList)` inside the same chain (no second `runAll`, so no "already running"). If there are no QA cases it logs `No QA cases: the ticket stays in Agent for you.` and does nothing else.
- `finishTicket` now requires `c.Status == "agent"` and moves to `done`.
- Autopilot stop message: `... the ticket stays in Agent for you.`

- [ ] **Step 1: Write failing tests** with the existing fake runner in `run_test.go`: all tasks done + QA cases pass → card ends in `done`; a failing QA case → `[FIX]` task added, card stays `agent` and `approved` stays true; zero QA cases → card stays `agent`; the card never has status `qa` at any point.
- [ ] **Step 2: Run** `go test ./internal/api -run 'Run|Autopilot' -v` → FAIL.
- [ ] **Step 3: Implement.** In the `runAll` goroutine, after the agent `runList(todo)` succeeds, call `s.continueToQA(ctx, id, runList)`; keep the existing `in.Kind == "qa"` branch for a manual QA run. Delete `advanceToQA` and its Technical-note log line; adjust the log wording to Agent.
- [ ] **Step 4: Run** `go test ./...` → PASS.
- [ ] **Step 5: Commit** `git commit -am "feat: Run All continues into QA and closes to Done"`.

### Task 7: Frontend: four columns, Business-time plan, lock

**Files:**
- Modify: `frontend/src/pages/Board.jsx` (`COLUMNS` line 18), `frontend/src/pages/Dashboard.jsx` (`STATUSES` line 17), `frontend/src/components/TicketModal.jsx` (`STATUS_LABEL` line 17), `frontend/src/components/CommandPalette.jsx` (`STATUS` map if it has `qa`), `frontend/src/index.css` (drop `qa` column/pill colours)
- Modify: `frontend/src/components/ItemsSection.jsx` (`showQA` line 98, the `!card.approved && items.length === 0` placeholder line 99, buttons gated on `card.approved` at lines 123 and 128)
- Modify: `frontend/src/components/BranchingSection.jsx`, `frontend/src/components/ItemModal.jsx` (read-only when the card is `agent`/`done`)

- [ ] **Step 1: Columns/labels.** Remove the `qa` entries in the files above. Keep Discuss available in Agent (already shown for non-Draft cards; verify, don't change).
- [ ] **Step 2: ItemsSection.** Show both lists whenever the card is not Draft. In Business show Generate (per kind, enabled when no job is running) and edit controls; show Run All / Run buttons only when the card is `agent` and approved; in `agent`/`done` hide edit, delete, restore, Generate and make `ItemModal` fields read-only (status and log stay visible). Replace the "approve the ticket to generate" placeholder with "Tasks and QA cases appear here when the ticket moves to Business." for Draft.
- [ ] **Step 3: Approve button** in `TicketModal.jsx`: when the API returns the 409 for missing tasks/cases, show its message via the existing toast; no extra client rule.
- [ ] **Step 4: Build and click-through.** `cd frontend && npm run build` → success. Then `rm backend/one-piece.db` (spec: wipe), `./start.sh`, and in the browser: create a ticket → Draft → Business (tasks and QA cases appear) → review passes → Approve → drag to Agent → Run All → QA runs → Done. Also: edit a QA case in Business and confirm "Approved" disappears; try dragging Business → Done (refused) and Agent → Business (approval removed).
- [ ] **Step 5: Commit** `git commit -am "feat(ui): four columns, contract in Business, locked in Agent"`.

### Task 8: Leftover sweep and docs

**Files:** `README.md`, anywhere `qa` status remains.

- [ ] **Step 1:** `grep -rniE "status ?(===|==|!=|=) ?['\"]qa|moved to QA|QA column|'qa'.*status" backend frontend/src README.md` — every hit that refers to a column/status is fixed; hits that refer to the item kind `"qa"` stay.
- [ ] **Step 2:** Update the flow description in `README.md` to the four columns and the contract rule.
- [ ] **Step 3:** `cd backend && go vet ./... && go test ./...` and `cd ../frontend && npm run build` → all green.
- [ ] **Step 4: Commit** `git commit -am "docs: contract-first flow"`.

---

## Self-review notes

- Spec coverage: columns (T1, T7); generate in Business on Draft→Business (T5); QA requirement-level (T5 prompt); contract version/revoke on edit (T2, T4); review covers contract (T3); Agent lock (T4, T7); Run All → QA → `[FIX]` ≤3 → Done (T6); return to Business revokes approval (T1, T4); no migration (header); Ship unchanged (not touched).
- Out of scope per owner: Preflight, Blockers, partial re-review.
- Names used across tasks: `Store.TouchCard`, `editContract`, `autoGenerateContract`, `continueToQA`, `finishTicket`, review keys `tasks, qa, open`.
- Open risk for the executor: `parseChecklist` in `agentcli.go` and the QA-related counters in `ListCards` (`QATotal/QARan/QAPassed`) were not read in full; confirm they do not hard-code `dod` or the `qa` status.
