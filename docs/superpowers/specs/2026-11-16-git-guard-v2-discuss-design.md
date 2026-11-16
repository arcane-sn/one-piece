# Git guard v2 and Discuss in every column

Date: 2026-11-16. Status: draft for the owner's approval. Part A of two; Part B is `2026-11-16-qa-browser-evidence-design.md` and depends on this one (QA evidence records branch and commit, QA sessions receive the decision log).

## Why

The branching slice (`2026-11-12-identifiers-branching-discuss-design.md`) assumed a short flow: cut `feature/{ticket}`, later cut `feature/{ticket}-dev`, merge, push, `gh pr create`. The owner's real flow is wider:

- `feature/{ticket}` is cut from the latest **release branch** (always a branch, never a tag).
- When the work is done, `feature/{ticket}-dev` is cut from the shared dev branch and the feature is integrated into it by **merge**, or by **cherry-picking the ticket's own commits** when dev has moved a lot.
- A ticket can touch **several repos** that are merged and deployed together.
- Only two branch names exist per ticket. No other prefixes.

Today `gitx.ValidateGit` allows: `status`, `fetch`, list forms of `branch`, `rev-parse`, `switch`, one `merge` form (`merge --no-edit <feature>` while on the -dev branch), `push` of the ticket's two branches, `log`, `diff --stat`. That cannot express a release-branch update, a cherry-pick, a stash, a refresh of the -dev branch, or an abort. Discuss exists only in Business (setup) and Done (closeout), so a question like "dev moved, refresh my -dev branch" has no home while a ticket is in Agent or QA. Agreements made in a chat live only in the transcript. Closeout always ends in `gh pr create`, which fails on non-GitHub remotes.

## Goals

1. The guard covers the owner's flow with an allow-list that is still exact: every form listed, every other flag rejected.
2. Integration is **merge or cherry-pick, saved per repo per ticket**, and closeout follows the saved mode.
3. Multi-repo closeout is all-or-nothing for pushing: nothing is pushed until every repo integrated cleanly. The server enforces it, not only the prompt.
4. Discuss is available in every column.
5. Whatever the owner and Claude agree on is saved on the ticket as a dated **decision log**, shown in the ticket and handed to later Run and QA sessions.
6. Non-GitHub remotes never touch `gh`.

## Non-goals and anti-goals

Out of scope: conflict resolution by Claude, pull requests for non-GitHub remotes, Jira or any external tracker, one Claude session per ticket, worktrees.

Failure even if the code works: Claude resolves a conflict itself; Claude pushes anything before every repo integrated; a force-push, reset, rebase, clean, branch deletion, `stash drop`, `stash clear`, `checkout -- <path>`, `checkout -f`, `checkout -B`, clone or worktree gets through; dev or a -dev branch is merged into a feature branch; a commit lands directly on the -dev branch; a stash of another ticket is popped.

## Decisions already made (not redesigned here)

A1 allow-list style. A2 the new forms (listed in "Guard" below). A3 the still-forbidden list. A4 integration mode per repo per ticket, set by a new subcommand. A5 Claude aborts and reports on any conflict. A6 fixes go on the feature branch and are integrated again. A7 non-GitHub remotes skip `gh`. A8 closeout pushes nothing until every repo integrated. A9 Discuss in every column. A10 decision log.

## Data model

All changes are additive, through `CREATE TABLE IF NOT EXISTS` and `addColumn` as in `store.go`.

| Change | Shape |
|---|---|
| `card_branches.mode` | `TEXT NOT NULL DEFAULT 'merge'`; `merge` or `cherry-pick`. Existing rows become `merge`. |
| `card_branches.status` | adds one value, `pushed` (pushed, no PR because the remote is not GitHub). Full set: `planned`, `branched`, `merged_dev`, `pushed`, `pr_open`, `blocked`. The `branch-status` and PUT validators read `BranchStatuses`, so only that list changes. |
| `card_picks` | `id, card_id, repo, source_sha, created_at, UNIQUE(card_id, repo, source_sha)`. One row per feature commit already cherry-picked into the -dev branch. Full 40-char SHAs of the **source** commits. |
| `card_stashes` | `id, card_id, repo, sha, branch, message, created_at, popped_at` (`popped_at` empty until popped). Written by the server after `stash push` creates a stash. |
| `card_decisions` | `id, card_id, text, source ('claude' or 'owner'), created_at`. Text up to 2000 characters, at most 200 rows per ticket (oldest rows stay; the 201st is refused with a clear message). |

Cascade: all four tables use `ON DELETE CASCADE` on the card, like the other card tables. `DeleteBranch` also deletes that repo's `card_picks` rows (the repo is no longer part of the ticket); stash rows stay as history.

## Guard (`internal/gitx`)

`Policy` gains fields; `gitx` stays free of database access, so `agentcli` computes them and passes them in, which keeps every rule table-testable.

```go
type Policy struct {
    Feature, Dev, Base, PRBase string // as today
    Ident      string   // ticket identifier, e.g. MID-12
    Mode       string   // "merge" | "cherry-pick"
    Picked     []string // full SHAs already cherry-picked for this repo
    Integrated bool     // this repo's row is merged_dev, pushed or pr_open
    PushReady  bool     // EVERY repo of the ticket is integrated and none is blocked (A8)
}
```

### Allowed forms (static, `ValidateGit`)

Existing forms stay as they are. New or changed forms:

| Form | Static check | Runtime check (in `Git`) |
|---|---|---|
| `checkout <branch>` | `ValidBranch` | clean tree; the name must resolve to a local branch or `origin/<name>` (no commit, no path, no detached HEAD). Executed as `checkout <branch> --` so a file with the same name can never be meant. |
| `checkout -b <b> <start>` | `<b>` is Feature or Dev; `<start>` is a branch name | clean tree; `<b>` does not exist; `<start>` is a branch ref. Executed with `--no-track` so a branch cut from `origin/release` never gets the release branch as its upstream. |
| `pull --ff-only` | exactly those two words | clean tree; current branch is Feature or Dev; its upstream is `origin/<same name>`, otherwise refuse with "push -u first" |
| `stash push -u -m <msg>` | `<msg>` is 1-120 characters of `[A-Za-z0-9 ._:/#-]` and contains `Ident` as a whole token (`MID-1` does not match inside `MID-12`) | none. This is the only write command that does not need a clean tree. After success the server records the new stash (see below). |
| `stash list` | exact | none |
| `stash pop` | exact | clean tree; the top stash message contains `Ident` as a whole token; the current branch equals the branch the stash was made on (`git stash list -1 --format=%gs` begins `On <branch>:`) |
| `merge --abort` | exact | current branch is Feature or Dev. Exempt from the clean-tree rule (a conflicted tree is dirty by definition). |
| `merge --no-edit <x>` | `<x>` valid | clean tree, then by current branch: on **Dev**, `<x>` is Feature (refused when `Mode` is `cherry-pick`) or `origin/<PRBase>`; on **Feature**, `<x>` is Base or `origin/<Base>`, and Base must not be PRBase, `dev`, or a `-dev` name; on any other branch, refuse. |
| `cherry-pick -x <sha>...` | 1-50 SHAs of 7-40 hex characters | clean tree; current branch is Dev; `Mode` is `cherry-pick`; every SHA resolves to a commit that is a **non-merge commit inside `<base-ref>..<Feature>`** and is not in `Picked`; SHAs are given oldest first (same order as the range). `<base-ref>` is `origin/<Base>` when it exists, otherwise `<Base>`. |
| `cherry-pick --abort` | exact | current branch is Dev. Exempt from the clean-tree rule. |
| `push [-u] origin <b>` | `<b>` is Feature or Dev and not protected (as today) | **New gate (A8):** pushing Dev needs `PushReady`. Pushing Feature needs `PushReady` too once this repo is `Integrated`; before that it stays free, so the owner can still back up work in progress. |

Still rejected, with a test each: `pull origin dev`, `pull` without `--ff-only`, `pull --rebase`, `stash drop`, `stash clear`, `stash pop stash@{1}`, `stash apply`, `stash push` without `-u` or with an ident-less message, `checkout -- file`, `checkout -f`, `checkout -B`, `checkout .`, `checkout <sha>`, `reset`, `rebase`, `clean`, `branch -d/-D`, `clone`, `worktree`, `push --force`, `push origin dev`, `merge dev` or `merge origin/dev` while on Feature, `merge feature/X-dev` while on Feature, `merge --no-edit <feature>` while on a branch other than Dev, `cherry-pick` without `-x`, `cherry-pick -m 1 <sha>`, `cherry-pick --continue`, a cherry-pick of a SHA outside the range, of a merge commit, or of an already picked SHA, `commit` of any form.

`GH` changes: besides today's checks it refuses when the repo's `origin` is not on GitHub (A7), and `pr create` needs the repo's row to be `merged_dev` or `pushed`.

**GitHub detection.** `gitx.RemoteKind(repo)` parses `git remote get-url origin` (`https://github.com/o/r.git`, `git@github.com:o/r.git`, `ssh://git@github.com/o/r`) and returns `github`, `other` or `none`. Host list is `github.com` for now (open question 4). It is a package variable so tests can stub it.

### Records written by the server after a successful command

`agentcli git` calls `gitx.GitX`, which returns the output plus facts the caller records. Claude never writes these tables.

- `cherry-pick -x`: insert one `card_picks` row per source SHA (full SHA). Recorded only on exit code 0. A conflict stops the sequence, and the required `cherry-pick --abort` restores the pre-sequence state, so nothing half-applied is ever recorded.
- `stash push`: compare the top stash SHA before and after; if it changed, insert a `card_stashes` row (`sha`, branch, message). "No local changes to save" creates no row.
- `stash pop`: after success, set `popped_at` on the row whose SHA was on top.

## agentcli changes (`cmd/server/agentcli.go`)

New subcommands, all in `branchCommands` (they need `ONE_PIECE_CARD`) and added to the Discuss allow-list in `realRunDiscuss`:

| Subcommand | Behaviour |
|---|---|
| `branch-mode <repo> merge\|cherry-pick` | sets `card_branches.mode`; prints the new mode. Refused for an unregistered repo. |
| `pending-picks <repo>` | prints the feature commits that are non-merge, inside the range, not yet picked, oldest first, `<full sha> <subject>`; `none` when empty. This is the list Claude passes to `cherry-pick -x`; Claude never has to compute a range. |
| `remote-kind <repo>` | prints `github`, `other` or `none`. |
| `decision` | one decision note, text on stdin, up to 2000 characters; stored with `source='claude'`. Prints `decision saved`. |
| `decisions` | prints the log, oldest first, so a resumed session can re-read it. |

Changed: `branches` prints `mode`, `remote kind`, and the pending-pick count for each repo. `git` builds the extended `Policy` (mode, picks, integrated, push-ready) from the store before calling `gitx.GitX`, and records the results above. `branch-status` accepts `pushed`.

Nothing can commit on the -dev branch (A6): there is no `commit` form, and merges and cherry-picks are the only commit-creating forms and run only there as integration. `commitTask` (Run) already requires the feature branch.

## Integration, push and status flow

Status meaning, per repo row: `planned` (decided, not cut), `branched` (feature branch exists, work in progress **or new commits not yet integrated**), `merged_dev` (the feature is integrated into the -dev branch locally), `pushed` (both branches pushed, no PR, non-GitHub), `pr_open`, `blocked` (a conflict or failure, with the reason in `note`).

- **Fixes after integration (A6).** Run's `ticketRepos` today only returns `branched` rows. It now returns every row except `planned` and `blocked`, and still requires the repo to be on its feature branch. When `commitTask` creates a commit in a repo whose status is `merged_dev`, `pushed` or `pr_open`, the server sets that row back to `branched` (keeping `prUrl`, adding the note "new commits after integration"). That re-closes the push gate, so the next closeout integrates again, and for `merge` mode `merge --no-edit <feature>` simply merges the new commits; for `cherry-pick` mode `pending-picks` lists only the new ones.
- **Push gate (A8).** `PushReady` is true only when every row of the ticket is `merged_dev`, `pushed` or `pr_open` and none is `blocked`. If any repo conflicts, Claude aborts, marks it `blocked`, and no repo is pushed. Local integrations in the repos that already succeeded stay local; they are not undone, because there is no rollback and nothing has left the machine.
- **Mode switch (A4).** The expected path is: `merge --no-edit <feature>` fails with a conflict → `merge --abort` → Claude reports the conflicting files (the git output lists them) and **proposes** cherry-pick → after the owner agrees, Claude runs `branch-mode <repo> cherry-pick` and records a `decision`. The guard makes the order structural: `cherry-pick` is refused while the mode is `merge`, and `merge` of the feature into -dev is refused while the mode is `cherry-pick`. The owner can also change the mode in the Branching list.

## Discuss in every column (A9)

- `discussStage(status)` returns `setup` for Business, `closeout` for Done, and `assist` for Draft, Agent and QA. The "Discuss is available while the ticket is in Business or Done" refusal in `postDiscussMessage` is removed. `GET .../discuss` returns the stage for every card.
- The system prompt still depends only on the stage. `assist` is a general git assistant for that ticket: it answers questions, refreshes the -dev branch, aborts, stashes, switches, pulls, and integrates on request, with the same subcommands and the same guard. It does not start setup or closeout on its own.
- **No overlap.** A Discuss turn is refused while a Run or Run All is active for the same ticket, and Run is refused while a Discuss turn is active, with a toast ("Claude is running git for this ticket"). Both would otherwise change the same working tree. Two **different** tickets in the same repo can still collide; see open question 3.
- A Discuss session has `Read`, `Grep`, `Glob` and the `Bash(<exe> <cmd>:*)` patterns only. It still has no shell and cannot edit files.

## Decision log (A10)

- `decision` (Claude) stores a dated note. Owners can add and delete notes in the ticket. Claude cannot edit or delete one.
- Prompt rule in every Discuss stage: after the owner agrees on something that should survive the chat (a base branch, merge versus cherry-pick, skipping a repo, "release branch is X"), run `decision` with one self-contained sentence. Notes record the agreement, not the chatter.
- **Handed to later sessions.** The first message of a fresh Discuss session, of every Agent Task Run, and of every QA session starts with a "Decision log (oldest first)" block, newest 30 notes and at most 6000 characters; if older ones are cut, the block says "N older notes omitted". It goes in the user message, not the system prompt, so the system prompt stays byte-identical.
- A resumed Discuss turn does not repeat the block; it can run `decisions`.
- Claude Review sessions do not get the command in this slice (open question 5).

## Prompts

The system prompts are rewritten together; the shared part adds the rules below, and the stage parts follow.

Shared rules: only the listed subcommands; never resolve a conflict, abort and report the files and the git message; ask one question per turn; before any step that needs a clean tree run `git status`, and if the tree is dirty offer `stash push -u -m "<ident> <reason>"` and wait for a yes; record agreements with `decision`; fixes are never committed on the -dev branch; replies in the owner's language.

- **setup** (Business): as today, but the base is "the latest release branch", never a tag; the owner names it. `checkout -b <feature> origin/<base>` after a `fetch`; then `branch-status <repo> branched`.
- **closeout** (Done):
  1. `branches` to see mode, remote kind and statuses.
  2. **Phase 1, integrate every repo, push nothing.** For each repo not yet integrated: `fetch --prune origin`; clean tree check; cut the -dev branch with `checkout -b <dev> origin/<PR base>` (or `checkout <dev>` and `merge --no-edit origin/<PR base>` if it exists); integrate by the saved mode (`merge --no-edit <feature>`, or `pending-picks` then `cherry-pick -x <shas>`); on success `branch-status <repo> merged_dev`. On any conflict: abort, `branch-status <repo> blocked <reason>`, report files, propose cherry-pick if the mode is `merge`, **stop**, and wait.
  3. **Phase 2, only when every repo is `merged_dev`.** For each repo: `push -u origin <feature>`, `push -u origin <dev>`. Then, if `remote-kind` is `github`: `gh pr create ...` and `branch-pr <repo> <url>`; otherwise `branch-status <repo> pushed` and say so. Never call `gh` on `other` or `none`.
  4. Summarize per repo: branches pushed, PR link or "pushed, no PR".
- **assist** (Draft, Agent, QA): general.

The user message of a fresh session adds the decision log, and each registered repo line gains `mode`, `remote`, and `N pending`. The tools list in `realRunDiscuss` gains `branch-mode`, `pending-picks`, `remote-kind`, `decision`, `decisions`.

## API routes

| Route | Purpose |
|---|---|
| `GET /api/cards/{id}/decisions` | the log, oldest first |
| `POST /api/cards/{id}/decisions {text}` | owner adds a note (`source='owner'`) |
| `DELETE /api/cards/{id}/decisions/{did}` | owner deletes a note |
| `GET /api/cards/{id}/stashes` | stash rows, newest first |
| `PUT /api/cards/{id}/branches` | now also accepts `mode` |
| `GET /api/cards/{id}/branches` | each row now carries `mode`, `picked` (count), `remote` is not included (it needs git; the Branching list shows pushed branch names instead) |

Existing Discuss routes keep their paths; only the stage logic changes.

## UI changes

- **TicketModal**: the Discuss button shows in every column. A **Decision log** section below Branching lists notes with date, source badge (Claude or you), an add field, and delete.
- **BranchingSection**: new `pushed` status chip; a **mode** select (Merge, Cherry-pick) per row; a "N picked" hint in cherry-pick mode; for `pushed` and `pr_open` rows the pushed branch names (feature and -dev) are shown, so a non-GitHub repo has a visible result without a PR link. A small **Stashes** list under the table shows each unpopped stash (repo, message, date) with the note "restore it in Discuss" so a forgotten stash is never invisible.
- **DiscussModal**: `STAGE` gains `assist` ("Git assistant") for non-guided columns; the modal title and hint follow it. The modal also reloads the decision log when a turn ends (same trigger as Branching).
- Run and Run All show a toast when refused because Discuss is running.

## Tests

Guard (`gitx/guard_test.go`, table tests; runtime guards on temp repos, as `TestGitRuntimeGuards` does):
- every allowed form above, each in the right branch context;
- every rejected neighbour in the list under "Guard", including `pull origin dev`, `stash drop`, `checkout -- file`, `merge dev` on Feature, cherry-pick of a SHA outside the range, of a merge commit and of an already picked SHA;
- stash ident matching (`MID-1` versus `MID-12`), pop refused for another ticket's stash and on a different branch;
- `pull --ff-only` refused when upstream is not the same-named remote branch;
- push gate: Dev push refused without `PushReady`; Feature push free before integration, gated after;
- `merge --abort` and `cherry-pick --abort` work on a dirty tree; every other write form refuses a dirty tree;
- `merge` of the base into Feature refused when Base equals the PR base or `dev`;
- `GH` refuses a non-GitHub remote; `RemoteKind` parses the three URL shapes.

Store and agentcli: `branch-mode`, `pending-picks` (merge commits and picked SHAs excluded, oldest first), `decision` limits, stash and pick bookkeeping, `PushReady` across several repos (one `blocked` closes it), status regress to `branched` after a Run commit, cascade delete, `branch-status pushed`.

API: Discuss accepted in all five columns; refused during a Run and vice versa; decision routes; first message of Run and QA contains the log and omits it when empty. Everything that starts a process stays behind the existing stub variables (`runDiscuss`, `agentRun`, `gitx.RemoteKind`).

## Verification against the real `claude` CLI

No new CLI behaviour is needed beyond what `brainstorm.go` already verified (`Bash(<exe> cmd:*)` patterns with a space-free executable path, stdin prompt, `--resume`). The plan's first step re-runs one real Discuss turn that calls `branch-mode` and `decision` through the symlinked executable, and notes the result in a comment next to `realRunDiscuss`.

## Open questions

1. **Pin the start points?** `switch -c` and `checkout -b` today accept any branch as start. Recommendation: require Feature to start at `Base` or `origin/<Base>` and Dev at `PRBase` or `origin/<PRBase>`. It is one more guard and closes "dev cut from the wrong branch"; the existing guard tests that use `main` as a start would change. Not done unless you agree.
2. **Cherry-pick range base.** The rule uses `origin/<Base>` when it exists, so commits that arrived through `merge origin/<Base>` into the feature are not counted as the ticket's own. If Base was never fetched, the local `<Base>` is used and may count too much. Recommendation: closeout always runs `fetch` first (the prompt already does).
3. **Two tickets, same repo, same time.** One working tree cannot be on two branches. This slice only prevents overlap inside one ticket. Recommendation: a per-repo lock later; it needs a lock shared between server and `agentcli` processes, so it is its own change.
4. **GitHub hosts.** Only `github.com` counts as GitHub. A GitHub Enterprise host would be classified `other` and skip `gh`. Say so if you have one; it becomes a per-board list.
5. **Decision log in Claude Review.** A review chat also reaches agreements ("this ticket may be big"). Not included, because the review chat intentionally has no git or extra commands. Easy to add later.
6. **Who may change the mode.** Claude may run `branch-mode`, and the order is enforced only by the prompt ("after the owner agrees") plus the transcript. A stricter option is to accept Claude's switch to `cherry-pick` only after the repo was marked `blocked` by a merge conflict. Recommendation: add that check; it costs one condition.
7. **Already-picked override.** A SHA that was picked is refused again. If a pick was undone by hand, the owner deletes nothing in the UI today. Recommendation: wait until it happens.
