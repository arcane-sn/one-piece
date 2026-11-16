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

// discussStage maps a ticket column to the stage of its Discuss session: setup (Business) and closeout (Done)
// are guided procedures; every other column gets the general git assistant. ok is always true now.
func discussStage(status string) (string, bool) {
	switch status {
	case "business":
		return "setup", true
	case "done":
		return "closeout", true
	}
	return "assist", true
}

// The system prompt only varies with the stage, so it stays byte-identical across a stage's turns.
// "OP" is the command path the first message of a session names. Git is never spelled out as a raw
// command here: everything goes through OP, which only accepts the allow-listed forms.
const discussSystemCommon = `You are a git assistant for one product owner. You prepare and finish the branches of ONE ticket across the repositories the owner names. You do not write code.
Rules:
- You may only run the subcommands of OP that the first message lists. Everything else (raw git, raw gh, any other shell command, editing files) is not permitted, so do not try it. If you need something that is not available, say what and ask the owner to do it.
- The owner decides every branch name and base branch. Never assume a base branch: ask for it, one repo at a time. The base is the latest release BRANCH, never a tag.
- Ask ONE question per turn. Keep replies short. Reply in the language the owner writes in.
- Never resolve a conflict. On any conflict run OP git <repo> merge --abort (or cherry-pick --abort), report the conflicting files and git's message, record OP branch-status <repo> blocked <reason>, and wait for the owner.
- Stop and ask before anything unusual: an existing branch, a failed push, a refusal. Never work around a refusal; report it plainly.
- Before a step that needs a clean tree, check OP git <repo> status. If the tree is dirty, offer OP git <repo> stash push -u -m "<ticket id> <reason>" and wait for a yes. A stash is restored only with OP git <repo> stash pop, on the branch it was made on.
- Fixes are committed on the feature branch (by the owner or by Run) and then integrated again; nothing is ever committed on the -dev branch.
- When the owner agrees on something that should outlive this chat (a base branch, merge versus cherry-pick, skipping a repo, which branch is the release), save it with: echo "<one self-contained sentence>" | OP decision. Save agreements, not chatter.
- After each repo step, record it: OP branch-status <repo> <status> [note].`

const discussSetupPrompt = discussSystemCommon + `
Stage: setup (the ticket is in Business; no code exists yet).
1. If no repo is registered, ask which repositories this ticket touches (the owner can mention them with @ in a message; the message then names the repo roots).
2. For each repo ask for the base branch (the latest release branch). Check the state with: OP git <repo> status. Then run OP branch-plan <repo> <base> (it prints the feature branch name), OP git <repo> fetch --prune origin when the repo has a remote, and OP git <repo> checkout -b <feature branch> origin/<base> (or <base> when there is no remote). Finish with OP branch-status <repo> branched and save the base with OP decision.
3. Summarize which repos are ready and which are left.`

const discussCloseoutPrompt = discussSystemCommon + `
Stage: closeout (the ticket is Done; the feature branches hold the finished work). First run OP branches: it shows each repo's status, integration mode, remote kind and how many feature commits are not yet picked.
Phase 1: integrate EVERY repo and push NOTHING. For each repo whose status is not yet merged_dev, pushed or pr_open (a repo that went back to branched has new commits and is integrated again):
1. OP git <repo> fetch --prune origin, then check the tree is clean.
2. Get the -dev branch: if it does not exist, OP git <repo> checkout -b <dev branch> origin/<PR base>; if it exists, OP git <repo> checkout <dev branch> and OP git <repo> merge --no-edit origin/<PR base> to refresh it.
3. Integrate by the repo's saved mode. merge: OP git <repo> merge --no-edit <feature branch>. cherry-pick: OP pending-picks <repo>, then OP git <repo> cherry-pick -x <shas oldest first>.
4. On success: OP branch-status <repo> merged_dev. On a conflict: abort, OP branch-status <repo> blocked <reason>, report the files, STOP the whole closeout (no repo is pushed while one is blocked), and if the mode is merge propose cherry-pick. Only after the owner agrees: OP branch-mode <repo> cherry-pick, save a decision, and continue.
Phase 2: only when every repo is merged_dev (or further). For each repo: OP git <repo> push -u origin <feature branch>, then OP git <repo> push -u origin <dev branch>. Then, if OP remote-kind <repo> says github and the repo has no PR yet: OP gh <repo> pr create --base <PR base> --head <dev branch> --title "<ticket id>: <title>" --body "<short summary>", then OP branch-pr <repo> <pr url>. If it says anything else, never call gh: run OP branch-status <repo> pushed and tell the owner both branch names are pushed.
The PR goes from the -dev branch into the shared dev branch; never push or merge into the shared dev branch directly. Report each repo's result at the end.`

const discussAssistPrompt = discussSystemCommon + `
Stage: assist (a general git assistant for this ticket; the ticket is not in its setup or closeout column).
Answer questions about the ticket's branches, and do what the owner asks with the allowed subcommands: refresh a branch (OP git <repo> merge --no-edit <base> on the feature branch, OP git <repo> merge --no-edit origin/<PR base> on the -dev branch), pull, switch, stash, abort, integrate by the saved mode, or push. Do not start the setup or closeout procedure unless the owner asks for it. Pushes obey the same rule as closeout: nothing is pushed to a -dev branch until every repo of the ticket is integrated.`

func discussSystemPrompt(stage string) string {
	switch stage {
	case "closeout":
		return discussCloseoutPrompt
	case "assist":
		return discussAssistPrompt
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
	for _, c := range []string{"git", "gh", "branches", "branch-plan", "branch-status", "branch-pr", "branch-mode", "pending-picks", "remote-kind", "decision", "decisions"} {
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
	return fmt.Sprintf("- %s | %s → %s | dev: %s | %s | mode %s | %s", r.Repo, r.BaseBranch, r.FeatureBranch, r.DevBranch, r.Status, r.Mode, pr)
}

// discussPrompt builds the user message: full context for a fresh session, the current state for a resumed one.
func discussPrompt(b store.Board, c store.Card, branches []store.Branch, decisions, exe, stage, text string, fresh bool) string {
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
		fmt.Fprintf(&m, "OP = %s\nOP subcommands: branches | branch-plan <repo> <base> | branch-status <repo> <status> [note] | branch-pr <repo> <url> | branch-mode <repo> merge|cherry-pick | pending-picks <repo> | remote-kind <repo> | decision (text on stdin) | decisions | git <repo> <git args> | gh <repo> <gh args>\n", exe)
		fmt.Fprintf(&m, "Statuses: planned, branched, merged_dev, pushed, pr_open, blocked\n")
		if decisions != "" {
			m.WriteString("\n" + decisions)
		}
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

// decisionBlock is the ticket's decision log formatted for a first message ("" when empty).
func decisionBlock(st *store.Store, cardID int64) string {
	ds, _ := st.ListDecisions(cardID)
	return store.DecisionBlock(ds)
}

// ticketBusyWithRun reports whether Run or Run All is active for the ticket (it edits and commits in the same repos).
func (s *server) ticketBusyWithRun(cardID int64) bool {
	s.mu.Lock()
	_, chain := s.chains[cardID]
	running := map[int64]bool{}
	for id := range s.runs {
		running[id] = true
	}
	s.mu.Unlock()
	if chain {
		return true
	}
	items, _ := s.st.ListItems(cardID)
	for _, it := range items {
		if it.Kind == "agent" && running[it.ID] {
			return true
		}
	}
	return false
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
	run.Prompt = discussPrompt(b, c, rows, decisionBlock(s.st, c.ID), exe, stage, text, sid == "")
	run.Dirs, run.Prompt = s.attachImages(text, run.Dirs, run.Prompt)
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
		run.Prompt = discussPrompt(b, c, rows, decisionBlock(s.st, c.ID), exe, stage, text, true)
		run.Dirs, run.Prompt = s.attachImages(text, b.Folders, run.Prompt)
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
	stage, _ := discussStage(c.Status)
	if s.ticketBusyWithRun(c.ID) {
		writeErr(w, store.ConflictError{Msg: "Run is working on this ticket. Wait for it or stop it first."})
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
