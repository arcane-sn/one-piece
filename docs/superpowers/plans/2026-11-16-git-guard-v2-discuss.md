# Plan: Git guard v2 and Discuss in every column

Spec: `docs/superpowers/specs/2026-11-16-git-guard-v2-discuss-design.md` (approved). Open questions resolved with the spec's recommendations: 1 pin start points (yes), 6 Claude may switch to cherry-pick only after the repo was `blocked` (yes); others as written.

Each step is one commit; `cd backend && go test ./...` must pass after each. Frontend steps also run `cd frontend && npm run build`.

1. **gitx guard.** Extend `Policy`; new allowed forms, runtime guards, push gate, `GitX` result, `PendingPicks`, `RemoteKind` (stub variable), `GH` remote check; pinned start points. Table tests (allowed, rejected neighbours, runtime on temp repos).
2. **Store.** `card_branches.mode`, status `pushed`, tables `card_picks`, `card_stashes`, `card_decisions`; store methods; `PushReady` helper; cascade tests; `ReadyForPush`/regress on commit.
3. **agentcli.** `branch-mode` (blocked-first rule for Claude), `pending-picks`, `remote-kind`, `decision`, `decisions`; extended `branches`; `git` builds the full Policy and records picks and stashes. Tests.
4. **Discuss API.** Stage `assist` for all columns, no 409; Discuss/Run mutual exclusion; new prompts; tool list; decision log in Discuss first message and in Run and QA first messages; decision routes; stash route; `mode` on PUT branches. Tests.
5. **Run integration.** `ticketRepos` includes integrated rows; commit after integration regresses status to `branched`. Tests.
6. **Frontend.** Discuss button everywhere, Decision log, Branching mode/pushed/stashes, assist label, toasts.
7. **Docs.** README section; final full test run and report.
