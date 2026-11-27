package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

const reviewTurnTimeout = 10 * time.Minute

// reviewMaxTurns is the step budget of one Review turn (like Discuss and Brainstorming; "continue" resumes).
const reviewMaxTurns = 25

// reviewChatSystem keeps the same bytes across turns. OP is the command path named in the first message.
const reviewChatSystem = `You are the business reviewer of ONE Business ticket, working WITH the product owner until the ticket is good enough to approve. You find the gaps and fix what is certain; you do not write product code and you never approve (the owner does). The shape of the ticket (its description and Business Flow) is also worked out in Discuss; here you check it.
The checklist, six criteria you judge every time (key: what a pass means):
- description: a developer can tell what to build; no contradictions or duplicates.
- cases: every case the ticket names or implies (A, B, C...) has a defined behaviour, nothing is left to guess.
- dod: the Definition of Done lists concrete outcomes a tester can check one by one (each one could become a test case).
- edge: errors, empty and invalid input, permissions and other edge cases are covered, or consciously out of scope.
- scope: one focused change, small enough for one agent run; otherwise say how to split it.
- flows: at least one Business Flow (swimlane text) exists and matches the description: who does what, in order.
How you work:
- Findings first. Read the ticket (OP ticket-show, only when the message says it changed or you have not seen it), judge the six criteria, then act. You may use Read, Grep and Glob on the product folders to check how the product works today, a handful of calls.
- Fix only what is certain: tighten wording, remove duplicates, add a missing DoD structure, write or repair a Business Flow. Use OP ticket-edit and OP flow-add / flow-update / flow-delete; they apply at once. Keep the owner's intent and facts; never invent product facts. ticket-edit replaces the WHOLE description: start from what ticket-show printed and keep every image (<img ...> tag or ![](...)) exactly as it was. If an edit is refused, read why and fix it.
- Do not guess what only the owner can decide (scope, business rules, a contradiction). Ask ONE focused question, or a short numbered list at most, and mark that criterion as a gap.
- End EVERY turn that judged the ticket with the verdict: OP review-verdict pass|needs_work [open point...] with this text on stdin (heredoc, plain lines; never JSON):
  summary: one or two sentences
  description: ok | why, short
  cases: gap | what is missing
  dod: ok | ...
  edge: ...
  scope: ...
  flows: ...
  (one line per key, all six, status ok, gap or na)
  pass only when no criterion is a gap; needs_work needs at least one gap or open point. na is for a criterion that truly does not apply, with the reason in the note. If the owner only asked a question and nothing was judged, answer without a verdict.
- Call OP only as the bare command with its full path written out: OP <subcommand> args, text on stdin via a heredoc (OP ... <<'EOF') or echo "..." | OP .... Never put cat, cd, a variable (OP=...; $OP), $(...), here-strings or loops around it, and run one OP command per Bash call: the permission check rejects anything else and the change is silently not made.
- Commands that take text read it from stdin with a shell heredoc, for example
  OP ticket-edit "New title" <<'EOF'
  ...whole new description...
  EOF
  flow-add "<title>" and flow-update <id> "<title>" take swimlane text on stdin.
- Never narrate tool mechanics. Reply in the language the owner writes in. Keep chat replies short.
` + swimlaneRules

// reviewRun is everything one Claude Review turn needs.
type reviewRun struct {
	System, Prompt, SessionID string
	Exe, DB                   string // Exe: space-free path of the server binary
	BoardID, CardID           int64
	Dirs                      []string // board folders (+ pasted images), read-only
	OnSession                 func(string)
}

// runReviewChat runs one `claude -p` review turn and returns the session id. A variable so tests can stub it.
var runReviewChat = realRunReviewChat

func realRunReviewChat(ctx context.Context, in reviewRun, onText func(string)) (string, error) {
	tools := []string{"Read", "Grep", "Glob"}
	for _, c := range []string{"ticket-show", "ticket-edit", "flow-add", "flow-update", "flow-delete", "review-verdict"} {
		tools = append(tools, "Bash("+in.Exe+" "+c+":*)")
	}
	return runSessionFn(ctx, sessionSpec{
		System: in.System, Prompt: in.Prompt, SessionID: in.SessionID, MaxTurns: reviewMaxTurns, Dirs: in.Dirs, AllowedTools: tools,
		Env: []string{"ONE_PIECE_DB=" + in.DB, "ONE_PIECE_BOARD=" + strconv.FormatInt(in.BoardID, 10),
			"ONE_PIECE_CARD=" + strconv.FormatInt(in.CardID, 10), "ONE_PIECE_MODE=review"},
		OnSession: in.OnSession,
	}, onText)
}

// reviewChatPrompt is lean on purpose: a fresh session gets the context once; a resumed one gets only what moved
// (the ticket is resent as "run ticket-show" only when its content version changed, the decision log only when it grew).
func reviewChatPrompt(b store.Board, c store.Card, exe, text string, fresh bool, seenVer int, decisions string, decisionsChanged bool, last *store.Review) string {
	var m strings.Builder
	fmt.Fprintf(&m, "Ticket: %s — %s\n", c.Ident, c.Title)
	if fresh {
		fmt.Fprintf(&m, "Board: %s\n", b.Name)
		if b.Description != "" {
			fmt.Fprintf(&m, "About the product: %s\n", b.Description)
		}
		if len(b.Folders) > 0 {
			fmt.Fprintf(&m, "Product folders you may read:\n- %s\n", strings.Join(b.Folders, "\n- "))
		}
		fmt.Fprintf(&m, "OP = %s\nOP subcommands: ticket-show | items | ticket-edit <title> (description on stdin) | flow-add <title> (text on stdin) | flow-update <id> <title> (text on stdin) | flow-delete <id> | review-verdict <pass|needs_work> [open point…] (summary and one line per criterion on stdin)\n", exe)
		m.WriteString("Run OP ticket-show first: it prints the description and the Business Flows.\n")
	} else {
		// a resumed session may still remember an older verdict format (JSON is blocked by the shell filter)
		m.WriteString("Verdict format (replaces any earlier instruction): plain lines on stdin, never JSON: \"summary: …\" then \"key: ok|gap|na | note\" for description, cases, dod, edge, scope, flows.\n")
	}
	if fresh {
	} else if c.ContentVersion != seenVer {
		fmt.Fprintf(&m, "The ticket changed since you last looked (content version %d). Run OP ticket-show before relying on it.\n", c.ContentVersion)
	} else if last != nil && last.Status == "done" {
		fmt.Fprintf(&m, "The ticket is unchanged since your last look; no need to run ticket-show. Your last verdict: %s", last.Verdict)
		if len(last.Missing) > 0 {
			fmt.Fprintf(&m, ", open: %s", strings.Join(last.Missing, "; "))
		}
		m.WriteString("\n")
	}
	if decisions != "" && (fresh || decisionsChanged) {
		m.WriteString("\nThe owner and Discuss agreed on these; do not flag them as gaps:\n" + decisions + "\n")
	}
	m.WriteString("\n" + text)
	return m.String() + opCallReminder
}

func (s *server) reviewChatRunning(cardID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.rchats[cardID]
	return ok
}

func (s *server) stopReviewChat(cardID int64) {
	s.mu.Lock()
	cancel := s.rchats[cardID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// runReviewTurn runs one turn in the background and settles the assistant message, session and job.
func (s *server) runReviewTurn(ctx context.Context, cancel context.CancelFunc, b store.Board, c store.Card, exe string, msgID, jobID int64, text string) {
	defer func() { // last: the ticket only reads as idle once everything is saved
		cancel()
		s.mu.Lock()
		delete(s.rchats, c.ID)
		s.mu.Unlock()
	}()
	sid, _ := s.st.GetReviewState(c.ID)
	run := reviewRun{System: reviewChatSystem, SessionID: sid, Exe: exe, DB: s.st.Path, BoardID: b.ID, CardID: c.ID, Dirs: b.Folders}
	seenVer, seenDec := s.st.ReviewSeen(c.ID)
	decs, _ := s.st.ListDecisions(c.ID)
	var last *store.Review
	if r, err := s.st.LatestReview(c.ID); err == nil {
		last = &r
	}
	run.Prompt = reviewChatPrompt(b, c, exe, text, sid == "", seenVer, store.DecisionBlock(decs), len(decs) != seenDec, last)
	run.Dirs, run.Prompt = s.attachImages(text, run.Dirs, run.Prompt)
	wrote := false
	onText := func(t string) {
		wrote = true
		s.st.AppendReviewText(msgID, t)
	}
	var announced string
	run.OnSession = func(id string) { announced = id }
	fresh := sid == ""
	newSID, err := runReviewChat(ctx, run, onText)
	if _, gone := s.st.GetCard(c.ID); gone != nil { // deleted while Claude was working
		return
	}
	status, jobErr := "done", ""
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		status, jobErr = "stopped", "stopped"
	case err != nil:
		status, jobErr = "failed", "Claude could not finish the review. Try again."
		log.Printf("review chat card %d failed: %v", c.ID, err)
		if !wrote {
			msg := "Claude could not answer. Try again."
			switch {
			case errors.Is(ctx.Err(), context.DeadlineExceeded):
				msg = "Claude took too long to answer. Try again."
			case strings.Contains(err.Error(), "No conversation found"):
				// Never swap in a fresh session behind the owner's back: it would lose what was agreed.
				msg = "Claude's earlier session could not be found, so I did not start a new one silently. Use the new-session button (top right) to continue with a fresh one; the chat history stays."
			}
			onText(msg)
		}
	}
	if status == "done" {
		if cur, err := s.st.GetCard(c.ID); err == nil {
			s.st.SetReviewSeen(c.ID, cur.ContentVersion, len(decs)) // Claude's own edits are not "a change" next turn
		}
		s.st.SetReviewState(c.ID, newSID)
	} else if announced != "" && (fresh || sid == "") {
		s.st.SetReviewState(c.ID, announced)
	}
	s.st.FinishReviewMessage(msgID, status)
	if jobID != 0 {
		if status == "stopped" { // a stop is not worth a toast
			s.st.MarkJob(jobID, "seen")
		}
		s.st.FinishJob(jobID, "", jobErrFor(status, jobErr), true)
	}
}

func jobErrFor(status, msg string) string {
	if status == "failed" {
		return msg
	}
	return ""
}

type reviewChatView struct {
	Messages  []store.ReviewMessage `json:"messages"`
	Running   bool                  `json:"running"`
	Available bool                  `json:"available"` // the ticket is in Business
}

func (s *server) reviewChatCard(w http.ResponseWriter, r *http.Request) (store.Card, bool) {
	id, err := pathID(r)
	var c store.Card
	if err == nil {
		c, err = s.st.GetCard(id)
	}
	if err != nil {
		writeErr(w, err)
		return c, false
	}
	return c, true
}

// GET /api/cards/{id}/review-chat
func (s *server) getReviewChat(w http.ResponseWriter, r *http.Request) {
	c, ok := s.reviewChatCard(w, r)
	if !ok {
		return
	}
	v := reviewChatView{Running: s.reviewChatRunning(c.ID), Available: c.Status == "business"}
	var err error
	if v.Messages, err = s.st.ListReviewMessages(c.ID); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// POST /api/cards/{id}/review-chat/messages {"text": "..."} → 202; the reply arrives through GET.
func (s *server) postReviewChatMessage(w http.ResponseWriter, r *http.Request) {
	c, ok := s.reviewChatCard(w, r)
	if !ok {
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" || len([]rune(text)) > 20000 {
		writeErr(w, store.ValidationError{Msg: "message must be 1-20000 characters"})
		return
	}
	if c.Status != "business" {
		writeErr(w, store.ConflictError{Msg: "Claude Review is available while the ticket is in Business."})
		return
	}
	b, err := s.st.GetBoard(c.BoardID)
	if err != nil {
		writeErr(w, err)
		return
	}
	exe, err := agentExe()
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), reviewTurnTimeout)
	s.mu.Lock()
	if _, busy := s.rchats[c.ID]; busy {
		s.mu.Unlock()
		cancel()
		writeErr(w, store.ConflictError{Msg: "Claude is still answering. Stop it or wait."})
		return
	}
	s.rchats[c.ID] = cancel
	s.mu.Unlock()
	release := func() {
		cancel()
		s.mu.Lock()
		delete(s.rchats, c.ID)
		s.mu.Unlock()
	}
	if _, err := s.st.AddReviewMessage(c.ID, "user", text, "done", 0); err != nil {
		release()
		writeErr(w, err)
		return
	}
	am, err := s.st.AddReviewMessage(c.ID, "assistant", "", "running", 0)
	if err != nil {
		release()
		writeErr(w, err)
		return
	}
	var jobID int64
	if j, err := s.st.CreateJob(store.Job{Kind: "review_chat", Scope: "card", TargetID: c.ID, CardID: c.ID, Label: "Review " + c.Ident}); err == nil {
		jobID = j.ID
	} else {
		log.Printf("review chat card %d: recording the job: %v", c.ID, err)
	}
	go s.runReviewTurn(ctx, cancel, b, c, exe, am.ID, jobID, text)
	writeJSON(w, http.StatusAccepted, map[string]bool{"running": true})
}

// POST /api/cards/{id}/review-chat/stop
func (s *server) stopReviewChatHandler(w http.ResponseWriter, r *http.Request) {
	c, ok := s.reviewChatCard(w, r)
	if !ok {
		return
	}
	s.stopReviewChat(c.ID)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/cards/{id}/review-chat/new: forget the Claude session; the transcript stays, with a divider.
func (s *server) newReviewChat(w http.ResponseWriter, r *http.Request) {
	c, ok := s.reviewChatCard(w, r)
	if !ok {
		return
	}
	if s.reviewChatRunning(c.ID) {
		writeErr(w, store.ConflictError{Msg: "Claude is still answering. Stop it or wait."})
		return
	}
	if err := s.st.SetReviewState(c.ID, ""); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.st.AddReviewMessage(c.ID, "marker", "New session", "done", 0); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/cards/{id}/revisions/{rid}/restore: Undo one of Claude's changes to the title/description.
func (s *server) restoreRevision(w http.ResponseWriter, r *http.Request) {
	c, ok := s.reviewChatCard(w, r)
	if !ok {
		return
	}
	rid, err := strconv.ParseInt(r.PathValue("rid"), 10, 64)
	if err != nil {
		writeErr(w, store.ErrNotFound)
		return
	}
	if s.reviewChatRunning(c.ID) {
		writeErr(w, store.ConflictError{Msg: "Claude is still working on this ticket. Stop it or wait."})
		return
	}
	next, newRev, err := s.st.RestoreRevision(c.ID, rid)
	if err != nil {
		writeErr(w, err)
		return
	}
	if newRev != 0 {
		s.st.AddReviewMessage(c.ID, "event", "Restored an earlier version of the ticket", "done", newRev)
	}
	writeJSON(w, http.StatusOK, next)
}
