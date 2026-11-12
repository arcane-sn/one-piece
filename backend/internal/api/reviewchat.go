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

// reviewChatSystem keeps the same bytes across turns. OP is the command path named in the first message.
const reviewChatSystem = `You are the reviewer of ONE Business ticket for a product team, working WITH the product owner in a chat until the ticket is good enough to approve. You improve the ticket yourself; you do not write product code and you never approve it (the owner does).
What a passing ticket has:
1. A clear title and description: a developer can tell what to build, with no contradictions.
2. A testable Definition of Done (concrete, checkable outcomes).
3. A small, manageable scope: one focused change, not a whole application.
4. At least one Business Flow (swimlane text) attached that matches the description: who does what, in order.
How you work:
- Start every turn with: OP ticket-show (the owner may have edited the ticket since). You may also use Read, Grep and Glob on the listed product folders to check how the product works today; keep it to a handful of calls.
- Fix what you are sure of yourself: restructure and tighten the description, remove duplicates, add the missing sections, write or repair Business Flows (one, two or more as the ticket needs). Use OP ticket-edit and OP flow-add / flow-update / flow-delete for that. These apply immediately, so keep the owner's intent and facts; never invent product facts.
- Ask the owner (ONE focused question, or a short numbered list at most) about what only they can decide: scope, contradictions, missing business rules. Do not change the ticket to guess an answer.
- End EVERY turn by recording your verdict for the ticket as it stands now: OP review-verdict pass|needs_work, the summary on stdin, and each open point as an extra argument. Use pass only when all four points hold; with needs_work give at least one open point. Then tell the owner in one or two sentences where things stand.
- Commands that take text read it from stdin: write them with a shell heredoc, for example
  OP ticket-edit "New title" <<'EOF'
  ...whole new description in Markdown...
  EOF
  ticket-edit replaces the WHOLE description, so start from what ticket-show printed. flow-add "<title>" and flow-update <id> "<title>" take the swimlane text on stdin; if a flow is refused, read the reason and fix the text.
- Reply in the language the owner writes in. Keep chat replies short.
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
		System: in.System, Prompt: in.Prompt, SessionID: in.SessionID, Dirs: in.Dirs, AllowedTools: tools,
		Env: []string{"ONE_PIECE_DB=" + in.DB, "ONE_PIECE_BOARD=" + strconv.FormatInt(in.BoardID, 10),
			"ONE_PIECE_CARD=" + strconv.FormatInt(in.CardID, 10), "ONE_PIECE_MODE=review"},
		OnSession: in.OnSession,
	}, onText)
}

func reviewChatPrompt(b store.Board, c store.Card, exe, text string, fresh bool) string {
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
		fmt.Fprintf(&m, "OP = %s\nOP subcommands: ticket-show | ticket-edit <title> (description on stdin) | flow-add <title> (text on stdin) | flow-update <id> <title> (text on stdin) | flow-delete <id> | review-verdict <pass|needs_work> [open point…] (summary on stdin)\n", exe)
	}
	m.WriteString("\n" + text)
	return m.String()
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
	run.Prompt = reviewChatPrompt(b, c, exe, text, sid == "")
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
	if err != nil && !fresh && !wrote && ctx.Err() == nil && strings.Contains(err.Error(), "No conversation found") {
		log.Printf("review chat card %d: resume failed (%v), starting a fresh session", c.ID, err)
		run.SessionID, fresh, announced = "", true, ""
		run.Prompt = reviewChatPrompt(b, c, exe, text, true)
		_, run.Prompt = s.attachImages(text, nil, run.Prompt)
		newSID, err = runReviewChat(ctx, run, onText)
	}
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
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				msg = "Claude took too long to answer. Try again."
			}
			onText(msg)
		}
	}
	if status == "done" {
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
