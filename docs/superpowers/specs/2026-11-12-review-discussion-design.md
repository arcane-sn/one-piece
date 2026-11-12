# Claude Review as a Discussion

Date: 2026-11-12. Status: draft for the owner's approval.

## Why

Claude Review today is a one-shot verdict that re-runs automatically every time a ticket or its Business Flow is saved (`reviewQuietly` in `cards.go` and `flows.go`). The owner generates a description, generates a flow, edits, and each step starts another review: too frequent, costly, and the verdict is usually "Needs Work" with a list the owner must fix by hand, then regenerate, then wait for the next review. The owner cannot steer the review either; the prompt is fixed.

The owner wants Claude Review to become a conversation (like Discuss) in which Claude and the owner improve the ticket together until Claude approves it, so there is no bouncing between Generate and Review.

## What the owner gets

1. In the ticket modal's sidebar, **Claude Review** shows a status badge (Not Reviewed, Needs Work, Passed, Outdated) and a button that opens the **Review modal**, a chat with Claude about this ticket.
2. Claude reads the ticket (title, description, Business Flows) and the board's product folders (read-only), gives its review, asks the owner about decisions only the owner can make, and **applies fixes directly**: it edits the ticket description and title, and creates, edits or deletes **Business Flows** (one, two or more). Applied changes show in the chat as short system lines and the ticket modal refreshes.
3. Every Claude turn ends with Claude recording its verdict for the ticket as it now stands: `needs_work` with the open points, or `pass`. The badge always matches the latest turn. Claude stops asking when the ticket is clear enough for a developer to build without guessing, and says so.
4. The owner approves (the existing Approve button, unlocked by a current `pass` verdict). Claude never approves.
5. Review no longer runs by itself on save. The inline "Claude's Review" comment below the ticket and the dark review log in the sidebar are removed. The chat history is kept and reopens with the modal.
6. The turn runs in the background like Discuss: the owner can close the modal; reopening shows the live or finished state. A finished turn raises the top-right toast ("Review MID-3 selesai") and the kanban card shows its spinner while Claude works (via the existing jobs mechanism).

## Goals

- One place to get a ticket to "Passed": a discussion that edits the ticket itself.
- Far fewer Claude runs: none on save, one per owner message.
- The owner can direct the review in plain words ("ignore the FE/BE split, this ticket is allowed to be big").
- Pasted screenshots work in the chat (reuse `attachImages`).

## Non-goals and anti-goals

- Claude does not approve, move tickets between columns, edit Agent Tasks or QA Test Cases, run git, or touch files in the product folders. Its tools: read-only code access plus the ticket commands below.
- No automatic review of any kind, including a debounced one.
- No Review Guidance field on the board: steering happens in the chat.
- Must not overwrite the owner's unsaved edits (see "Concurrent edits").
- Draft tickets are not reviewed (unchanged): the Review button needs the ticket in Business.

## How it works

**Session.** Reuse the Discuss machinery (`runSession`, resumable `claude -p`, background turn, polling, stop, new session). Tables `discuss_messages` and `discuss_state` gain a `kind` column (`discuss` default, `review`); handlers are parameterised by kind and exposed under `/api/cards/{id}/review-chat/{messages,stop,new}` plus `GET /api/cards/{id}/review-chat`. Discuss behaviour does not change.

**Claude's commands.** Through the agent CLI (`internal/../cmd/server/agentcli.go`, same symlinked executable and `Bash(<exe> …:*)` allow-list pattern as Discuss), a review session may run only: `ticket-show` (title, description, flows with ids), `ticket-edit --title … --description-stdin`, `flow-add --title … --stdin`, `flow-update <id> …`, `flow-delete <id>`, and `review-verdict pass|needs_work --summary … --missing …`. No git/gh patterns are allowed in this session.
- Ticket and flow edits go through the same store functions as the UI (`touchCard` bumps `content_version`), so they behave exactly like owner edits, minus the automatic review.
- Flow text is validated like generated flows (`flowArrow`, title length, source size); a bad flow is rejected with the reason so Claude fixes it. The system prompt includes `swimlaneRules`.
- `review-verdict` inserts a finished row in `card_reviews` for the **current** `content_version`. The existing Approve rule (current `pass`, no unsaved edits) is unchanged.
- Before each Claude description or title change the previous text is saved (`card_revisions`: card id, version, title, description, time). The review modal shows an **Undo** next to the change line; undo restores that revision (itself a normal edit).

**Prompt.** A review system prompt built from today's review criteria (clear goal, testable Definition of Done, small scope, consistent, Business Flow present and matching) plus: apply what you are sure of, ask about what only the owner can decide, never invent product facts, end every turn with `review-verdict`, mention pasted images, answer in the owner's language.

**Concurrent edits.** The Review button is disabled while the ticket modal has unsaved edits ("Save first"). While a turn runs, the ticket modal's title, description and flow editors are read-only with a "Claude is editing" note and Save disabled; when the turn ends the modal reloads the ticket. Claude's edits therefore never race with the owner's.

**Notifications.** A review turn records a `jobs` row (kind `review_chat`, scope `card`, label "Review <ident>") so the toast and the card spinner come for free; it is finished (and marked consumed) when the turn ends.

**Removals and migration.** Remove `reviewQuietly` calls, the `POST /api/cards/{id}/review` start endpoint and the one-shot `runReview`, `ReviewComment`, the sidebar log box and "Start Review/Retry". Keep `card_reviews` and `GET /api/cards/{id}/review` (the badge reads it). Old reviews stay valid or outdated by version; reviews that were `running` at deploy are failed by the existing sweep.

## Tests the builder must write (the spec's risky inputs)

- No Claude run happens on card create/update or flow create/update/delete.
- A review turn that edits the description bumps `content_version`, stores a revision, and the following `review-verdict` records at the new version; an owner edit afterwards makes the verdict Outdated.
- `review-verdict` and the ticket/flow commands work only inside a review session (env-scoped to the card) and cannot touch another card's flows.
- Bad flow text is rejected; the allow-list refuses git, gh and unknown subcommands in a review session.
- Undo restores the previous text and leaves the verdict Outdated.
- Starting a second turn while one runs is refused; server restart fails a running turn; deleting the card removes its chat and revisions.
- Pasted images in a review message reach Claude (existing helper).

## Left to the builder

Exact wording of the system prompt and chat/toast copy; the layout of the review modal (reuse the Discuss modal component with a different header and a verdict strip); how revisions are pruned (keep the last 20 per card is fine); the CLI flag names.

## Calls I made (flagged for the owner)

- Claude edits the description and flows **directly** (the owner's answer), with Undo from saved revisions as the safety net.
- Every turn ends with a verdict so the badge never lags behind the chat.
- Editors are read-only while Claude's turn runs, instead of merging edits.
- The Review chat lives in the existing Discuss tables with a `kind` column, not new tables.
- Draft tickets still cannot be reviewed.
