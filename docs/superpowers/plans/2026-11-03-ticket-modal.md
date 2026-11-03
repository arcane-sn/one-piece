# Ticket Modal (spec slice 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the inline add/edit card forms with a Jira-style ticket modal (Title, Description, right panel with Deadline and a locked Approved button).

**Architecture:** Cards gain an optional `deadline` (date string). A new `TicketModal` React component handles create and edit, owns its API calls (like `BoardModal`) and reports results to `Board`, which owns the cards list. The Approved control is rendered but disabled: the review gate it depends on is spec slice 2, and approval must not be possible without it.

**Tech Stack:** Go (net/http, modernc sqlite), React 19 + Vite. Backend tests with `go test`; the frontend has no test runner, so UI is verified in the browser (cmux).

**Spec:** `docs/superpowers/specs/2026-11-03-ticket-modal-design.md` (slice 1 only; slices 2-4 are out of scope here).

## Global Constraints

- All UI text in English; plain-Indonesian chat, English code/comments.
- Palette/classes: reuse existing tokens in `frontend/src/index.css` and the modal classes added for `BoardModal` (`modal-backdrop`, `modal`, `field`, `modal-actions`).
- Non-GET requests must send JSON content type (already done by `api()`); body cap 1 MiB.
- Deadline format on the wire: `""` (none) or `YYYY-MM-DD`.
- `approved` is NOT added in this slice; no API may set approval.
- gofmt clean, `go vet ./...` and `go test ./...` green before each commit.

## Review Focus

- Invalid deadline strings (`tomorrow`, `2026-13-45`, `2026-1-5`) → 400, never stored.
- Clearing a deadline (sending `""`) really clears it.
- Database from before this change (cards table without `deadline`) opens and lists cards with `deadline:""`.
- Closing the modal with unsaved edits asks for confirmation; closing with no edits does not.
- Save is disabled while saving and while Title is blank (no double submit, no 400 for blank title).
- Saving an edit never changes a card's column or position (a drag that happened while the modal was open survives).

---

### Task 1: Card deadline in the backend

**Files:**
- Modify: `backend/internal/store/store.go` (generic add-column migration), `backend/internal/store/cards.go`, `backend/internal/api/cards.go`
- Modify tests: `backend/internal/store/cards_test.go`, `backend/internal/store/boards_test.go`, `backend/internal/store/description_test.go`, `backend/internal/api/cards_test.go` (call-site updates)
- Create: `backend/internal/store/deadline_test.go`

**Interfaces:**
- Produces: `Card.Deadline string` (`json:"deadline"`), `Store.CreateCard(boardID int64, title, description, deadline string) (Card, error)`, `Store.UpdateCard(id int64, title, description, deadline string) (Card, error)`, `cleanDeadline(string) (string, error)`; API `cardIn` gains `Deadline string \`json:"deadline"\``.
- Consumes: existing `cleanCard`, `ValidationError`, `Open`.

- [ ] **Step 1: Write failing tests in `deadline_test.go`**
  - `TestDeadlineRoundTrip`: create with `"2026-12-31"` → `Card.Deadline` equal; `ListCards` returns it; `UpdateCard(..., "")` clears it.
  - `TestDeadlineRejectsBadValues`: table `tomorrow`, `2026-13-45`, `2026-1-5`, `31/12/2026` → `isValidation(err)` for both `CreateCard` and `UpdateCard`.
  - `TestOpenMigratesCardsWithoutDeadline`: same technique as `TestOpenMigratesBoardsWithoutDescription` (rebuild a `cards` table without `deadline`, insert one row, reopen, `ListCards` returns it with `Deadline == ""`).
  - Update every existing `CreateCard(b, "t", "d")`/`UpdateCard(id, "t", "d")` call in the listed test files to pass `""` as the new last argument; add to `internal/api/cards_test.go` one case that POSTs `{"title":"x","deadline":"2026-12-31"}` and expects it echoed, and one with `"deadline":"nope"` expecting 400.

- [ ] **Step 2: Run to verify RED**
  Run: `cd backend && go vet ./... 2>&1 | head`
  Expected: compile errors (too many/few arguments, `Deadline` undefined).

- [ ] **Step 3: Implement**
  - `store.go`: replace the one-off `description` migration with `func addColumn(db *sql.DB, table, column, ddl string) error` (checks `pragma_table_info`, runs `ALTER TABLE ... ADD COLUMN ... ddl` only if missing) and call it for `boards.description` and `cards.deadline TEXT NOT NULL DEFAULT ''`. Add `deadline TEXT NOT NULL DEFAULT ''` to the `cards` CREATE statement.
  - `cards.go`: add `Deadline` to `Card`, `cardCols`, both scans (`scanCard`, the loop in `ListCards`); `cleanDeadline` accepts `""` or a value that `time.Parse("2006-01-02", v)` accepts **and** round-trips unchanged (rejects `2026-1-5`); thread the `deadline` argument through `CreateCard`/`UpdateCard` and their SQL.
  - `api/cards.go`: pass `in.Deadline` in `createCard` and `updateCard`.

- [ ] **Step 4: Run to verify GREEN**
  Run: `cd backend && gofmt -l . ; go vet ./... && go test ./...`
  Expected: no gofmt output, all packages `ok`.

- [ ] **Step 5: Commit**
  ```bash
  git add backend && git commit -m "feat(api): optional card deadline"
  ```

---

### Task 2: `TicketModal` component and styles

**Files:**
- Create: `frontend/src/components/TicketModal.jsx`
- Modify: `frontend/src/index.css` (append ticket styles)

**Interfaces:**
- Produces: `export default function TicketModal({ boardId, card, onChanged, onDeleted, onClose })`.
  - `card` is `undefined` for create, else a card object `{id,title,description,deadline,status,...}`.
  - `onChanged(card)` is called after every successful save (create or update) with the server's card.
  - `onDeleted(card)` after a confirmed delete; `onClose()` to dismiss (the modal calls it only after the dirty-check passes).
- Consumes: `api` from `../api`, existing `.modal-backdrop`/`.modal`/`.field`/`.modal-actions` classes.

- [ ] **Step 1: Build the component**
  - Layout: wide modal (`.modal.ticket`, ~760px, two columns). Left: Title input (autofocus on create), Description textarea (`rows` ~8, `maxLength` 10000). Right panel: **Status** (read-only label from `card.status`, "Business" for create), **Deadline** (`<input type="date">`, with a "Clear" link when set), **Approved** (disabled button, title/hint "Available after Claude review").
  - State: local `current` card (set after create so the next Save is a PUT), `title`, `description`, `deadline`, `busy`, `error`, `saved` flag.
  - Save: POST `/api/boards/{boardId}/cards` for new, PUT `/api/cards/{id}` otherwise, body `{title, description, deadline}`; on success call `onChanged(card)`, set `current`, show "Saved" (cleared on next edit). Disabled when `busy` or Title blank.
  - Dirty = fields differ from last-saved values. Close paths (Cancel/× button, Esc, backdrop click) ask `window.confirm("Discard unsaved changes?")` only when dirty.
  - Delete (edit mode only): `window.confirm` then `DELETE /api/cards/{id}`, then `onDeleted(current)`.
  - Errors from the API are shown in an `.error` line above the actions.

- [ ] **Step 2: Styles**
  Append to `index.css`: `.modal.ticket { width: min(780px, 100%); }`, a two-column grid (`.ticket-body { display:grid; grid-template-columns: 1fr 220px; gap: 20px }`, collapsing to one column under 640px), `.ticket-side` (glass-ish panel with the three labelled rows), `.status-pill`, and `.hint` text style. Reuse existing tokens.

- [ ] **Step 3: Build check**
  Run: `cd frontend && npm run build`
  Expected: builds with no errors.

- [ ] **Step 4: Commit**
  ```bash
  git add frontend && git commit -m "feat(ui): TicketModal component"
  ```

---

### Task 3: Wire the modal into the board

**Files:**
- Modify: `frontend/src/pages/Board.jsx`, `frontend/src/components/CardItem.jsx`, `frontend/src/components/Column.jsx`

**Interfaces:**
- Consumes: `TicketModal` from Task 2; `Card.deadline` from Task 1.
- Produces: `CardItem({ card, onOpen, onDropOn })` (click calls `onOpen(card)`; the inline edit form, its state and the `onSave`/`onDelete` props are removed; the little × delete button is removed, delete now lives in the modal). `Column` loses `onSave`/`onDelete` props and gets `onOpen`; its `footer` for Business becomes a button "+ Add card" calling `onCreate`.

- [ ] **Step 1: Implement**
  - `Board.jsx`: remove `AddCard`, `addCard`, `saveCard`, `deleteCard`. Add `ticket` state (`null | 'new' | Card`). Pass `onOpen={setTicket}` and `onCreate={() => setTicket('new')}`. Render `<TicketModal key=... boardId={id} card={ticket==='new'?undefined:ticket} onChanged={upsert} onDeleted={remove} onClose={() => setTicket(null)} />`. `upsert(card)` replaces by id or appends, then `reloadBoards()`; `remove(card)` filters it out, closes the modal and calls `reloadBoards()`. Keep `moveCard` and drag-and-drop exactly as is.
  - `CardItem.jsx`: show title, description (clamped), and a deadline chip (calendar glyph + the date) when `card.deadline` is set; the whole card is the click target.
  - `Column.jsx`: pass-through of the new props; keep empty-state and drop logic unchanged.

- [ ] **Step 2: Build**
  Run: `cd frontend && npm run build`
  Expected: success.

- [ ] **Step 3: Verify in the browser (cmux, server on 127.0.0.1:5300, restarted with the new binary)**
  Check each, reading the result of every step:
  1. "+ Add card" opens an empty modal; Save is disabled until a title is typed; after Save the card appears in Business and the modal stays open (now editing that card, "Saved" shown).
  2. Set a deadline, Save → chip appears on the card; Clear + Save → chip disappears.
  3. Click a card → modal opens pre-filled; edit the title, press Esc → confirm appears; cancel keeps the modal, accept closes without saving.
  4. Save an edit on a card in the Agent column and confirm it stays in Agent at the same position.
  5. Delete from the modal removes the card and updates sidebar counts.
  6. Approved button is visibly disabled with the hint.
  Expected: all six behave as described; report any that do not.

- [ ] **Step 4: Full backend suite and commit**
  Run: `cd backend && go vet ./... && go test ./...` → all `ok`.
  ```bash
  git add -A && git commit -m "feat(ui): ticket modal replaces inline card forms"
  ```

---

### Task 4 (added during execution): Rich-text description and Title Case UI

Requested by the user after Task 3: "Description harus bisa di edit kaya paragraph, list, pasang WYSIWYG yang oke … lengkap kaya Jira" and "New Capital, Create Ticket" (Title Case for titles/buttons/labels).

- Description editor: Tiptap 3 (`@tiptap/react`, `starter-kit`, `markdown`, `extension-list` for task lists, `extensions` for placeholder) in `components/RichEditor.jsx`; toolbar: text style (paragraph/H1-3), bold, italic, strike, inline code, bullet/numbered/task list, quote, code block, divider, link, undo/redo.
- Ruling: the description is stored as **Markdown** in the existing `description` column (no schema change): readable for `claude -p`, no HTML sanitising needed, plain-text preview on cards via a small stripper. Costs if wrong: tables/images are not supported yet; old plain-text descriptions render as Markdown paragraphs.
- Title Case applied to headings, buttons, field labels, empty-state titles; sentences, placeholders and confirm messages stay sentence case.

### Task 5 (added during execution): Generate description with Claude, custom style dropdown

Requested by the user: remove the redundant "Approval" label; a small button at the end of the Description title row opens a Prompt modal that generates the description; the native dropdown in the editor toolbar looked bad.

- `POST /api/generate-description` `{prompt, title, description, boardId}` → `{description}` (Markdown). `runClaude` runs `claude -p --tools "" --no-session-persistence --setting-sources project` with a system prompt asking for Summary/Context/Requirements/Definition of Done/Out of Scope and a small, manageable scope; cwd is an empty temp dir. Stubbed in tests (`generate_test.go`). Failure or empty output → 502.
- `PromptModal.jsx` (idea → draft preview → Use This Draft / Regenerate), `RichEditor` syncs external value changes, toolbar text-style is a custom menu.
- Ruling: the draft replaces the current description only after the user presses "Use This Draft" (a hint warns when a description already exists). Costs if wrong: no merge/append mode.
