# Async Jobs: Generate That Survives Closing the Modal

Date: 2026-11-12. Status: draft for the owner's approval.

## Why

Today Generate Agent Tasks, Generate QA Test Cases, Generate Flow and Generate Description run inside the HTTP request that started them (`generateItems` in `items.go`, `generateFlow` in `genflow.go`, `generateDescription` in `generate.go`, each using `r.Context()`). Closing the modal drops the connection, which cancels the `claude -p` process or loses the result. Reopening shows nothing, and the result sometimes appears later, which looks broken. Discuss, Brainstorming and Run already run in the backend and keep their state; the four Generate actions do not.

The owner wants to start a Generate, close the modal (or switch to another ticket or board), and later find either a loading state (still running) or the result (finished). Several of these may run at once across different tickets.

## Goals

1. Each Generate keeps running in the backend after the modal closes, and its state is stored on the server: `running`, `done` (with result) or `error` (with message).
2. Reopening a modal reads the latest state from the server and shows loading, the result, or the error.
3. When a job finishes, a toast appears at the top wherever the owner is in the app, e.g. "Generate Agent Tasks MID-3 selesai". A failure toasts too.
4. A kanban card shows a small spinner while any job runs on that ticket.
5. Regenerating Agent Tasks or QA Test Cases no longer deletes the old ones (see "Supersede").
6. Jobs for different tickets run concurrently.

## Non-goals and anti-goals

- No streaming of partial output for Generate (Discuss and Brainstorming already stream).
- No job queue, retries or scheduling. One job per target at a time.
- No notification outside the app (no OS or email notifications).
- Must not lose a user's saved work: a finished job never overwrites text the owner typed (see Description and Flow).
- Must not leave a job `running` forever after a server restart.

## Job model

A new table `jobs`: `id`, `kind` (`agent_tasks`, `qa_cases`, `flow`, `description`), `scope` (`card`, `item`, `board`), `target_id`, `status` (`running|done|error`), `request` (JSON, input the job was started with), `result` (JSON), `error` (text), `seen` (bool), `created_at`, `finished_at`.

- At most one `running` job per (`kind`, `scope`, `target_id`). Starting another returns 409 "Already generating".
- Start endpoints keep their paths but return `202` with the job immediately; the work runs in a goroutine on a context owned by the server, not the request. Timeouts stay (3 to 4 minutes).
- `GET /api/jobs?scope=&target=` returns the latest job per kind for a target, so a modal can load its state. `GET /api/jobs/active` returns running jobs and finished-unseen jobs for the toast poller and card spinners. `POST /api/jobs/{id}/seen` marks a job as seen.
- A startup sweep marks leftover `running` jobs `error` ("Server restarted"), like `FailRunningDiscuss`.
- Deleting a card, item or board cancels and removes its jobs.

## Where results land

- **Agent Tasks / QA Test Cases** (scope `card`): on success the server applies the result itself (supersede rule below) and the job stores the new item ids. The modal only has to reload the items.
- **Flow generate** (scope `card` or `item`) and **Description generate**: the result is stored in the job and shown to the owner; it is applied to the editor only when the owner accepts it, so nothing typed in the meantime is overwritten.
- **New Ticket modal** (no ticket exists yet): the job uses scope `board` with `target_id` = board id and kind `description` or `flow`. The reopened New Ticket modal looks up the board-scope job and shows loading or the result. If the owner never reopens it, only the toast tells them it finished (decision A, chosen by the owner). Once the owner accepts or dismisses the result, the job is marked seen.

## Supersede (replaces delete on Regenerate)

Old behaviour: `ReplaceItemsOfKind` deletes every item of the kind. New behaviour, for Agent Tasks and QA Test Cases:

1. Items of that kind with status `todo` are marked superseded (new column `card_items.superseded_at`). Items `doing` or `done` stay active. These are the owner's rules: only To Do is struck.
2. The generated items are appended with new idents (the existing `nextItemIdent` counter, so idents are never reused).
3. Superseded items get a log line "Diganti oleh generate baru", render struck through and grey, grouped under a "Superseded" label below the active ones, can be opened and read, and have a Restore button (clears `superseded_at`) and the existing Delete.
4. Superseded items do not take part in: Run, Run All, the "Finish all Agent Tasks before running QA" check, the all-done/ready-for-QA logic, or branching/commit logic.
5. A "split" verdict from Claude still changes nothing.
6. Business and Technical Flows keep today's behaviour (they are saved edits, not a regenerated list).

## UI

- A small frontend job store (polls `GET /api/jobs/active` every 3 s while any job is running, backing off to 15 s otherwise) feeds: the toast (using `lib/toast`; fires once per job, then `seen`), the spinner on `CardItem`, and the Generate buttons inside modals (disabled with spinner while their job runs).
- Modals call `GET /api/jobs?...` on open; if a job is `running` they show loading and poll until done.
- The toast text names the ticket ident and the kind.

## Constraints

- SQLite uses a single connection: never query `s.DB` while a transaction is open.
- Mutations stay JSON with the existing guard middleware; the stub seams in `internal/api/main_test.go` stay and gain job-aware stubs.
- The existing sidebar/scrollbar styling is unrelated and untouched.

## Left to the builder

Table indexes, poll intervals, exact toast copy, the Restore button placement, and whether the job store lives in a React context or a small module. These are my calls and can change without changing what is built.

## Calls I made (flagged for the owner)

- 409 when the same kind is already running for a target, instead of queueing.
- Polling instead of server-sent events, to keep to the existing fetch-based style.
- Flow and Description results need an explicit accept; Agent/QA results apply themselves because they are new rows, not edits of text.
- Server restart marks running jobs as failed rather than resuming them.
