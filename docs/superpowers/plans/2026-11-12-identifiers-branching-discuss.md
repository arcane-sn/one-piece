# Identifiers, Branching, and the Discuss session Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every ticket, Agent Task, and QA Test Case gets a generated identifier; a per-ticket Discuss session lets Claude create `feature/{ticket}` branches, prepare `feature/{ticket}-dev`, push, and open a PR; Run checks and commits per task.

**Architecture:** Go backend + SQLite + React, extending what exists. Identifiers are stored columns (never recomputed). Git never runs from a free-form Claude shell: Claude gets only `<server-binary> git|gh|branch-*` subcommands, and a Go guard (`internal/gitx`) validates every argument shape, so the safety rules are enforced and unit-testable with real temp repos. The Discuss session reuses the Brainstorming chat pattern (`claude -p`, resumable session, stream into messages).

**Tech Stack:** Go ≥ 1.26, `net/http`, `modernc.org/sqlite`, system `git` and `gh` binaries; React + Vite.

**Spec:** `docs/superpowers/specs/2026-11-12-identifiers-branching-discuss-design.md`

## Global Constraints

- Board key: 2-5 uppercase letters, unique across boards. Identifiers: ticket `{KEY}-{n}`, Agent Task `{ticket}-T{n}`, QA Test Case `{ticket}-QA{n}`, e.g. `MID-12`, `MID-12-T3`, `MID-12-QA2`. They are stored, never recomputed; numbers are never reused (also not after delete or Regenerate).
- Branch names: `feature/{ticket}` and `feature/{ticket}-dev` (prefix `feature/` and dev branch `dev` are per-board defaults), built from the identifier, not the title.
- Branching status values: `planned`, `branched`, `merged_dev`, `pr_open`, `blocked`.
- Claude in the Discuss session runs only `git` and `gh`, only through the guarded subcommands; denied: force push (`-f`, `--force`, `--force-with-lease`), `reset --hard`, `clean`, `branch -D`, `checkout .` / `restore .`, `rebase`, push to `dev`/`main`/`master`/the base branch, `gh pr merge`, deleting remote branches.
- Claude stops and asks on: uncommitted changes, missing base branch, merge conflict, `gh` not installed, remote rejection.
- Commit message after an Agent Task: `{task identifier}: {task title}`, made by the server, not by Claude.
- Discuss is available only while the ticket is in Business or Done.
- Board keeps one folder (the master folder); repos are git folders inside it; Run keeps using the master folder unless the ticket has Branching rows.
- Repo is public: no passwords, `.env`, `*.db`, uploads in commits.
- Automated tests: Go `testing` + `httptest`, DB in `t.TempDir()`, `claude` stubbed (`TestMain` in `internal/api/main_test.go`), git tests use real repos created in `t.TempDir()`. Frontend is verified manually.

## Review Focus

1. Board name with no ASCII letters (`"2024!!"`, `"日本"`) still gets a valid, unique 2-5 letter key (Task 1).
2. Delete a task or test case, then Regenerate or add: new numbers continue, old numbers never come back (Task 1).
3. `repo` argument pointing outside the master folder, to a non-repo, through `..`, or through a symlink is rejected (Tasks 3, 5).
4. Branch names with spaces, `..`, a leading `-` (option injection such as `--upload-pack=x`), or ending in `.lock` are rejected; dirty working tree blocks `switch` and `merge` (Task 5).
5. Run on a ticket whose repo is on the wrong branch fails with a clear message and commits nothing; a commit failure marks the task Blocked and leaves other repos untouched (Task 9).

---

### Task 1: Identifier storage

**Files:**
- Create: `backend/internal/store/idents.go`, `backend/internal/store/idents_test.go`
- Modify: `backend/internal/store/store.go` (migrations + call backfill at end of `Open`), `boards.go`, `cards.go`, `items.go`

**Interfaces:**
- Produces:
  - `Board.Key string \`json:"key"\``, `Card.Ident string \`json:"ident"\``, `Item.Ident string \`json:"ident"\``.
  - `func DeriveKey(name string, taken map[string]bool) string` — letters from `name` (ASCII, uppercased); fewer than 2 letters → base `BRD`; try the first 3, then 4, then 5 letters; if all taken append `A`..`Z` to the first 4 (or to `BRD`). Result always matches `^[A-Z]{2,5}$` and is not in `taken`.
  - `func (s *Store) SetBoardKey(id int64, key string) (Board, error)` — `ValidationError` unless `^[A-Z]{2,5}$` and not used by another board; does not touch existing identifiers.
  - New columns: `boards.key`, `boards.card_seq`, `cards.ident`, `cards.task_seq`, `cards.qa_seq`, `card_items.ident` (counters `INTEGER NOT NULL DEFAULT 0`, others `TEXT NOT NULL DEFAULT ''`).
  - `CreateBoard` derives the key; `CreateCard` and `CreateDraft` assign `{KEY}-{card_seq+1}` inside their transaction; `insertItem` assigns `{card ident}-T{n}` for kind `agent` and `-QA{n}` for kind `qa` from the card's counters (so `AddFixTask`, `CreateItem`, and `ReplaceItemsOfKind` all get numbers; `ReplaceItemsOfKind` never resets counters).
  - `func (s *Store) backfillIdents() error` — run at the end of `Open`; idempotent: boards without key get one; cards with empty ident are numbered per board by id; items with empty ident per card and kind by id; counters set to the max assigned.

- [ ] **Step 1: Write the failing tests** in `idents_test.go`:

```go
func TestDeriveKey(t *testing.T)               // "Midas"→"MID"; "Midas 2" with MID taken→"MIDA"; "2024!!"→"BRD"; "日本"→"BRD"; "BRD" taken→ a different valid key; every result matches ^[A-Z]{2,5}$
func TestCardIdentStableAndNeverReused(t *testing.T) // MID-1, MID-2; delete MID-1; new card is MID-3; SetBoardKey("XYZ") leaves MID-1..3 unchanged, next card is XYZ-4
func TestSetBoardKeyValidation(t *testing.T)   // "a", "TOOLONG", "mid", "M1" → ValidationError; key used by another board → ValidationError
func TestItemIdentsNeverReused(t *testing.T)   // T1,T2 + QA1; delete T2; CreateItem → T3; ReplaceItemsOfKind("agent", 2 items) → T4,T5 ([FIX] rows kept); AddFixTask → next T; qa counter independent
func TestBackfillIdents(t *testing.T)          // insert rows with ident='' via raw SQL (two boards, items of both kinds), reopen with Open → idents by id order, counters allow the next create to continue
```

- [ ] **Step 2: Run** `cd backend && go test ./internal/store -run 'Ident|DeriveKey|BoardKey|Backfill'` — expect FAIL (undefined symbols).
- [ ] **Step 3: Implement** the columns via the existing `addColumn` loop in `Open`, the functions above in `idents.go`, and the wiring in `boards.go`/`cards.go`/`items.go`. Add `ident` (and `key`) to every SELECT/scan list. Use one `UPDATE … SET seq=seq+1 … RETURNING`-style or read-modify-write inside the transaction so concurrent creates cannot repeat a number (single connection already serializes, keep it inside the tx anyway).
- [ ] **Step 4: Run** `go test ./...` in `backend` — expect PASS (existing tests unchanged).
- [ ] **Step 5: Commit** `feat(store): generated identifiers for boards, tickets, tasks and test cases`.

### Task 2: Identifiers in API, prompts, and UI

**Files:**
- Modify: `backend/internal/api/boards.go` (`boardIn.Key`), `review.go`, `items.go` (generate prompt), `run.go` (`executeItem` prompt), `genflow.go`; `frontend/src/components/CardItem.jsx`, `TicketModal.jsx`, `ItemsSection.jsx`, `ItemModal.jsx`, `BoardModal.jsx`, `frontend/src/index.css`
- Test: `backend/internal/api/idents_test.go`

**Interfaces:**
- Consumes: `Board.Key`, `Card.Ident`, `Item.Ident`, `SetBoardKey` (Task 1).
- Produces: `PUT /api/boards/{id}` accepts optional `"key"` (uppercased by the handler, then `SetBoardKey`; 400 on invalid/taken). All JSON shapes already carry `key`/`ident`.

- [ ] **Step 1: Write the failing tests:**

```go
func TestBoardKeyEditable(t *testing.T)        // PUT {"name":"B","key":"abc"} → 200, board.key=="ABC"; PUT key "a" → 400; another board taking "ABC" → 400
func TestPromptsCarryIdentifiers(t *testing.T) // stubbed streamClaude/agentRun/runClaude capture the user message: review contains "Ticket ID: MID-1"-style line (use the real ident), items prompt too, run prompt contains "Your Agent Task: <task ident> " and QA run prompt "Your QA Test Case: <qa ident> "
```

- [ ] **Step 2: Run** `go test ./internal/api -run 'BoardKey|PromptsCarry'` — expect FAIL.
- [ ] **Step 3: Implement:** add a `Ticket ID: {ident}` line to the review and items prompts, and `{ident}` before the title in the run/QA prompts; handle `Key` in `updateBoard`.
- [ ] **Step 4: Frontend:** show the identifier as a small monospace tag (`.ident`) on the kanban card, in the ticket modal heading (when editing), before the title in Agent Task / QA Test Case rows, in the item modal heading; add an editable "Board key" field (2-5 letters, uppercased on input) to the Edit Board form only.
- [ ] **Step 5: Verify** `go test ./...`, `cd frontend && npm run build`; manually open a board: existing items show backfilled ids, a new ticket gets the next number, editing the key leaves old ids.
- [ ] **Step 6: Commit** `feat: show identifiers and give them to Claude`.

### Task 3: Branching data, board defaults, and API

**Files:**
- Create: `backend/internal/gitx/names.go`, `backend/internal/gitx/names_test.go`, `backend/internal/store/branches.go`, `backend/internal/store/branches_test.go`, `backend/internal/api/branches.go`, `backend/internal/api/branches_test.go`, `frontend/src/components/BranchingSection.jsx`
- Modify: `store.go` (table + board columns), `boards.go` (fields), `frontend/src/components/TicketModal.jsx`, `BoardModal.jsx`, `api.go` (routes)

**Interfaces:**
- Produces in `gitx`: `func ValidBranch(name string) bool` (matches `^[A-Za-z0-9][A-Za-z0-9._/-]*$`, no `..`, no `//`, no trailing `/` or `.lock`); `func IsRepo(path string) bool` (path is a directory containing `.git` as dir or file); `func RepoRootWithin(root, path string) (string, error)` — resolves symlinks on both, requires `path` inside `root`, returns the nearest ancestor (up to `root` inclusive) that `IsRepo`, error when none or outside.
- Produces in `store`:
  - `Board.BranchPrefix string \`json:"branchPrefix"\`` (default `feature/`), `Board.DevBranch string \`json:"devBranch"\`` (default `dev`), `func (s *Store) SetBoardBranchDefaults(id int64, prefix, devBranch string) (Board, error)` (prefix must match `^[a-z0-9][a-z0-9._-]*/$`, devBranch must pass `ValidBranch`).
  - Table `card_branches(id, card_id → cards ON DELETE CASCADE, repo, base_branch, feature_branch, dev_branch, status DEFAULT 'planned', pr_url DEFAULT '', note DEFAULT '', UNIQUE(card_id, repo))`.
  - `type Branch struct{ID, CardID int64; Repo, BaseBranch, FeatureBranch, DevBranch, Status, PRURL, Note string}` with lowercase-camel JSON tags.
  - `ListBranches(cardID int64) ([]Branch, error)`; `PlanBranch(cardID int64, repo, baseBranch string) (Branch, error)` — repo resolved with `RepoRootWithin(first board folder, repo)`, names = `board.BranchPrefix + card.Ident` and that + `-dev`, base must pass `ValidBranch`; re-planning an existing row whose status is not `planned` → `ConflictError`; `UpdateBranch(cardID int64, repo string, f BranchPatch) (Branch, error)` where `BranchPatch` has optional pointer fields for base/feature/dev branch, status (one of the five values), prUrl, note; `DeleteBranch(cardID int64, repo string) error`.
- Produces API: `GET /api/cards/{id}/branches` → `[]Branch`; `POST` `{repo, baseBranch}` → 201; `PUT` `{repo, …patch}` → 200; `DELETE ?repo=` → 204. `PUT /api/boards/{id}` accepts optional `branchPrefix`, `devBranch`.

- [ ] **Step 1: Write the failing tests:**

```go
// gitx
func TestValidBranch(t *testing.T)        // ok: "feature/MID-1", "dev", "release/1.2"; bad: "", "-x", "--upload-pack=x", "a b", "a..b", "a//b", "a/", "x.lock", "~x", "a:b"
func TestRepoRootWithin(t *testing.T)     // temp master with repoA(.git) and plain dir: repoA/src → repoA; plain dir → error; "../outside" → error; symlink inside master pointing outside → error; master itself with .git → master
// store
func TestPlanBranchNamesAndValidation(t *testing.T) // names "feature/MID-1" and "feature/MID-1-dev"; custom prefix/devBranch honored; repo outside folder / non-repo / bad base → ValidationError; replan when status!='planned' → ConflictError; delete card cascades rows
func TestUpdateBranchStatus(t *testing.T) // all five statuses accepted, "weird" → ValidationError; prUrl stored
// api
func TestBranchesAPI(t *testing.T)        // POST/GET/PUT/DELETE round trip as the handlers define; 400 for repo outside the folder
```

- [ ] **Step 2: Run** `go test ./internal/gitx ./internal/store ./internal/api -run 'Branch|RepoRoot'` — expect FAIL.
- [ ] **Step 3: Implement** the signatures above (`names.go` in `gitx`, rest in store/api). Create repos in tests with `git init` via `os/exec`.
- [ ] **Step 4: Frontend:** `BranchingSection` (below Business Flow in `TicketModal`, only when the ticket exists and is not Draft): one row per repo showing repo folder name, `base → feature`, status chip, PR link; a small modal to add (repo path + base branch) / edit fields / delete. Board edit form gets "Branch prefix" and "Dev branch" fields.
- [ ] **Step 5: Verify** `go test ./...` and `npm run build`; manually add a Branching row for a real repo inside a board folder, then try a path outside the folder (expect toast/inline error).
- [ ] **Step 6: Commit** `feat: per-ticket branching records and board branch defaults`.

### Task 4: Repo-aware `@` picker and mention resolution

**Files:**
- Modify: `backend/internal/api/files.go`, `frontend/src/components/MentionTextarea.jsx`
- Test: `backend/internal/api/files_test.go`

**Interfaces:**
- Consumes: `gitx.IsRepo`, `gitx.RepoRootWithin` (Task 3).
- Produces: `fileHit.IsRepo bool \`json:"isRepo"\``; `func repoMentions(folders []string, text string) (repos, notRepos []string)` — resolves `@<folder name>/…` like `mentionedPaths`, then to the repo root; unresolvable-to-repo mentions go to `notRepos` (absolute paths), both deduplicated.

- [ ] **Step 1: Write the failing tests:**

```go
func TestFilePickerMarksAndRanksRepos(t *testing.T) // master with repoA(.git), plain dir "docs", file; q=="" → repoA has isRepo=true and sorts before docs; q="repoa" works nested
func TestRepoMentions(t *testing.T)                 // "@master/repoA/src/x.go" → repos=[repoA]; "@master/docs" → notRepos; "@master/../x" ignored; duplicates collapsed
```

- [ ] **Step 2: Run** `go test ./internal/api -run 'FilePicker|RepoMentions'` — expect FAIL.
- [ ] **Step 3: Implement** both; repos rank 0 in `listFiles` ranking (before name-prefix rank).
- [ ] **Step 4: Frontend:** show a small "repo" tag on `isRepo` items in the picker list.
- [ ] **Step 5: Verify** `go test ./...`, `npm run build`.
- [ ] **Step 6: Commit** `feat: repo-aware @ picker`.

### Task 5: Git guard (`internal/gitx`)

**Files:**
- Create: `backend/internal/gitx/guard.go`, `backend/internal/gitx/guard_test.go`

**Interfaces:**
- Consumes: `ValidBranch` (Task 3).
- Produces:
  - `type Policy struct{ Feature, Dev, Base, PRBase string }` — the ticket's branch names for one repo; `PRBase` is the board's dev branch (PR target).
  - `func ValidateGit(args []string, p Policy) error` — pure. Allowed forms only (anything else → error naming the rejected command): `status [-s|--short|--porcelain|-b]`; `fetch [--prune] [origin]`; `branch --show-current` | `branch --list [<pattern>]` | `branch -r`; `rev-parse --abbrev-ref HEAD` | `rev-parse --verify <ref>`; `switch <branch>`; `switch -c <new> <start>` (`new` ∈ {Feature, Dev}; `start` is a valid branch or `origin/<branch>`); `merge --no-edit <branch>` (`branch` == Feature); `push [-u] origin <branch>` (`branch` ∈ {Feature, Dev}, never `dev`, `main`, `master`, `p.Base`, or `p.PRBase`); `log` with only `--oneline`, `-n <digits>`, `<ref>..<ref>`; `diff --stat <ref>..<ref>`. Every branch/ref argument must pass `ValidBranch` (or `origin/` + valid).
  - `func Git(repo string, args []string, p Policy) (string, error)` — `ValidateGit`, then runtime guards, then `exec git -C repo …` with combined output (30 s timeout; push 2 min). Runtime guards: `switch`/`merge` refuse when `git status --porcelain` is non-empty (error: `uncommitted changes in <repo>`); `merge` refuses unless the current branch is `p.Dev`; `switch -c` refuses if the branch already exists.
  - `func GH(repo string, args []string, p Policy) (string, error)` — only `pr create --base <p.PRBase> --head <p.Dev> --title <t> --body <b>` (title ≤ 200 chars, body ≤ 10000) and `pr view <p.Dev>`; error `gh is not installed` when `exec.LookPath("gh")` fails.

- [ ] **Step 1: Write the failing tests** (`guard_test.go`; helper `newRepo(t)` runs `git init -b main`, sets `user.name/email`, makes one commit; a second helper adds a bare `origin` remote):

```go
func TestValidateGitAllowsTheFlow(t *testing.T)  // switch -c feature/MID-1 main; merge --no-edit feature/MID-1; push -u origin feature/MID-1-dev; status --porcelain; log --oneline -n 5; fetch --prune origin
func TestValidateGitRejects(t *testing.T)        // table: push --force, push -f, push --force-with-lease, push origin dev, push origin main, push origin <Base>, branch -D x, branch -d x, reset --hard, clean -fd, checkout ., restore ., rebase main, switch --detach, merge feature/other, switch -c other main, fetch --upload-pack=x, log --all, "push origin :feature/MID-1" (delete), "--version", empty args
func TestGitRuntimeGuards(t *testing.T)          // real repo: dirty tree → switch refused with "uncommitted changes"; switch -c creates feature from main and checks it out; merge refused when not on Dev; existing branch refuses switch -c
func TestGitMergeAndPush(t *testing.T)           // repo + bare origin: create feature with a commit, create dev from main, merge --no-edit feature, push -u origin dev branch succeeds; origin has it
func TestGHRules(t *testing.T)                   // pr merge → error; pr create with wrong --base → error; with PATH lacking gh → "gh is not installed"
```

- [ ] **Step 2: Run** `go test ./internal/gitx` — expect FAIL.
- [ ] **Step 3: Implement** `ValidateGit` as a switch on `args[0]` with a strict shape check per command (reject unknown flags rather than denylisting), then `Git` and `GH`. Never pass user text through a shell; branch names validated before use.
- [ ] **Step 4: Run** `go test ./internal/gitx` — expect PASS.
- [ ] **Step 5: Commit** `feat(gitx): guarded git and gh for Claude`.

### Task 6: Agent CLI subcommands for branching

**Files:**
- Modify: `backend/cmd/server/agentcli.go`, `backend/cmd/server/main.go` (nothing if `agentCommands` map is the only gate), `backend/cmd/server/agentcli_test.go`

**Interfaces:**
- Consumes: `store.PlanBranch`/`UpdateBranch`/`ListBranches` (Task 3), `gitx.Git`/`gitx.GH` (Task 5), `repoMentions`-style resolution is NOT needed here (the repo argument must be absolute and pass `PlanBranch`).
- Produces: environment `ONE_PIECE_CARD` (card id) required by these subcommands; new entries in `agentCommands`:
  - `branches` → one line per row: `repo | base → feature | dev | status | pr`.
  - `branch-plan <repo> <base>` → `PlanBranch`; prints the row.
  - `branch-status <repo> <status> [note…]` → `UpdateBranch` (status one of the five values).
  - `branch-pr <repo> <url>` → sets `prUrl` and status `pr_open`.
  - `git <repo> <git args…>` and `gh <repo> <gh args…>` → find the card's row whose `Repo` equals `<repo>` exactly (else error `repo is not registered for this ticket; run branch-plan first`), build `gitx.Policy{Feature, Dev, Base: row.BaseBranch, PRBase: board.DevBranch}`, run `gitx.Git`/`gitx.GH`, print the output; non-zero exit on any error.

- [ ] **Step 1: Write the failing tests** with real temp repos and a temp DB:

```go
func TestBranchPlanThenGit(t *testing.T)     // branch-plan <repo> main; git <repo> switch -c feature/<ident> main → ok, branch is created; branch-status <repo> branched; branches lists "branched"
func TestGitRequiresRegisteredRepo(t *testing.T) // git <otherRepo> status → error mentions branch-plan; relative path → error
func TestGitSubcommandStillGuarded(t *testing.T) // git <repo> push --force origin x → error from gitx
func TestAgentCLIRequiresCardEnv(t *testing.T)   // branch subcommands without ONE_PIECE_CARD → clear error; existing tickets/draft commands still work without it
```

- [ ] **Step 2: Run** `go test ./cmd/server -run 'Branch|GitRequires|GitSubcommand|CardEnv'` — expect FAIL.
- [ ] **Step 3: Implement** the subcommands in `runAgentCLI`; add the names to `agentCommands`; the card must belong to `ONE_PIECE_BOARD`'s board.
- [ ] **Step 4: Run** `go test ./...` — expect PASS.
- [ ] **Step 5: Commit** `feat(agent-cli): guarded git/gh and branching record commands`.

### Task 7: Discuss session backend

**Files:**
- Create: `backend/internal/store/discuss.go`, `backend/internal/store/discuss_test.go`, `backend/internal/api/discuss.go`, `backend/internal/api/discuss_test.go`, `backend/internal/api/session_test.go`
- Modify: `store.go` (tables), `backend/internal/api/brainstorm.go` (extract runner), `api.go` (routes, `FailRunningDiscuss` sweep in `New`, `discuss map[int64]context.CancelFunc` on `server`), `main_test.go` (stub `runDiscuss`)

**Interfaces:**
- Produces in `brainstorm.go`: `type sessionSpec struct{ System, Prompt, SessionID string; Dirs, AllowedTools, Env []string; OnSession func(string) }` and `func runSession(ctx context.Context, spec sessionSpec, onText func(string)) (string, error)` — the current body of `runBrainstorm` (temp cwd, stream-json parsing, `--resume`, `--max-turns 12`, session id handling) parameterized by the spec; `runBrainstorm` now builds a `sessionSpec` and calls it with identical arguments to today.
- Produces in `store`: tables `discuss_messages(id, card_id → cards CASCADE, role, text, status, created_at)` and `discuss_state(card_id PRIMARY KEY → cards CASCADE, session_id)`; methods mirroring the brainstorm ones with a `Discuss` name and `cardID`: `AddDiscussMessage`, `AppendDiscussText`, `FinishDiscussMessage`, `ListDiscussMessages`, `RunningDiscussTurn`, `FailRunningDiscuss`, `GetDiscussState`/`SetDiscussState` (session id only). Do not generalize the brainstorm store; mirror it.
- Produces in `api/discuss.go`: `var runDiscuss = func(ctx context.Context, in discussRun, onText func(string)) (string, error)` (test seam, builds a `sessionSpec` with `AllowedTools`: `Read Grep Glob`, and `Bash(<exe> git:*)`, `Bash(<exe> gh:*)`, `Bash(<exe> branches:*)`, `Bash(<exe> branch-plan:*)`, `Bash(<exe> branch-status:*)`, `Bash(<exe> branch-pr:*)`; `Env`: `ONE_PIECE_DB`, `ONE_PIECE_BOARD`, `ONE_PIECE_CARD`; `Dirs`: the board folder); `func discussStage(status string) (string, bool)` ("setup" for business, "closeout" for done, false otherwise); `func discussSystemPrompt(stage string) string`; `func discussPrompt(b store.Board, c store.Card, branches []store.Branch, exe, stage, text string, fresh bool) string`.
- Produces API (mirroring brainstorm): `GET /api/cards/{id}/discuss`, `POST /api/cards/{id}/discuss/messages` `{text}`, `POST /api/cards/{id}/discuss/stop`, `POST /api/cards/{id}/discuss/new`. Messages/sessions/stop behave exactly like the brainstorm handlers; 409 `Discuss is available while the ticket is in Business or Done.` for other statuses; 409 when the board has no valid folder.
- System prompt content (stage `setup`): ask which repos (`@`) if none are registered; ask the owner for the base branch per repo (the owner decides; never assume one); check state with `git … status`; run `branch-plan` then `git … switch -c <feature> <base>` then `branch-status … branched`; stage `closeout`: for each registered repo `git fetch`, create the `-dev` branch from the dev branch, merge the feature branch, push both, `gh pr create`, record with `branch-status … merged_dev` / `branch-pr`. Both stages include the stop-and-ask rules and the fact that anything not available as a subcommand is not permitted.

- [ ] **Step 1: Write the failing tests:**

```go
// session_test.go — the runner against a fake `claude` (shell script placed first in PATH via t.Setenv)
func TestRunSessionParsesStream(t *testing.T)     // fake prints stream-json: system/session id, assistant text events, result with session_id; runSession returns the session id and calls onText per text block; args include --allowedTools entries and --resume when SessionID set; Env reaches the process
func TestRunSessionErrorAndCancel(t *testing.T)   // fake exits 1 with "No conversation found" → error mentions it; cancelled ctx returns ctx.Err()
// store
func TestDiscussMessagesAndState(t *testing.T)    // add/append/finish/list ordering; cascade on card delete; FailRunningDiscuss marks running as failed
// api
func TestDiscussOnlyInBusinessAndDone(t *testing.T) // card in business → POST message 202/200 and an assistant message appears (stub runDiscuss calls onText); move to agent → 409; to done → ok
func TestDiscussPromptStages(t *testing.T)        // setup prompt names the ticket ident, board defaults and registered repos; closeout prompt mentions the -dev branch and gh pr create; neither contains a raw `git` instruction outside "<exe> git"
func TestDiscussStopAndNew(t *testing.T)          // stop cancels a running stub turn; new clears session id and starts a fresh transcript marker
```

- [ ] **Step 2: Run** `go test ./internal/store ./internal/api -run 'Discuss|RunSession'` — expect FAIL.
- [ ] **Step 3: Implement.** First do the `runSession` extraction and confirm the existing brainstorm tests still pass (`go test ./internal/api -run Brainstorm`), then add the store, prompts, handlers, and routes by mirroring `brainstorm.go`.
- [ ] **Step 4: Run** `go test ./...` — expect PASS.
- [ ] **Step 5: Manual check:** start the server, open the existing Brainstorming chat on a board, send a message — it must still work (the runner was refactored).
- [ ] **Step 6: Commit** `feat: Discuss session backend and shared session runner`.

### Task 8: Discuss session UI

**Files:**
- Create: `frontend/src/components/DiscussModal.jsx`
- Modify: `frontend/src/components/TicketModal.jsx`, `frontend/src/components/BranchingSection.jsx`, `frontend/src/index.css`

**Interfaces:**
- Consumes: the Discuss API (Task 7), `MentionTextarea` (Task 4), `BranchingSection` (Task 3), `toast` (`src/lib/toast.js`).
- Produces: a "Discuss" button in the ticket modal header area, rendered only when `card.status` is `business` or `done` and the card is saved; `DiscussModal({ card, boardId, onClose })` modeled on `BrainstormModal.jsx` (chat list, composer using `MentionTextarea`, Stop, New session) with a stage line ("Setting up branches" / "Closing out"); while a turn runs it polls `GET /api/cards/{id}/discuss` and the Branching section reloads when a turn finishes.

- [ ] **Step 1: Implement** `DiscussModal` and the button; reuse the chat styles of the Brainstorm modal; errors via `toast`.
- [ ] **Step 2: Verify** `npm run build`; manually: open a Business ticket, click Discuss, send "@<repo>" with a base branch → a `Branching` row appears when Claude records it (requires a real `claude`); a ticket in Agent shows no Discuss button.
- [ ] **Step 3: Commit** `feat(ui): Discuss session modal`.

### Task 9: Git-aware Run

**Files:**
- Create: `backend/internal/api/rungit.go`, `backend/internal/api/rungit_test.go`
- Modify: `backend/internal/api/run.go`

**Interfaces:**
- Consumes: `store.ListBranches`, `gitx` helpers (current branch via `gitx.Git(repo, {"branch","--show-current"}, policy)` is fine, or `exec` directly for read-only calls).
- Produces:
  - `func (s *server) ticketRepos(cardID int64) ([]store.Branch, error)` — the ticket's Branching rows with status `branched` (the only status in which Run is in scope); an empty slice means Run behaves exactly as before.
  - `func checkOnFeature(repos []store.Branch) error` — for each repo, current branch must equal `FeatureBranch`; otherwise `store.ConflictError{Msg: "<repo base name> is on <current>, expected <feature>. Switch it (Discuss or git) and run again."}`. Called from `runnable` for Agent items (so single Run, Run All first item and each chain item are all covered).
  - `func commitTask(repos []store.Branch, it store.Item) (changed []string, err error)` — per repo: `git -C repo status --porcelain`; if non-empty `git add -A` and `git commit -m "<it.Ident>: <it.Title>"` (title shortened to 120 runes). Runs from `executeItem` after a successful Agent Task; a commit error → log `Failed: could not commit in <repo>: <msg>`, status `todo`, outcome `runBlocked`; success appends the log line `Committed in <repo names>`.
  - Scope: when `ticketRepos` is non-empty, `agentRun` is called with `cwd = repos[0].Repo` and `dirs = remaining repos` instead of the board's first folder and its siblings.

- [ ] **Step 1: Write the failing tests** (real temp repos on branches; `agentRun` stubbed to write a file inside the given `cwd`):

```go
func TestRunRefusesWrongBranch(t *testing.T)      // repo registered as branched on feature/X but checked out on main → POST run → 409 with "expected feature/"; item stays todo; no commit
func TestRunCommitsPerTask(t *testing.T)          // on the feature branch, stub writes a file → task done; `git log -1 --format=%s` == "<ident>: <title>"; a second repo registered but untouched has no new commit
func TestRunScopeUsesRegisteredRepos(t *testing.T) // stub records cwd/dirs: cwd == registered repo, master folder not among dirs; ticket without rows still gets the board's first folder (unchanged behavior)
func TestCommitFailureBlocksTask(t *testing.T)    // make commit fail (e.g. repo with a failing pre-commit hook script) → task todo with "Failed: could not commit", chain stops
func TestRunAllChecksBranchOnce(t *testing.T)     // Run All on two tasks both commit separately (two commits); switching the repo off the branch mid-chain blocks the next item with a "Blocked:" log
```

- [ ] **Step 2: Run** `go test ./internal/api -run 'RunRefuses|RunCommits|RunScope|CommitFailure|RunAllChecks'` — expect FAIL.
- [ ] **Step 3: Implement** in `rungit.go`; call `checkOnFeature` from `runnable` (Agent kind only) and `commitTask` from `executeItem`; keep QA runs unchanged (read-only).
- [ ] **Step 4: Run** `go test ./...` — expect PASS.
- [ ] **Step 5: Commit** `feat: Run checks the feature branch and commits per task`.

### Task 10: README and end-to-end check

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update README:** document identifiers, board key/defaults, the Discuss session (stages, `@`, safety rules, `gh` requirement), and Branching; move "Discuss" out of any "not yet" list.
- [ ] **Step 2: Full verification:** `cd backend && go vet ./... && go test ./...`; `cd frontend && npm run build`.
- [ ] **Step 3: Manual end-to-end** in a scratch master folder with two git repos (each with a bare `origin`, branches `main` and `dev`) and `gh` available (or note it missing): create a board on the master folder; create a ticket (check `MID-1`); Discuss in Business → `@repoA`, base `main` → Claude creates `feature/MID-1`, Branching shows `branched`; approve, generate tasks, Run All → one commit per task on `feature/MID-1`; move the ticket to Done; Discuss → `feature/MID-1-dev` from `dev`, merge, push, PR opened and linked; confirm force-push and push-to-dev requests are refused by asking Claude to do them.
- [ ] **Step 4: Commit** `docs: identifiers, branching and Discuss session`.
