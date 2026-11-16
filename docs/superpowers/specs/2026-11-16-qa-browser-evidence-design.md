# Browser QA with screenshot evidence

Date: 2026-11-16. Status: draft for the owner's approval. Part B of two; it builds on `2026-11-16-git-guard-v2-discuss-design.md` (Part A): evidence records the branch and commit of every repo under test, and QA sessions receive the ticket's decision log.

## Why

A QA Test Case today is judged by Claude reading code. `executeItem` in `run.go` calls `streamClaude` with `qaSystemPrompt` and `Read/Grep/Glob`, and `parseQA` reads one JSON verdict. For frontend work that proves little: nothing is rendered, nothing is clicked, and the owner has nothing to look at afterwards. The owner wants QA Test Cases to run in a real browser and leave evidence a person can check in a minute.

## What the owner gets

1. On a board, an **Edit Board → QA browser stack** section: base URL, allowed hosts, and a list of services (name, folder, start command, port, ready URL). The owner fills it in; Claude never touches it.
2. A **Log in for QA** button that opens a visible Chromium. The owner logs in by hand (the site has a bot check) and closes the window. The session is saved and reused.
3. **Run with Claude** on a QA Test Case of a configured board starts the stack, runs one Playwright spec written by Claude from the case's steps, and stops the stack. Every step ends with a screenshot.
4. The QA item shows the run: pass or fail, each step with its screenshot, the trace and video, and the branch plus commit of every repo under test.
5. A real failure creates a `[FIX]` task as today. A problem with the environment (login expired, port taken, stack not ready, broken spec) **blocks** the item with a plain message and creates nothing.
6. Boards without a QA stack behave exactly as today.

## Goals

- Real browser, one spec per QA Test Case, a screenshot per step, a verdict based on the run plus the screenshots.
- Claude cannot modify product repos, cannot read the saved login, cannot reach production, and never gets a shell.
- Infrastructure trouble is never reported as a product bug.
- Evidence is stored locally, served only to the logged-in owner, never committed.

## Non-goals

Playwright MCP or exploratory browsing, visual-regression baselines, automated login, parallel QA runs, running on Windows or Linux, Jira or any external tracker, worktrees. (The first version targets macOS, like the rest of the tool.)

## Decisions already made (not redesigned here)

B1 one spec per case, each step a `test.step()` ending in a screenshot. B2 a new server-validated subcommand runs only that item's spec with a server-generated config. B3 specs live in a QA workspace owned by One Piece under the data directory; the QA session cannot modify product repos, with a server-side backstop. B4 evidence per run, stored under the data directory, served through an authenticated route like uploads, never committed. B5 Claude reads the JSON result and the screenshots and returns the same verdict JSON; `pass` needs a passing run and matching screenshots; `fail` creates a `[FIX]` task. B6 infrastructure problems block without a `[FIX]`; Claude may repair its own spec a small, fixed number of times. B7 the stack is configured per board by the owner. B8 the server starts and stops the stack, never reuses a running server, one QA run at a time app-wide. B9 manual login once, storageState under the data directory, unreadable by Claude, validity checked before each run. B10 requests to hosts outside the allowed list are aborted. B11 boards without a QA stack keep today's code QA.

## Verified against the real `claude` CLI (2.1.325, spikes run 2026-11-16)

These shape the design. Each gets a regression note in a comment next to the code that relies on it, as `brainstorm.go` does.

| Check | Result |
|---|---|
| An allow rule `Edit(//<abs path>)` for one file lets Claude's `Write`/`Edit` change that file | works |
| The same session writing any other file (another file in that folder, a product folder, a `../` path into a product folder) | refused: "requested permissions to write … but you haven't granted it yet" |
| A `Write(...)` rule | the CLI rejects it with a warning: only `Edit(path)` rules exist, and they cover every file-editing tool. Use `Edit`. |
| `Bash` echo redirect into a product folder when only `Bash(<exe> cmd:*)` is allowed | refused |
| A bare `Read` allow | **reads anywhere the OS user can**, including the saved login file. A bare `Read`/`Grep`/`Glob` is therefore not enough for QA. |
| `--disallowedTools "Read(//<dir>/**)"` with bare `Read`, `Grep`, `Glob` allowed | `Read`, `Grep` (content mode) and `Glob` are all refused inside `<dir>`, and still work elsewhere. This is the mechanism for hiding the login. |
| `--strict-mcp-config` | accepted by `claude -p` |
| A symlink placed at an allowed edit path | not conclusive (the harness stopped it on "read it first"). The design never relies on the CLI here: the server creates the spec as a regular file and refuses a symlink (see Security). |

The plan's first step re-runs these as one scripted matrix against the installed CLI (including `Grep` with a path-scoped allow, to decide between "bare plus deny list" and "scoped allow") and fails loudly if a result changes.

## Architecture

```
owner ── UI ──► server (Go)
                 │ 1. preflight: toolchain, ticket repos on feature branch, port check, session check
                 │ 2. snapshot repos (branch, sha, dirty)           ┐ evidence/<run>/repos.json
                 │ 3. start allow-list proxy + stack, wait ready    │
                 │ 4. claude -p  (cwd temp dir)                     │
                 │      Edit: exactly specs/<ident>.spec.ts         │
                 │      Bash: <exe> qa-run, <exe> qa-blocked only   │
                 │         qa-run ─► playwright (server config,     │
                 │                   server proxy, sandbox) ───────►┘ screenshots, trace, video, result.json
                 │      Read: result summary + screenshots
                 │ 5. re-snapshot repos; any change ⇒ run fails (backstop)
                 │ 6. stop stack and proxy; decide outcome; save run
```

Directories (all under the data directory, which is the folder of the SQLite file; `backend/` by default):

```
<data>/qa/runner/            package.json pinning @playwright/test, node_modules, browsers (qa-setup)
<data>/qa/workspace/<board>/ helpers.ts (server-written), specs/<ITEM-IDENT>.spec.ts (Claude's one file per case)
<data>/qa/auth/<board>.json  Playwright storageState, mode 0600, directory 0700
<data>/qa/runs/<run>/        repos.json, services/<name>.log, attempt-1/ … attempt-N/ (config, screenshots, trace, video, result.json, blocked-hosts.json)
```

The toolchain is installed once by `server qa-setup` (runs `npm install` and `npx playwright install chromium` in `<data>/qa/runner`). Nothing in tests needs it. If it is missing, QA on a configured board is **blocked** with "Run `server qa-setup`".

## Data model

| Table or column | Shape |
|---|---|
| `board_qa` | `board_id PK, base_url, login_url, session_check_url, allowed_hosts` (JSON array), `headed` (0/1), `updated_at`. A board is "QA-configured" when it has a row with a `base_url` and at least one service. |
| `board_qa_services` | `id, board_id, position, name, folder, command, port, ready_url, ready_timeout_s DEFAULT 120`. `folder` is a path that must resolve (symlinks resolved) inside one of the board's folders; `ready_url` must have a loopback host (`127.0.0.1`, `localhost`, `::1`) and the same port. Names unique per board, `[a-z0-9-]{1,30}`. |
| `qa_runs` | `id, item_id, card_id, board_id, status` (`running`, `passed`, `failed`, `blocked`, `stopped`), `block_reason, verdict_issue, summary, attempts, repos` (JSON: `[{repo, branch, sha, dirty}]`), `started_at, finished_at`. |
| `qa_steps` | `run_id, attempt, position, title, status, screenshot, duration_ms, error` (error text is scrubbed and shortened). Filled from `result.json` after each attempt. |
| `.gitignore` | adds `backend/qa/`, `test-results/`, `playwright-report/`, `*.webm`, `storageState*.json`, `*.trace.zip`. A test asserts these lines exist. |

Retention: the server keeps the last 10 runs per item and deletes older rows and folders (rows first, then files). Evidence for a deleted item or card is removed with it.

## QA stack (B7, B8)

**Config validation.** `PUT /api/boards/{id}/qa` rejects: a service folder outside the board's folders; a port outside 1024-65535 or duplicated; a `ready_url` that is not loopback or is on a different port; a base URL whose host is not in the allowed hosts; allowed-host entries that are not a bare host (`example.test`, `*.example.test`) or loopback. `command` is stored as text and run as `sh -c` in the folder: it is the **owner's** configuration, saved only through the owner's UI session, and no Claude command can read or write it. The base URL's host and the loopback hosts are always allowed.

**Starting.** Before Claude is involved the server, holding the global QA lock:
1. refuses if another QA run, login, or session check holds the lock ("Another QA run is in progress");
2. for each service port, tries to bind and to connect on `127.0.0.1` and `::1`. If anything answers or the bind fails, the item is **blocked**: "Port 5000 is already in use by a process One Piece did not start. Stop it and run again." It never reuses a running server, because it may be on the wrong branch;
3. starts each service in its own process group (`Setpgid`), output to `services/<name>.log` in the run folder, with `PORT` and `BROWSER=none` set in the environment (open question 4);
4. polls each `ready_url` until it answers 2xx or 3xx or its timeout passes ("`bff` was not ready after 120 s, see its log" → blocked);
5. records the PIDs in `<data>/qa/stack.json`; on server start, leftover entries are killed after checking `ps` still shows the stored command (so a reused PID is never killed).

**Stopping.** After the run, on stop, on server shutdown and on any error: SIGTERM to each group, SIGKILL after 5 s, then confirm the ports are free.

**Chains.** Run All for QA keeps one stack up for the whole chain and stops it at the end (item-by-item restarts would take minutes). A chain stops at the first blocked item, as `runAll` already does (open question 5).

**One at a time.** A single server-wide mutex (`qaLock`) covers QA runs, "Log in for QA", and the session check.

## Login (B9)

- `POST /api/boards/{id}/qa/login` (needs the lock and the toolchain) starts `login.mjs`, a script embedded in the server binary and written to the runner folder. It launches **headed** Chromium (same channel as the runs), opens `login_url` (default: the base URL), and **autosaves** `context.storageState()` every 3 seconds to a temp file and renames it over `<data>/qa/auth/<board>.json` (atomic, mode 0600). The owner logs in and closes the window; the last autosave is the session. The server watches the process; when it exits it runs the session check and the UI shows the result. No "I'm done" button is needed.
- **Session check before every run.** Two layers: (1) read the file's cookies and fail early if every cookie with an expiry is past it (session cookies carry no expiry and cannot be judged); (2) a short headless navigation to `session_check_url` (default: base URL) with the saved state: the session is **invalid** if the final URL lands on the login URL's host and path, or the response is 401/403. Invalid or missing: the item is blocked with **"Login needed"** and a pointer to Edit Board → Log in for QA.
- The file is never returned by any route, never copied into evidence, never logged. The server scrubs every cookie and token value of 8 or more characters from anything it passes to Claude or stores (step errors, summaries, service logs shown in the UI).
- **Forget login** (`DELETE /api/boards/{id}/qa/session`) deletes the file.

## Security: what keeps Claude inside (B3, B9, B10)

**Claude's session (permissions, verified above).** `--permission-mode default` (never `acceptEdits`), `--setting-sources project` (so the owner's personal allow rules never apply), `--strict-mcp-config` (no MCP browser or other tools), `cwd` a temp folder, `--add-dir` for the product folders, the workspace and this run's evidence folder.
- Allowed: `Read`, `Grep`, `Glob`; `Edit(//<workspace>/specs/<ITEM-IDENT>.spec.ts)` (one file); `Bash(<exe> qa-run:*)`; `Bash(<exe> qa-blocked:*)`.
- Denied explicitly (belt and braces): `Edit(//<each board folder>/**)`, and `Read(//<data>/qa/auth/**)`, `Read(//<data>/*.db*)`, `Read(//<data>/qa/runner/**)`, `Read(//**/.env*)`, plus the usual secret folders in the OS user's home (`.ssh`, `.aws`, `.config`, `.claude`). Because the CLI lets a bare `Read` reach any readable file, the deny list is a required part of the design, not a nicety.
- Not given: any other `Bash`, `WebFetch`, `WebSearch`, `NotebookEdit`.

**The spec file.** The server creates `specs/<ITEM-IDENT>.spec.ts` as a regular file before the session, and `qa-run` refuses when the path or its folder is a symlink or not a regular file (checked with `lstat` right before Playwright starts).

**The spec is code that runs on the owner's machine**, so it is constrained as well:
1. **Lint** (`lintSpec`, in `qa-run`, before Playwright): the only imports are `./helpers`; no `require`, dynamic `import()`, `eval`, `Function(`, `process`, `globalThis`, `child_process`, `fs`, `net`, `http(s)`, `os`, `fetch(`, `XMLHttpRequest`, `WebSocket`, `chromium.launch`, `browser.newContext` or `newBrowserContext`; the file must contain at least one `qaStep(` and one `expect(`; maximum 400 lines. A rejected spec returns the reasons to Claude and does not start a browser. Lint is a guard against mistakes and against text injected through a page, not a security boundary against a determined author, so it is not the only layer:
2. **Sandboxed run.** `qa-run` starts Playwright through `sandbox-exec` (present on macOS) with a profile that denies writes everywhere except the attempt's evidence folder, the runner's temp folder and the system temp folder, and denies **all file writes under every board folder**. That is the OS-level enforcement of "the QA session cannot modify product repos" for the one thing Claude can make run: its spec. The stack services are started by the server outside this profile because dev servers write caches in their folders.
3. **Backstop.** Before and after every run the server records, for each repo under test and each service folder's repo, `HEAD` and `git status --porcelain`. Any difference fails the run as **blocked** with "QA changed `<repo>`; nothing was reported as a verdict" and keeps the evidence (open question 3 covers the cache-file noise this might cause).
4. **Honest limit.** Playwright must read the saved login to give it to the browser context, so spec code that bypasses the lint could in principle read it. The mitigations are the lint, the scrubber, and that Claude only ever sees a **server-composed summary** from `qa-run`, never raw process output.

**Allowed hosts (B10).** Three layers, strongest first:
1. **A filtering proxy** the server runs for the run's lifetime on `127.0.0.1:<random port>`, set as Chromium's proxy in the generated config. It allows `CONNECT` and plain requests only to the allowed hosts (exact or `*.suffix`, port ignored) and loopback, refuses everything else with 403, and records every refusal in `blocked-hosts.json`. Because it sits below the browser's JavaScript, it also covers IP-address URLs and WebSockets. Loopback traffic is not proxied (it is the stack itself).
2. **`helpers.ts` fixture.** The `test` that specs must import auto-registers a `context.route('**/*')` handler that aborts requests whose host is not allowed (a clear error, visible in the result). Service workers are blocked in the config.
3. **Lint** forbids launching a browser or context of the spec's own.

A step error with a non-empty `blocked-hosts.json` is classified as an environment problem (below), because the usual cause is a host missing from the list.

## Running a case

### The spec (B1)

`helpers.ts` (written by the server, not editable by Claude) exports `test`, `expect`, and:

```ts
qaStep(page, 'Open the cart', async () => { ... })   // test.step(title) { await body(); } then a screenshot in `finally`
```

The screenshot is taken **even when the step throws**, so a failing step still has its picture. File names are `NN-<slug>.png` in the attempt's `steps/` folder, `NN` zero-padded in step order. Claude writes the spec from the case's steps and expected result, one `qaStep` per step, with an `expect` for each expected result. It never types credentials (the session is already logged in) and never navigates outside the base URL.

### The generated config (B2)

One config per attempt, generated by the server into the attempt folder from the board's settings: `testDir` and `testMatch` limited to the one spec, `workers: 1`, `retries: 0`, `timeout` 120 s per test, `use.baseURL`, `use.storageState`, `use.proxy` (the filtering proxy), `use.trace: 'on'`, `use.video: 'on'`, `use.serviceWorkers: 'block'`, `use.headless` from `board_qa.headed`, a JSON reporter writing `result.json`, `outputDir` in the attempt folder. Claude cannot change any of it, and `qa-run` accepts **no arguments**: any argument is rejected.

### `qa-run` and `qa-blocked` (agentcli)

| Subcommand | Behaviour |
|---|---|
| `qa-run` | Needs `ONE_PIECE_MODE=qa`, `ONE_PIECE_ITEM`, and the run id and paths the server put in the environment at spawn (Claude cannot change them). Lints the spec, runs Playwright for one attempt, parses `result.json`, and prints a **server-composed summary**: attempt number, one line per step (`NN status title screenshot-path`), the first error of the first failing step (scrubbed, 500 characters), the list of blocked hosts, and a classification hint. At most **3 attempts that reach Playwright** and 8 calls in total (lint rejections included); the next call prints "limit reached; report your verdict or use qa-blocked". |
| `qa-blocked <reason…>` | Claude declares an environment problem it cannot repair (selectors that no longer match because the page changed shape is **not** one; a login page, an error page, a missing feature flag is). Records the reason on the run. The server blocks the item. It never creates a `[FIX]`. |

The verdict JSON is **unchanged** (`{"result":"pass"|"fail","issue":…,"desc":…}`), and `parseQA` is untouched. "Blocked" is a separate channel on purpose (open question 2).

### Outcome rules (B5, B6) — server side, in `executeQAItem`

Let *run* be the last attempt that reached Playwright.

| Situation | Outcome |
|---|---|
| Preflight failed: toolchain missing, login needed, port taken, service not ready, a ticket repo not on its feature branch, another QA run active | **blocked**, message as written above, no `[FIX]` |
| Claude called `qa-blocked` | **blocked** with Claude's reason |
| No attempt reached Playwright (lint kept rejecting, spec never written) | **blocked**: "Broken spec after N tries" |
| *run* has a top-level error (compile error, no test found), a navigation failure (`ERR_CONNECTION_REFUSED`, `ERR_NAME_NOT_RESOLVED`, `ERR_BLOCKED_BY_CLIENT`, a timeout on the first `goto`), a redirect onto the login URL, or non-empty `blocked-hosts.json` with a failing step | **blocked** (classified by `classifyAttempt`, a pure function tested on JSON fixtures) |
| *run* passed and Claude says `pass` | **passed**; item Done |
| *run* passed and Claude says `fail` (screenshots do not match the expected result) | **failed**; `[FIX]` task as today |
| *run* failed on an assertion and Claude says `fail` | **failed**; `[FIX]` task as today |
| *run* failed and Claude says `pass` | **blocked**: "Verdict disagrees with the run" (a pass needs both), no `[FIX]` |
| Repo backstop tripped | **blocked** |
| Claude never opened this attempt's screenshots (the server sees the `Read` tool calls in the stream) | **blocked**: "Claude did not look at the screenshots" |
| Stop pressed | **stopped**, item back to To Do, as now |

Blocked leaves the item in To Do with the reason in its log and on the run, and stops a Run All chain, like `runBlocked` today.

### Prompts

**System prompt** (new `qaBrowserSystemPrompt`, byte-identical across runs): you are a QA engineer; write ONE Playwright spec for ONE test case; the exact file you may edit; the `qaStep` and `expect` API in `helpers.ts` (read it first); one `qaStep` per step, assert the expected result; no credentials, no navigation outside the base URL, no imports other than `./helpers`; run `qa-run`; read the summary, then **open every screenshot of the final attempt with Read** and compare it with the expected result; you may fix your own spec up to the limit when the failure is yours (wrong selector, timing); never weaken an assertion or change an expected result to get a pass; a page that shows a login form, an error, or missing data that the code should provide is a finding or `qa-blocked`, never something to hide; final message only the verdict JSON.

**First message:** product, ticket (ident, title, description), the **decision log** (Part A), the test case, base URL, spec path, helpers path, evidence layout, and the repos under test with branch and commit. Boards without QA config keep `qaSystemPrompt` unchanged (B11).

## API routes

| Route | Purpose |
|---|---|
| `GET /api/boards/{id}/qa` | config, services, toolchain state, session state (`none`, `valid`, `expired`, `checking`) and saved-at time |
| `PUT /api/boards/{id}/qa` | replace config and services (validated) |
| `POST /api/boards/{id}/qa/login` | open the headed browser; 409 when the lock is held or the toolchain is missing |
| `POST /api/boards/{id}/qa/session/check` | run the session check now |
| `DELETE /api/boards/{id}/qa/session` | forget the login |
| `GET /api/items/{id}/qa-runs` | runs with steps, repos, blocked hosts, evidence links |
| `GET /api/qa-evidence/{run}/{attempt}/{file}` | one evidence file. Authenticated by the existing `guard` (it is under `/api/`). `file` is one of `steps/NN-slug.png`, `trace.zip`, `video.webm`, `result.json`, `repos.json`, matched against exact patterns and checked to resolve inside the run folder; `X-Content-Type-Options: nosniff`, no HTML or SVG is ever served; `trace.zip` and `video.webm` get `Content-Disposition: attachment`. No route serves `auth/`, `services/` or `runner/`. |

Existing routes (`run`, `run-all`, `stop`) are unchanged. The `PUT` and `login` routes sit behind the same session cookie as everything else; the server still listens on loopback only.

## UI changes

- **BoardModal (edit mode)**: a collapsible "QA browser stack" section: base URL, login URL, session check URL (both optional), allowed hosts (one per line), "show the browser while QA runs" toggle, and the services list (add, remove, reorder) with a folder picker limited to the board's folders. A status line shows the toolchain ("Playwright ready" or the `qa-setup` hint) and the session ("Logged in, saved 2 h ago", "Login needed"), with **Log in for QA**, **Check session**, **Forget login**.
- **ItemModal (QA items)**: when the board is configured, a **Browser QA** panel: latest run status, repos with branch and short SHA (and a "dirty" mark), the steps with screenshot thumbnails that open full size, links to trace and video, previous runs in a small list, and for a blocked run the reason with a button to the fix (for "Login needed": Log in for QA). Run copy changes from "Claude checks this against the code" to "Claude runs this in a browser (stack: …)".
- **ItemsSection**: a **Blocked** chip on QA rows whose last run was blocked.
- Toast when a run finishes or is blocked (existing job and toast mechanism).

## Tests (no browser, no network, no real stack)

Everything that starts a process sits behind a variable tests replace: `agentQA` (Claude session), `playwrightRun` (attempt), `stackStart` (services), `sessionCheck` (headless check), `loginRun` (headed login), `gitSnapshot`.

- `lintSpec`: allowed spec passes; each banned token and import fails; missing `qaStep` or `expect` fails; oversize fails.
- Allowed hosts: matcher table (exact, `*.suffix`, port ignored, IP literal, case, trailing dot, `evil-example.test` versus `example.test`); the proxy with `httptest` (allowed passes, refused returns 403 and is logged, CONNECT checked).
- Config validation table (folder outside board, duplicate port, non-loopback ready URL, base URL host missing from the list).
- Stack manager with a helper process started by the test binary: ready wait, ready timeout, port already bound by the test (blocked, process **not** reused), stop kills the group and frees the port, stale `stack.json` cleanup refuses to kill a PID whose command differs.
- `classifyAttempt` on JSON fixtures: assertion failure, compile error, `ERR_CONNECTION_REFUSED`, login redirect, blocked host, passed.
- Outcome table: every row above through `executeQAItem` with stubs, asserting item status, run status, and that **no `[FIX]` task exists** for every blocked row and exactly one for the failed rows.
- Backstop: a temp git repo mutated by the stubbed session makes the run blocked; an unchanged repo does not.
- Unconfigured board takes the old `qaSystemPrompt` path (B11), unchanged.
- One run at a time: a second start is refused; login and session check respect the same lock.
- `qa-run` refuses arguments, a wrong mode, a symlinked spec, and a fourth Playwright attempt; its summary contains no raw stdout and none of the scrubbed values.
- Scrubber removes cookie and token values from step errors and summaries.
- Evidence route: needs a session (401 otherwise), accepts only the exact file patterns, rejects `..`, encoded separators, `auth`, `services`, `runner`, serves PNG with `nosniff`, sends attachment for trace and video.
- Retention deletes rows and folders beyond 10 runs; deleting an item removes its evidence.
- `.gitignore` contains the required lines. No test file or fixture contains a cookie, token, real host, or product name.

Real-CLI verification (the matrix above) is a script run in the plan's first step, not a unit test, so `go test ./...` still needs neither the CLI nor a browser.

## Order of work (for the plan)

1. CLI matrix script and the comments that record it. 2. Store: config, services, runs, steps, retention. 3. Pure pieces with tests: host matcher, proxy, lint, classify, scrubber, config validation, evidence route. 4. Stack manager and locks. 5. Toolchain (`qa-setup`), `helpers.ts`, config generator, `qa-run`, `qa-blocked`, login script. 6. `executeQAItem` and the outcome table, wired behind the "configured board" check. 7. UI. 8. One manual end-to-end pass on a sample app, outside the repo, to confirm it works with a real browser.

## Open questions

1. **The bot check may also hit headless runs.** A site that blocks automation at login may keep challenging a headless Chromium even with a valid saved session, and the session check itself is a headless visit. Mitigation built in: the `headed` board setting and the same Chromium for login and runs. If the site still challenges, QA on that site is not workable without a headed run on every case. Say how the site behaves and I will pick the default.
2. **A separate "blocked" channel (`qa-blocked`) instead of a third verdict value.** You asked for the verdict JSON to stay the same, so Claude reports an environment problem through a subcommand. If you prefer `"result":"blocked"` in the JSON, `parseQA` grows one value; nothing else changes.
3. **Dev servers write files.** A service that writes tracked files (a lockfile, generated code) would trip the backstop. Recommendation: compare only `HEAD` and tracked-file changes outside paths the owner lists as ignorable per service (a "may write" glob list in the services form). Until then the backstop is strict and says which file changed.
4. **Service environment.** The frontend may need an environment variable pointing at the local BFF, and `PORT` may not be how a service chooses its port. Recommendation: an optional `env` field per service (`KEY=value` lines, no secrets allowed in the list; a secret belongs in the owner's shell profile). Not in the first cut unless you need it.
5. **Stack per chain or per item.** Per chain is faster; per item guarantees a clean server for every case. Recommendation: per chain, restarted whenever a ticket repo's `HEAD` changes between items.
6. **`sandbox-exec` is deprecated by Apple** but still ships and works. It is the only OS-level write restriction available without a container. If it ever disappears, the lint, the proxy and the backstop remain, and the run says so in its log.
7. **Session check method.** "Final URL is the login URL" works for redirect-style logins only. A site that answers 200 with a login component needs a marker (a selector that exists only when logged in). Recommendation: an optional `logged_in_selector` on the board config; I need to know how your site behaves to choose.
8. **Evidence size.** Video and trace per attempt can reach tens of MB. Keeping 10 runs per item is a guess; tell me if you want a different limit or a disk-usage line in the board settings.
