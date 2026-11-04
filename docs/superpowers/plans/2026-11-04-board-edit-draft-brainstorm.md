# Board Edit, Draft Column, Brainstorming Chat Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the owner edit a board from its page, park rough tickets in a new Draft column, and plan a whole product with Claude in a per-board chat that keeps one versioned Markdown plan and creates Draft cards.

**Architecture:** Draft is a fifth card status guarded in the store. The chat runs `claude -p --resume` per board; context is a small ticket index plus read-on-demand subcommands of the server binary (`ticket`, `tickets`, `draft-add`, `draft-update`) that share the `store` package. The plan is a per-board file Claude edits; the server snapshots versions after each turn. The frontend polls like the existing review log.

**Tech Stack:** Go (net/http, modernc sqlite), React 19 + Vite (no frontend test runner: verify with `npm run build`, `npx oxlint src`, and the manual checks written in each task).

**Spec:** `docs/superpowers/specs/2026-11-04-board-edit-draft-brainstorm-design.md`

## Global Constraints

- Server only listens on loopback; keep `ListenAddrOK` untouched.
- UI text in English. Statuses in order: `draft, business, agent, qa, done`.
- Brainstorm limits: turn timeout 5 minutes, `--max-turns 12`, one running turn per board (second send → 409).
- Claude may only write Draft cards of the session's board and only the board's plan file; board folders are read-only (`--add-dir`).
- Keep the `claude` system prompt text identical between turns (prompt cache); changes travel in the user message.
- Tests must never start the real `claude` (stub in `backend/internal/api/main_test.go`).
- Every commit message ends with `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`.

## Review Focus

- Saving a Draft card in the ticket modal must not start a review (`updateCard` calls `reviewQuietly`). Pinned in Task 3.
- Dragging a card to Draft while its review runs → 409, card unchanged. Pinned in Task 2.
- A Stop or crash mid-turn, then another message: the stored session id may be unusable; the next turn must fall back to a fresh session instead of erroring forever. Pinned in Task 6.
- Binary path or board folder containing spaces must not break `--allowedTools` or `--add-dir`. Verified by the spike in Task 6.
- Board deleted while a turn runs, and board with no plan yet (Plan tab shows "No plan yet", no crash). Pinned in Task 6.

---

### Task 1: Edit board from the board page

**Files:**
- Modify: `frontend/src/pages/Board.jsx`

**Interfaces:**
- Consumes: `BoardModal({initial, onSaved(board), onClose})` and `PUT /api/boards/{id}` (both exist).
- Produces: an "Edit Board" button in the board header.

- [ ] **Step 1:** In `Board.jsx` add `const [editing, setEditing] = useState(false)`; render `<button onClick={() => setEditing(true)}>Edit Board</button>` inside `.page-head` (after the `.chips` div, same style class as other header buttons in `index.css`; add a rule only if none fits).
- [ ] **Step 2:** Render `{editing && <BoardModal initial={board} onSaved={(b) => { setBoard(b); setEditing(false); reloadBoards() }} onClose={() => setEditing(false)} />}`; import `BoardModal`.
- [ ] **Step 3: Verify.** `cd frontend && npm run build && npx oxlint src` → build succeeds, no new warnings. Manual: run `./start.sh`, open a board, Edit Board, rename + add a folder, Save → header name and chips update without reload; the sidebar shows the new name.
- [ ] **Step 4: Commit** `feat(ui): edit board from the board page`.

### Task 2: Draft status in the store

**Files:**
- Modify: `backend/internal/store/cards.go` (`Statuses`, `ListCards` ordering, `MoveCard`), `backend/internal/store/reviews.go` (`SetApproved`)
- Test: `backend/internal/store/cards_test.go`, `backend/internal/store/reviews_test.go`

**Interfaces:**
- Produces: `Statuses = {"draft","business","agent","qa","done"}`; `MoveCard` rules below; `Board.Counts` gains a `draft` key automatically (`zeroCounts` iterates `Statuses`).
- `status` has no CHECK constraint in `store.go`, so no migration is needed.

- [ ] **Step 1: Write failing tests** (use `threeCards` / `openTemp` helpers):
  - `TestListCardsOrdersDraftFirst`: move card a to draft; `ListCards` returns a before b, c.
  - `TestMoveFromDraftOnlyToBusiness`: draft→agent, draft→qa, draft→done each return `ConflictError`; draft→business succeeds.
  - `TestMoveToDraftOnlyFromBusiness`: agent→draft returns `ConflictError`; business→draft succeeds and resets `Approved` to false but keeps flows/reviews (assert `LatestReview` still found).
  - `TestMoveToDraftBlockedWhileReviewRuns`: `StartReview` on a business card, then move to draft → `ConflictError`, card still `business`.
  - `TestSetApprovedRejectsDraft` (reviews_test.go): approving a draft card → `ConflictError`.
- [ ] **Step 2:** `cd backend && go test ./internal/store -run 'Draft' -v` → FAIL.
- [ ] **Step 3: Implement.** Update `Statuses` and the `CASE status` ordering in `ListCards` (draft=0). In `MoveCard` (inside the tx, after loading the card): reject per the rules above with `ConflictError{"..."}`; when `status=="draft"` set `approved=0` in the same tx. In `SetApproved`, return `ConflictError{"Draft tickets can't be approved. Move it to Business first."}` when `c.Status=="draft"`.
- [ ] **Step 4:** `go test ./internal/store` → PASS.
- [ ] **Step 5: Commit** `feat(store): Draft status with move rules`.

### Task 3: Draft in API and board UI

**Files:**
- Modify: `backend/internal/api/review.go` (`startReview`), `backend/internal/api/cards.go` (`reviewQuietly`), `frontend/src/pages/Board.jsx` (`COLUMNS`), `frontend/src/components/Column.jsx`, `frontend/src/components/TicketModal.jsx`
- Test: `backend/internal/api/cards_test.go`, `backend/internal/api/review_test.go`

**Interfaces:**
- Consumes: Task 2 store rules.
- Produces: `startReview(c)` returns `store.ConflictError` for a draft card; `reviewQuietly` silently skips drafts.

- [ ] **Step 1: Write failing tests:** `TestReviewDraftCardIs409` (POST `/api/cards/{id}/review` on a card moved to draft → 409); `TestUpdateDraftCardStartsNoReview` (PUT a draft card, then `GET /api/cards/{id}/review` → `null`); `TestMoveDraftToAgentIs409` through `/move`.
- [ ] **Step 2:** `go test ./internal/api -run Draft -v` → FAIL.
- [ ] **Step 3: Implement** the two guards in `startReview` / `reviewQuietly` by checking `c.Status == "draft"`.
- [ ] **Step 4: Frontend.** `COLUMNS` gets `['draft','Draft','#9A8F86']` first. `Column.jsx` empty text for draft: title "No Drafts Yet", hint "Brainstorm a plan to fill this." `TicketModal.jsx`: `STATUS_LABEL.draft = 'Draft'`; when `current?.status === 'draft'` hide `FlowsSection`, `ItemsSection`, `ReviewComment`, and `ReviewSide`. Check where board counts are rendered (`grep -rn counts frontend/src`) and include Draft if statuses are listed there.
- [ ] **Step 5: Verify.** `go test ./...`; `npm run build && npx oxlint src`. Manual: drag a Business card to Draft (Approved chip disappears), open it (no review panel), drag back to Business (review/flows still there), try dragging Draft → Agent (error shown, card returns).
- [ ] **Step 6: Commit** `feat: Draft column with review/approve gates`.

### Task 4: Brainstorm storage

**Files:**
- Modify: `backend/internal/store/store.go` (schema, WAL pragma, `Store.Dir`)
- Create: `backend/internal/store/brainstorm.go`
- Test: `backend/internal/store/brainstorm_test.go`

**Interfaces — Produces** (all on `*Store`):
```go
type BrainstormMessage struct{ ID int64 `json:"id"`; Role string `json:"role"` /*user|assistant*/; Text string `json:"text"`; Status string `json:"status"` /*done|running|stopped|failed*/ }
type BrainstormState struct{ SessionID, SentIndex string }
type PlanVersion struct{ Version int `json:"version"`; CreatedAt string `json:"createdAt"`; Content string `json:"content,omitempty"` }
AddBrainstormMessage(boardID int64, role, text, status string) (BrainstormMessage, error)
AppendBrainstormText(id int64, text string) error            // joins with "\n\n" when Text non-empty
FinishBrainstormMessage(id int64, status string) error
ListBrainstormMessages(boardID int64) ([]BrainstormMessage, error)
RunningBrainstormTurn(boardID int64) (bool, error)            // any assistant message with status running
FailRunningBrainstorm() error                                  // startup sweep: running → stopped
GetBrainstormState(boardID int64) (BrainstormState, error)     // zero value when none
SetBrainstormState(boardID int64, st BrainstormState) error
AddPlanVersion(boardID int64, content string) (PlanVersion, error)   // Version = max+1
ListPlanVersions(boardID int64) ([]PlanVersion, error)               // newest first, Content empty
GetPlanVersion(boardID int64, v int) (PlanVersion, error)            // ErrNotFound
LatestPlanVersion(boardID int64) (PlanVersion, error)                // ErrNotFound when none
PlanPath(boardID int64) string                                       // <Dir>/plans/board-<id>.md
```
`Store.Dir string` is `filepath.Dir(path)` set in `Open`. New tables: `brainstorm_messages(id, board_id, role, text, status, created_at)`, `brainstorm_state(board_id PK, session_id, sent_index)`, `plan_versions(id, board_id, version, content, created_at)`, all `REFERENCES boards(id) ON DELETE CASCADE`.

- [ ] **Step 1: Write failing tests:** `TestPlanVersionsIncrementAndRestoreKeepsHistory` (3 adds → versions 3,2,1, `GetPlanVersion(1)` content intact); `TestBrainstormTextAppendAndStatus`; `TestFailRunningBrainstormMarksStopped`; `TestBrainstormStateDefaultsToZero`; `TestDeleteBoardCascadesBrainstormData`; `TestOpenUsesWAL` (`PRAGMA journal_mode` returns `wal`).
- [ ] **Step 2:** `go test ./internal/store -run 'Brainstorm|Plan|WAL' -v` → FAIL.
- [ ] **Step 3: Implement.** Add tables to `schema`; add `&_pragma=journal_mode(WAL)` to the DSN (needed because the CLI subcommands in Task 5 write from another process).
- [ ] **Step 4:** `go test ./internal/store` → PASS.
- [ ] **Step 5: Commit** `feat(store): brainstorm messages, session state, plan versions, WAL`.

### Task 5: Agent subcommands (Claude's read/write hands)

**Files:**
- Modify: `backend/internal/store/cards.go` (add funcs), `backend/cmd/server/main.go` (dispatch)
- Create: `backend/cmd/server/agentcli.go`
- Test: `backend/internal/store/cards_test.go`, `backend/cmd/server/agentcli_test.go`

**Interfaces:**
- Produces in store: `CreateDraft(boardID int64, title, description string) (Card, error)`; `UpdateDraft(boardID, id int64, title, description string) (Card, error)` (both `ConflictError` unless the card is `draft` and belongs to `boardID`; titles/descriptions go through `cleanCard`); `TicketIndex(cards []Card) string` (one line `#<id> <status> <title>` per card, `""` for none).
- Produces in cmd: `runAgentCLI(args []string, getenv func(string) string, in io.Reader, out io.Writer) error`. Env: `ONE_PIECE_DB`, `ONE_PIECE_BOARD`. Commands: `tickets` (prints `TicketIndex`), `ticket <id>` (status, title, deadline, full description; cards of other boards → not found), `draft-add <title>` and `draft-update <id> <title>` (description read from stdin, prints `#<id> created|updated`). `main()` dispatches when `os.Args[1]` is one of these four names.

- [ ] **Step 1: Write failing tests:** store — `TestUpdateDraftRejectsNonDraftAndOtherBoard`, `TestCreateDraftLandsInDraftAtEnd`, `TestTicketIndexLines`; cmd — `TestAgentCLITicketReadsOwnBoardOnly`, `TestAgentCLIDraftAddReadsDescriptionFromStdin`, `TestAgentCLIRefusesWithoutBoardEnv`.
- [ ] **Step 2:** `go test ./internal/store ./cmd/server -run 'Draft|AgentCLI|TicketIndex' -v` → FAIL.
- [ ] **Step 3: Implement** per interfaces; `runAgentCLI` opens the store from `ONE_PIECE_DB` and never exposes any other write path.
- [ ] **Step 4:** `go test ./...` → PASS.
- [ ] **Step 5: Commit** `feat: agent subcommands for ticket reads and Draft writes`.

### Task 6: Brainstorm API and Claude runner

**Files:**
- Create: `backend/internal/api/brainstorm.go`
- Modify: `backend/internal/api/api.go` (server struct field `turns`, routes, startup `FailRunningBrainstorm`), `backend/internal/api/boards.go` (delete plan file in `deleteBoard`), `backend/internal/api/main_test.go` (stub `runBrainstorm`)
- Test: `backend/internal/api/brainstorm_test.go`

**Interfaces:**
- Consumes: Task 4 store funcs, Task 5 subcommands, `shorten`/`describeTool` from `review.go`.
- Produces:
```go
type brainstormRun struct{ System, Prompt, SessionID, Exe, DB, PlanPath string; BoardID int64; Dirs []string }
var runBrainstorm = func(ctx context.Context, in brainstormRun, onText func(string)) (sessionID string, err error)  // stubbable
const brainstormSystemPrompt = `...`  // spec "Perilaku Claude": one question per turn, play back, mark guesses, never build; use `tickets`/`ticket`/`draft-add`/`draft-update` and edit only the plan file
func indexChange(prev, cur string) string  // "" if equal, else lines "+ #5 draft Title" / "- #3 ..." under a one-line header
```
Routes: `GET /api/boards/{id}/brainstorm` → `{messages, running bool, plan: {version,content}|null, versions}`; `POST /api/boards/{id}/brainstorm/messages {text}` → 202 (409 if running, 400 if blank); `POST .../brainstorm/stop` → 204; `POST .../brainstorm/new` → 204 (409 if running; clears `SessionID` and `SentIndex`); `GET /api/boards/{id}/plan/{v}` → PlanVersion with content; `POST /api/boards/{id}/plan/{v}/restore` → 200 PlanVersion (409 if running; writes the plan file, adds a new version).

- [ ] **Step 1: Spike (no code committed).** In a scratch dir run `claude -p --output-format stream-json --verbose --max-turns 3 --setting-sources project --system-prompt "x" --allowedTools "Read,Bash(<abs path with a space> tickets:*)" "run tickets"` and a `--resume <session_id>` follow-up. Confirm: the `result` event carries `session_id`; the allowedTools pattern works with this binary path (if a space breaks it, make the server create a space-free symlink under `os.TempDir()` and pass that as `Exe`); `Edit(<planfile>)`/`Write(<planfile>)` patterns permit only that file; a heredoc after `draft-add` matches the `Bash(... draft-add:*)` pattern. Record what worked as a comment above `runBrainstorm`.
- [ ] **Step 2: Write failing tests** (stub `runBrainstorm` in each test): `TestBrainstormSendStoresMessagesAndSession` (stub returns `"sess-1"` after calling `onText("hi")`; after the turn: user + assistant(done, text "hi") stored, state `SessionID=="sess-1"`); `TestBrainstormSecondSendWhileRunningIs409`; `TestBrainstormStopKeepsPartialReplyAsStopped`; `TestBrainstormResumeFailureFallsBackToFreshSession` (first call with SessionID set returns error, second call with `SessionID==""` succeeds); `TestBrainstormSnapshotsPlanWhenFileChanged` (stub writes `PlanPath`; a new version appears, and none when unchanged); `TestRestorePlanWritesFileAndAddsVersion`; `TestIndexChange`; `TestDeleteBoardRemovesPlanFile`; `TestGetBrainstormWithNoPlanReturnsNullPlan`.
- [ ] **Step 3:** `go test ./internal/api -run 'Brainstorm|Plan|IndexChange' -v` → FAIL.
- [ ] **Step 4: Implement the turn** (`runTurn(boardID, msgID int64, text string)` in a goroutine, context 5 min, cancel func kept in `s.turns` under a mutex): build the first-message prefix when `SessionID==""` (board name/description/folders, `tickets` index, "read the plan file at <path> if it exists") or `indexChange(state.SentIndex, current)` otherwise; call `runBrainstorm`; on `onText` call `AppendBrainstormText`; on resume error retry once with `SessionID=""`; afterwards save state (`SessionID`, `SentIndex` = freshly built index, which includes Claude's own changes), snapshot the plan file if its content differs from `LatestPlanVersion`, and `FinishBrainstormMessage` with `done`/`stopped`/`failed`. The real `runBrainstorm` mirrors `streamClaude` (stream-json parsing, `cmd.Env` adds `ONE_PIECE_DB`, `ONE_PIECE_BOARD`; args `--resume`, `--max-turns 12`, `--system-prompt`, `--add-dir` for each folder and the plan dir, the allowedTools proven in Step 1).
- [ ] **Step 5:** `go test ./...` → PASS.
- [ ] **Step 6: Commit** `feat(api): brainstorm chat sessions, plan versions, restore`.

### Task 7: Brainstorm modal

**Files:**
- Create: `frontend/src/components/BrainstormModal.jsx`
- Modify: `frontend/src/pages/Board.jsx` (button + mount), `frontend/src/index.css` (small rules reusing `.modal`, `.ticket-*` styles where possible)

**Interfaces:**
- Consumes: Task 6 routes. Produces: `BrainstormModal({boardId, onClose, onCardsChanged})`.

- [ ] **Step 1:** `Board.jsx`: a "Brainstorming" button in `.chips` next to the folder chips opens the modal; `onCardsChanged={load}`.
- [ ] **Step 2:** Modal with two tabs. **Chat:** message list (user/assistant, assistant text rendered as plain pre-wrapped text; status chip for `running`/`stopped`/`failed`), textarea + Send (Enter sends, Shift+Enter newline, disabled while `running`), Stop button while running, "New conversation" button (confirm via `window.confirm`). **Plan:** latest plan rendered with the existing Markdown renderer used by `RichEditor`/review comments (reuse, don't add a library), or "No plan yet" when `plan` is null; version list with a Restore button per version (confirm, disabled while running).
- [ ] **Step 3:** Poll `GET /api/boards/{id}/brainstorm` every 1s while `running`, otherwise once on open; when a turn finishes (running true→false) call `onCardsChanged()`. Show API errors (409 etc.) inline like `TicketModal`.
- [ ] **Step 4: Verify.** `npm run build && npx oxlint src`. Manual with real `claude`: open Brainstorming, send "Rencanakan fitur login owner", see streamed text; ask Claude to create two tickets → two cards appear in Draft after the turn; Plan tab shows a version; Restore an older version adds a new one; Stop mid-reply then send again works; reopen the modal → history persists.
- [ ] **Step 5: Commit** `feat(ui): Brainstorming chat modal with plan tab`.

---

## Self-review notes

- Spec coverage: edit board → T1; Draft rules, gates, hidden panels, keep-content-on-return → T2–T3; chat UI/plan/restore/no manual edit → T6–T7; token rules (index, resume, stable system prompt, change diff, limits) → T6; Claude write guard + WAL → T4–T5; stop/timeout/resume-fallback/partial reply → T6; board delete cleanup → T6.
- README "Belum ada" line should be updated after Task 7 (fold into its commit).
