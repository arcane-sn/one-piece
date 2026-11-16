# Plan: Browser QA with screenshot evidence

Spec: `docs/superpowers/specs/2026-11-16-qa-browser-evidence-design.md` (approved). Decisions on its open questions: 1 `headed` board setting, default off; 2 `qa-blocked` channel; 3 strict backstop (HEAD + `status --porcelain`); 5 **stack per item** (simpler, always clean; per-chain reuse can follow); 6 `sandbox-exec`; 7 and 4 not in this cut; 8 keep 10 runs per item.

One commit per step, `cd backend && go test ./...` green after each (no browser, network or CLI needed).

1. **Pure pieces, package `internal/qa`:** host matcher, filtering proxy, `LintSpec`, scrubber, result parsing + `Classify`, config validation, evidence file patterns.
2. **Stack manager + repo snapshots** (`internal/qa`): port check, start/ready/stop with process groups, `stack.json` cleanup, git snapshot/compare.
3. **Store:** `board_qa`, `board_qa_services`, `qa_runs`, `qa_steps`, retention; `.gitignore`.
4. **Toolchain assets:** embedded `helpers.ts`, `login.mjs`, package.json, config generator, sandbox profile, `server qa-setup`.
5. **agentcli `qa-run` / `qa-blocked`.**
6. **API:** board QA config routes, login/session check/forget, QA lock, evidence route, runs route.
7. **`executeQAItem`** with the outcome table, CLI permission set, prompts; opt-in real-CLI matrix test.
8. **UI:** Board QA section, Browser QA panel, Blocked chip.
9. **Docs + final run.**
