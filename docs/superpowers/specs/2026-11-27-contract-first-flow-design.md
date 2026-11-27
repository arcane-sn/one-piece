# Contract-first ticket flow (design)

Date: 2026-11-27 · Status: approved by owner (DB may be wiped, no migration)

## Intent and why

Today a ticket is approved on its description alone. Agent Tasks are generated after approval, in the Agent column, and QA Test Cases only after the work is done, in a separate QA column. The owner is therefore approving something thin and discovering the real plan and the real acceptance criteria late.

New idea: the ticket is a **contract** settled in Business, before any agent works. The contract has three parts:

1. a short description: what the feature is about, and which requirements it has
2. Agent Tasks, already generated
3. QA Test Cases, already generated, acting as the **Definition of Done**

(The Plan branching is part of the contract too.) "Approved by me" means the owner accepts all of it. Agents then execute against the contract; they do not rewrite it.

## Columns

`Draft/Backlog → Business (To Do) → Agent → Done`. The `qa` column is removed. Discuss is opened from Agent cards.

## Goals

- Approval covers description, Agent Tasks, QA Test Cases and Plan branching together.
- Tasks and QA cases exist in Business, so the owner reviews them there.
- Inside Agent, tasks run, then QA runs against the Definition of Done, with the existing `[FIX]` loop (3 rounds).
- A ticket whose contract is wrong goes back to Business to be re-reviewed and re-approved. Nothing is edited silently in Agent.

## Non-goals (owner decided)

- No Preflight at Approve (feasibility checks of repos, QA stack, files).
- No Blocker mechanism, no partial re-review after returning to Business. The return is a plain move back to Business; approval is revoked, the review is redone in full.

## Anti-goals

- An approved ticket whose tasks or QA cases the agent can change on its own.
- QA cases so tied to code that does not exist yet that they are mostly wrong when run.

## Approaches considered

- **A. Stricter gate before Approved (chosen).** The Claude review checks coverage and traceability and blocks approval on open questions; generation reads the repo so tasks point at real areas. Prevents bounce-backs at the source.
- B. Preflight at Approve. Rejected by owner for now.
- C. Controlled bounce with Blockers and partial re-review. Rejected by owner for now.

## Design (what the builder implements)

**Status model.** `Statuses` becomes `draft, business, agent, done`. No migration: the owner agreed all board data can be deleted and started from scratch, so the schema is simply changed and the old DB file is removed. Frontend columns and the Done/ QA badges follow.

**Generation in Business.** Moving a ticket Draft → Business auto-generates Agent Tasks (with their Technical Flow, as today) and then QA Test Cases. A Generate button for each kind stays in Business and regenerates (old To Do items are struck through, as today). `generateItems` no longer requires `Approved` or the `qa` status; it requires `business` (or `agent` is rejected).

**QA cases are requirement-level.** The QA generation prompt writes behaviour a user can observe, each case citing the requirement it covers. It may read the repo to ground cases in screens that already exist, but it does not invent selectors or click paths for code that is not written. Concrete steps and prerequisites are resolved when the QA run executes, after the code exists.

**Contract version.** Creating, editing, deleting or regenerating an Agent Task or QA case, or changing the Plan branching, in Business bumps `ContentVersion` and revokes approval (as flows already do, `flows.go:32`). The review is tied to that version, so approval always means the whole contract as it is now.

**Review covers the whole contract.** The review prompt and context include description, flows, Agent Tasks, QA cases and branch plan. New checklist items, shown like the existing ones:
- every requirement is covered by at least one Agent Task
- every QA case traces to a requirement, and every requirement has a QA case
- no open questions or "TBD" in any part
- tasks refer to areas that exist in the repo (the reviewer reads the repo)

`SetApproved` is unchanged in rule (passing review of the current version) but the review is now broader. The review also fails when tasks or QA cases are missing.

**Agent column.** Moving to Agent requires approval. Items of an approved ticket are locked for edit (status, run logs and `[FIX]` tasks still change). Run All → when all tasks are Done, QA runs automatically against the Definition of Done (replacing `advanceToQA`/`autoGenerateQA`, which no longer move or generate). After the existing 3 `[FIX]` rounds the ticket stays in Agent for the owner.

**Done.** Ticket moves to Done when every QA case has passed. (My call: the owner can still drag it to Done manually.) Ship (push, PRs) is unchanged and works from Done.

**Returning.** Dragging Agent → Business is allowed; it revokes approval. Done work is kept on the items.

## Calls I made (please check)

1. Tasks and QA cases are generated automatically on Draft → Business, not on demand.
2. QA cases are requirement-level and finalised at run time.
3. Auto-move to Done when all QA cases pass.
4. No migration: the existing DB is wiped (owner's decision).
5. Items are locked in Agent; the only way to change the contract is back to Business.

## Left to the builder

Removing old DB handling and any code that only existed for the `qa` column, review prompt wording, UI placement of the new checklist items, how the QA run records "passed per case" for the Done rule (existing evidence badges may already carry this), test updates for the removed column.
