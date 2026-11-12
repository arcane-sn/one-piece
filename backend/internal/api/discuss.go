package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

const discussTurnTimeout = 10 * time.Minute // pushes and PRs can be slow

// discussStage maps a ticket column to the stage of its Discuss session; ok is false elsewhere.
func discussStage(status string) (string, bool) {
	switch status {
	case "business":
		return "setup", true
	case "done":
		return "closeout", true
	}
	return "", false
}

// The system prompt only varies with the stage, so it stays byte-identical across a stage's turns.
// "OP" is the command path the first message of a session names. Git is never spelled out as a raw
// command here: everything goes through OP, which only accepts the allow-listed forms.
const discussSystemCommon = `You are a git assistant for one product owner. You prepare and finish the branches of ONE ticket across the repositories the owner names. You do not write code.
Rules:
- You may only run the subcommands of OP that the first message lists. Everything else (raw git, raw gh, any other shell command, editing files) is not permitted, so do not try it. If you need something that is not available, say what and ask the owner to do it.
- The owner decides every branch name and base branch. Never assume a base branch: ask for it, one repo at a time.
- Ask ONE question per turn. Keep replies short. Reply in the language the owner writes in.
- Stop and ask before anything unusual: uncommitted changes, an existing branch, a merge conflict, a failed push. Never work around a refusal; report it plainly.
- After each repo step, record it: OP branch-status <repo> <status> [note].`

const discussSetupPrompt = discussSystemCommon + `
Stage: setup (the ticket is in Business; no code exists yet).
1. If no repo is registered, ask which repositories this ticket touches (the owner can mention them with @ in a message; the message then names the repo roots).
2. For each repo ask for the base branch. Check the state with: OP git <repo> status. Then run OP branch-plan <repo> <base> (it prints the feature branch name), OP git <repo> fetch --prune origin when the repo has a remote, and OP git <repo> switch -c <feature branch> <base> (or origin/<base> after the fetch). Finish with OP branch-status <repo> branched.
3. Summarize which repos are ready and which are left.`

const discussCloseoutPrompt = discussSystemCommon + `
Stage: closeout (the ticket is Done; the feature branches hold the finished work).
For each registered repo whose status is not pr_open, in this order:
1. OP git <repo> fetch --prune origin
2. OP git <repo> switch -c <dev branch> origin/<PR base>  (the -dev branch is cut from the board's shared dev branch, named in the first message)
3. OP git <repo> merge --no-edit <feature branch>  (resolve nothing yourself: on a conflict stop and ask)
4. OP git <repo> push -u origin <feature branch>, then OP git <repo> push -u origin <dev branch>, then OP branch-status <repo> merged_dev
5. OP gh <repo> pr create --base <PR base> --head <dev branch> --title "<ticket id>: <title>" --body "<short summary>", then OP branch-pr <repo> <pr url>
The PR goes from the -dev branch into the shared dev branch; never push or merge into the shared dev branch directly. Report each repo's PR link at the end.`

func discussSystemPrompt(stage string) string {
	if stage == "closeout" {
		return discussCloseoutPrompt
	}
	return discussSetupPrompt
}

// discussRun is everything one Discuss turn needs.
type discussRun struct {
	System, Prompt, SessionID string
	Exe, DB                   string // Exe: space-free path of the server binary
	BoardID, CardID           int64
	Dirs                      []string // board folders, read-only
	OnSession                 func(string)
}

var runSessionFn = runSession // test seam for realRunDiscuss

// runDiscuss runs one `claude -p` Discuss turn and returns the session id. It is a variable so tests can stub it.
var runDiscuss = realRunDiscuss

func realRunDiscuss(ctx context.Context, in discussRun, onText func(string)) (string, error) {
	tools := []string{"Read", "Grep", "Glob"}
	for _, c := range []string{"git", "gh", "branches", "branch-plan", "branch-status", "branch-pr"} {
		tools = append(tools, "Bash("+in.Exe+" "+c+":*)")
	}
	return runSessionFn(ctx, sessionSpec{
		System: in.System, Prompt: in.Prompt, SessionID: in.SessionID, Dirs: in.Dirs, AllowedTools: tools,
		Env: []string{"ONE_PIECE_DB=" + in.DB, "ONE_PIECE_BOARD=" + strconv.FormatInt(in.BoardID, 10),
			"ONE_PIECE_CARD=" + strconv.FormatInt(in.CardID, 10)},
		OnSession: in.OnSession,
	}, onText)
}

func branchLine(r store.Branch) string {
	pr := r.PRURL
	if pr == "" {
		pr = "no PR yet"
	}
	return fmt.Sprintf("- %s | %s → %s | dev: %s | %s | %s", r.Repo, r.BaseBranch, r.FeatureBranch, r.DevBranch, r.Status, pr)
}

// discussPrompt builds the user message: full context for a fresh session, the current state for a resumed one.
func discussPrompt(b store.Board, c store.Card, branches []store.Branch, exe, stage, text string, fresh bool) string {
	var m strings.Builder
	fmt.Fprintf(&m, "Ticket: %s — %s\nStage: %s\n", c.Ident, c.Title, stage)
	if fresh {
		fmt.Fprintf(&m, "Board: %s\n", b.Name)
		if b.Description != "" {
			fmt.Fprintf(&m, "About the product: %s\n", b.Description)
		}
		if len(b.Folders) > 0 {
			fmt.Fprintf(&m, "Product folders you may read:\n- %s\n", strings.Join(b.Folders, "\n- "))
		}
		fmt.Fprintf(&m, "New branches are named %s%s (feature) and %s%s-dev (dev); the shared dev branch (PR target) is %s.\n",
			b.BranchPrefix, c.Ident, b.BranchPrefix, c.Ident, b.DevBranch)
		fmt.Fprintf(&m, "OP = %s\nOP subcommands: branches | branch-plan <repo> <base> | branch-status <repo> <status> [note] | branch-pr <repo> <url> | git <repo> <git args> | gh <repo> <gh args>\n", exe)
		fmt.Fprintf(&m, "Statuses: planned, branched, merged_dev, pr_open, blocked\n")
		fmt.Fprintf(&m, "\nTicket description (Markdown):\n%s\n", c.Description)
	}
	m.WriteString("\nRegistered repos:\n")
	if len(branches) == 0 {
		m.WriteString("none yet\n")
	}
	for _, r := range branches {
		m.WriteString(branchLine(r) + "\n")
	}
	if repos, not := repoMentions(b.Folders, text); len(repos) > 0 || len(not) > 0 {
		if len(repos) > 0 {
			fmt.Fprintf(&m, "\nRepos the owner mentioned with @ in this message:\n- %s\n", strings.Join(repos, "\n- "))
		}
		if len(not) > 0 {
			fmt.Fprintf(&m, "Mentioned but not inside a git repo (ask what they meant):\n- %s\n", strings.Join(not, "\n- "))
		}
	}
	m.WriteString("\n" + text)
	return m.String()
}

func (s *server) discussRunning(cardID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.discuss[cardID]
	return ok
}

func (s *server) stopDiscuss(cardID int64) {
	s.mu.Lock()
	cancel := s.discuss[cardID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// runDiscussTurn runs one turn in the background and settles the assistant message and session.
func (s *server) runDiscussTurn(ctx context.Context, cancel context.CancelFunc, b store.Board, c store.Card, stage, exe string, msgID int64, text string) {
	defer func() { // last: the card only reads as idle once everything is saved
		cancel()
		s.mu.Lock()
		delete(s.discuss, c.ID)
		s.mu.Unlock()
	}()
	sid, _ := s.st.GetDiscussState(c.ID)
	rows, _ := s.st.ListBranches(c.ID)
	run := discussRun{System: discussSystemPrompt(stage), SessionID: sid, Exe: exe, DB: s.st.Path, BoardID: b.ID, CardID: c.ID, Dirs: b.Folders}
	run.Prompt = discussPrompt(b, c, rows, exe, stage, text, sid == "")
	wrote := false
	onText := func(t string) {
		wrote = true
		s.st.AppendDiscussText(msgID, t)
	}
	var announced string
	run.OnSession = func(id string) { announced = id }
	fresh := sid == ""
	newSID, err := runDiscuss(ctx, run, onText)
	if err != nil && !fresh && !wrote && ctx.Err() == nil && strings.Contains(err.Error(), "No conversation found") {
		log.Printf("discuss card %d: resume failed (%v), starting a fresh session", c.ID, err)
		run.SessionID, fresh, announced = "", true, ""
		run.Prompt = discussPrompt(b, c, rows, exe, stage, text, true)
		newSID, err = runDiscuss(ctx, run, onText)
	}
	if _, gone := s.st.GetCard(c.ID); gone != nil { // deleted while Claude was working
		return
	}
	status := "done"
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		status = "stopped"
	case err != nil:
		status = "failed"
		log.Printf("discuss card %d failed: %v", c.ID, err)
		if !wrote {
			msg := "Claude could not answer. Try again."
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				msg = "Claude took too long to answer. Try again."
			}
			onText(msg)
		}
	}
	if status == "done" {
		s.st.SetDiscussState(c.ID, newSID)
	} else if announced != "" && (fresh || sid == "") { // a stopped/failed first turn keeps its context
		s.st.SetDiscussState(c.ID, announced)
	}
	s.st.FinishDiscussMessage(msgID, status)
}

type discussView struct {
	Messages []store.DiscussMessage `json:"messages"`
	Running  bool                   `json:"running"`
	Stage    string                 `json:"stage"` // "" when the ticket's column has no Discuss
}

func (s *server) discussCard(w http.ResponseWriter, r *http.Request) (store.Card, bool) {
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

// GET /api/cards/{id}/discuss
func (s *server) getDiscuss(w http.ResponseWriter, r *http.Request) {
	c, ok := s.discussCard(w, r)
	if !ok {
		return
	}
	v := discussView{Running: s.discussRunning(c.ID)}
	v.Stage, _ = discussStage(c.Status)
	var err error
	if v.Messages, err = s.st.ListDiscussMessages(c.ID); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// POST /api/cards/{id}/discuss/messages {"text": "..."} → 202; the reply arrives through GET.
func (s *server) postDiscussMessage(w http.ResponseWriter, r *http.Request) {
	c, ok := s.discussCard(w, r)
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
	stage, ok := discussStage(c.Status)
	if !ok {
		writeErr(w, store.ConflictError{Msg: "Discuss is available while the ticket is in Business or Done."})
		return
	}
	b, err := s.st.GetBoard(c.BoardID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(b.Folders) == 0 {
		writeErr(w, store.ConflictError{Msg: "This board has no folder for Claude to work in."})
		return
	}
	if fi, err := os.Stat(b.Folders[0]); err != nil || !fi.IsDir() {
		writeErr(w, store.ConflictError{Msg: "Folder not found: " + b.Folders[0]})
		return
	}
	exe, err := agentExe()
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), discussTurnTimeout)
	s.mu.Lock()
	if _, busy := s.discuss[c.ID]; busy {
		s.mu.Unlock()
		cancel()
		writeErr(w, store.ConflictError{Msg: "Claude is still answering. Stop it or wait."})
		return
	}
	s.discuss[c.ID] = cancel
	s.mu.Unlock()
	release := func() {
		cancel()
		s.mu.Lock()
		delete(s.discuss, c.ID)
		s.mu.Unlock()
	}
	if _, err := s.st.AddDiscussMessage(c.ID, "user", text, "done"); err != nil {
		release()
		writeErr(w, err)
		return
	}
	am, err := s.st.AddDiscussMessage(c.ID, "assistant", "", "running")
	if err != nil {
		release()
		writeErr(w, err)
		return
	}
	go s.runDiscussTurn(ctx, cancel, b, c, stage, exe, am.ID, text)
	writeJSON(w, http.StatusAccepted, map[string]bool{"running": true})
}

// POST /api/cards/{id}/discuss/stop
func (s *server) stopDiscussHandler(w http.ResponseWriter, r *http.Request) {
	c, ok := s.discussCard(w, r)
	if !ok {
		return
	}
	s.stopDiscuss(c.ID)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/cards/{id}/discuss/new: forget the Claude session; the transcript stays, with a divider.
func (s *server) newDiscuss(w http.ResponseWriter, r *http.Request) {
	c, ok := s.discussCard(w, r)
	if !ok {
		return
	}
	if s.discussRunning(c.ID) {
		writeErr(w, store.ConflictError{Msg: "Claude is still answering. Stop it or wait."})
		return
	}
	if err := s.st.SetDiscussState(c.ID, ""); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.st.AddDiscussMessage(c.ID, "marker", "New session", "done"); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
